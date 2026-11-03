//go:build postgres

package postgres

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestPostgresAccountCredentials(t *testing.T) {
	resetDB(t)
	storagetest.RunAccountCredentialSuite(t, testDB.store)
}
