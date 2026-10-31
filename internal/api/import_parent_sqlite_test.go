//go:build sqlite

package api

import (
	"path/filepath"
	"testing"

	"cloudpam/internal/storage"
	"cloudpam/internal/storage/sqlite"
)

func TestImportPools_ParentRowsSQLite(t *testing.T) {
	testImportPoolParentRows(t, func(t *testing.T) storage.Store {
		t.Helper()
		st, err := sqlite.New("file:" + filepath.Join(t.TempDir(), "import.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		return st
	})
}
