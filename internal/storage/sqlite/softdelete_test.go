//go:build sqlite

package sqlite

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestSQLiteSoftDeleteReuse(t *testing.T) {
	storagetest.RunSoftDeleteReuseSuite(t, newTestStore(t))
}
