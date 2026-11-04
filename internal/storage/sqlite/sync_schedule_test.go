//go:build sqlite

package sqlite

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestSQLiteSyncSchedules(t *testing.T) {
	s := newTestStore(t)
	storagetest.RunSyncScheduleSuite(t, s, s, s)
}
