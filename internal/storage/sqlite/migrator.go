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
)

var migFileRe = regexp.MustCompile(`^(\d+)_.+\.sql$`)

func runMigrations(db *sql.DB) error {
    if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
        return err
    }
    // discover migrations dir
    dir, err := findMigrationsDir()
    if err != nil {
        return err
    }
    entries, err := os.ReadDir(dir)
    if err != nil {
        return err
    }
    type mig struct{ version int; name, path string }
    var files []mig
    for _, e := range entries {
        if e.IsDir() { continue }
        m := migFileRe.FindStringSubmatch(e.Name())
        if len(m) == 0 { continue }
        v := 0
        fmt.Sscanf(m[1], "%d", &v)
        files = append(files, mig{version: v, name: e.Name(), path: filepath.Join(dir, e.Name())})
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
    for _, f := range files {
        if applied[f.version] { continue }
        sqlBytes, err := os.ReadFile(f.path)
        if err != nil { return err }
        stmt := strings.TrimSpace(string(sqlBytes))
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
    }
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

