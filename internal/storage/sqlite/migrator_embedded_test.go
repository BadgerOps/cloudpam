//go:build sqlite

package sqlite

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	migfs "cloudpam/migrations"
)

// A migrations/ directory beside the working directory, such as an older
// checkout, must not replace the migrations compiled into the binary.
func TestRunMigrationsIgnoresMigrationsDirInWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := "CREATE TABLE stale_checkout_marker (id INTEGER);"
	if err := os.WriteFile(filepath.Join(dir, "migrations", "0001_stale.sql"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "embedded.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	want := 0
	entries, err := fs.ReadDir(migfs.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if migFileRe.MatchString(e.Name()) {
			want++
		}
	}
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != want {
		t.Fatalf("applied %d migrations, want the %d embedded ones", applied, want)
	}
	var marker int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'stale_checkout_marker'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != 0 {
		t.Fatal("ran the migrations/ directory from the working directory instead of the embedded migrations")
	}
}
