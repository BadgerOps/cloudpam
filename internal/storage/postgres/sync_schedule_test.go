//go:build postgres

package postgres

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestPostgresSyncSchedules(t *testing.T) {
	resetDB(t)
	s := testDB.store
	storagetest.RunSyncScheduleSuite(t, s, s, s)
}
