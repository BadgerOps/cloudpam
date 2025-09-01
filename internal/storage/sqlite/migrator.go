//go:build sqlite

package sqlite

import (
    "database/sql"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "time"

    migfs "cloudpam/migrations"
)

var migFileRe = regexp.MustCompile(`^(\d+)_.+\.sql$`)


func runMigrations(db *sql.DB) error {
    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
        return err
    }
    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_info (id INTEGER PRIMARY KEY CHECK(id=1), schema_version INTEGER NOT NULL, min_supported_schema INTEGER NOT NULL DEFAULT 1, app_version TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
        return err
    }
    // discover migrations dir
    var useFS fs.FS
    if dir, err := findMigrationsDir(); err == nil {
        useFS = os.DirFS(dir)
    } else {
        // fallback to embedded
        useFS = migfs.Files
    }
    entries, err := fs.ReadDir(useFS, ".")
    if err != nil { return err }
    type mig struct{ version int; name, path string }
    var files []mig
    for _, e := range entries {
        if e.IsDir() { continue }
        name := e.Name()
        m := migFileRe.FindStringSubmatch(name)
        if len(m) == 0 { continue }
        v := 0
        fmt.Sscanf(m[1], "%d", &v)
        files = append(files, mig{version: v, name: name, path: name})
    }
    sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
    applied := map[int]bool{}
    rows, err := db.Query(`SELECT version FROM schema_migrations`)
    if err != nil { return err }
    defer rows.Close()
    for rows.Next() {
        var v int
        if err := rows.Scan(&v); err != nil { return err }
        applied[v] = true
    }
    if err := rows.Err(); err != nil { return err }
    latest := 0
    for _, f := range files {
        if applied[f.version] { continue }
        var sqlBytes []byte
        // read from selected fs
        if of, ok := useFS.(fs.ReadFileFS); ok {
            b, e := of.ReadFile(f.path); if e != nil { return e }; sqlBytes = b
        } else {
            // should not happen, but keep fallback
            b, e := os.ReadFile(filepath.Clean(f.path)); if e != nil { return e }; sqlBytes = b
        }
        if err != nil { return err }
        stmt := strings.TrimSpace(string(sqlBytes))
        if stmt == "" { continue }
        tx, err := db.Begin()
        if err != nil { return err }
        if _, err := tx.Exec(stmt); err != nil {
            _ = tx.Rollback()
            return fmt.Errorf("migration %s failed: %w", f.name, err)
        }
        if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`, f.version, f.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
            _ = tx.Rollback()
            return err
        }
        if err := tx.Commit(); err != nil { return err }
        if f.version > latest { latest = f.version }
    }
    // Update schema_info
    if latest == 0 {
        // compute current by reading max(version)
        _ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&latest)
    }
    appVersion := os.Getenv("APP_VERSION")
    if appVersion == "" { appVersion = "dev" }
    // upsert id=1
    _, _ = db.Exec(`INSERT INTO schema_info(id, schema_version, min_supported_schema, app_version, applied_at)
                    VALUES(1, ?, COALESCE((SELECT min_supported_schema FROM schema_info WHERE id=1),1), ?, ?)
                    ON CONFLICT(id) DO UPDATE SET schema_version=excluded.schema_version, app_version=excluded.app_version, applied_at=excluded.applied_at`,
        latest, appVersion, time.Now().UTC().Format(time.RFC3339))
    return nil
}

func findMigrationsDir() (string, error) {
    // Try common relative paths from various working dirs
    candidates := []string{
        "migrations",
        filepath.Join("..", "migrations"),
        filepath.Join("..", "..", "migrations"),
        filepath.Join("..", "..", "..", "migrations"),
        filepath.Join("..", "..", "..", "..", "migrations"),
    }
    for _, c := range candidates {
        ok, err := dirHasSQL(c)
        if err == nil && ok {
            return c, nil
        }
    }
    return "", fmt.Errorf("migrations directory not found; tried %v", candidates)
}

func dirHasSQL(dir string) (bool, error) {
    var found bool
    err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
        if err != nil { return err }
        if d.IsDir() { return nil }
        if strings.HasSuffix(d.Name(), ".sql") { found = true }
        return nil
    })
    return found, err
}
