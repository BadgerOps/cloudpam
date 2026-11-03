package storage_test

import (
	"testing"

	"cloudpam/internal/storage"
	"cloudpam/internal/storage/storagetest"
)

func TestMemoryStoreSoftDeleteReuse(t *testing.T) {
	storagetest.RunSoftDeleteReuseSuite(t, storage.NewMemoryStore())
}
