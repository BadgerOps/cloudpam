package storage_test

import (
	"testing"

	"cloudpam/internal/storage"
	"cloudpam/internal/storage/storagetest"
)

func TestMemorySyncSchedules(t *testing.T) {
	ms := storage.NewMemoryStore()
	ds := storage.NewMemoryDiscoveryStore(ms)
	storagetest.RunSyncScheduleSuite(t, ms, ds, ds)
}
