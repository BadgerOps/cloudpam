//go:build postgres

package postgres

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestPostgresRestore(t *testing.T) {
	resetDB(t)
	storagetest.RunRestoreSuite(t, testDB.store)
}
