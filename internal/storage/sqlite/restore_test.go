//go:build sqlite

package sqlite

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestSQLiteRestore(t *testing.T) {
	storagetest.RunRestoreSuite(t, newTestStore(t))
}
