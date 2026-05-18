//go:build sqlite && !postgres

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"cloudpam/internal/observability"
)

func TestSQLiteStoreInitFailureExits(t *testing.T) {
	if os.Getenv("CLOUDPAM_TEST_SELECT_STORE") == "sqlite" {
		missingDir := filepath.Join(t.TempDir(), "missing")
		t.Setenv("SQLITE_DSN", "file:"+filepath.Join(missingDir, "cloudpam.db")+"?mode=rwc")
		selectStore(discardLogger())
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSQLiteStoreInitFailureExits")
	cmd.Env = append(os.Environ(), "CLOUDPAM_TEST_SELECT_STORE=sqlite")
	if err := cmd.Run(); err == nil {
		t.Fatal("selectStore returned successfully after sqlite init failure; want process exit")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("selectStore exit = %v; want non-zero exit", err)
	}
}

func discardLogger() observability.Logger {
	return observability.NewLogger(observability.Config{
		Level:  "error",
		Format: "json",
		Output: io.Discard,
	})
}
