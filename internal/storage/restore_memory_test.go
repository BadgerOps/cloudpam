package storage_test

import (
	"testing"

	"cloudpam/internal/storage"
	"cloudpam/internal/storage/storagetest"
)

func TestMemoryStoreRestore(t *testing.T) {
	storagetest.RunRestoreSuite(t, storage.NewMemoryStore())
}
