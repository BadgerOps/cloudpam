//go:build sqlite

package sqlite

import (
	"testing"

	"cloudpam/internal/storage/storagetest"
)

func TestSQLiteAccountCredentials(t *testing.T) {
	storagetest.RunAccountCredentialSuite(t, newTestStore(t))
}
