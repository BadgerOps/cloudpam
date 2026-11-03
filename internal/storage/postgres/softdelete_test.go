//go:build postgres

package postgres

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestPostgresSoftDeleteReuse(t *testing.T) {
	resetDB(t)
	storagetest.RunSoftDeleteReuseSuite(t, testDB.store)
}
