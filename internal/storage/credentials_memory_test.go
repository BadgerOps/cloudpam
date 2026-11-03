package storage_test

import (
	"testing"

	"cloudpam/internal/storage"
	"cloudpam/internal/storage/storagetest"
)

func TestMemoryStoreAccountCredentials(t *testing.T) {
	storagetest.RunAccountCredentialSuite(t, storage.NewMemoryStore())
}
