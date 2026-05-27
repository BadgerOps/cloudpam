//go:build sqlite && postgres

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"cloudpam/internal/observability"
)

func TestBothStoreInitFailureExitsForConfiguredPostgres(t *testing.T) {
	assertStoreInitFailureExits(t, "postgres", map[string]string{
		"DATABASE_URL": "://not-a-postgres-url",
	})
}

func TestBothStoreInitFailureExitsForSQLite(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing")
	assertStoreInitFailureExits(t, "sqlite", map[string]string{
		"DATABASE_URL": "",
		"SQLITE_DSN":   "file:" + filepath.Join(missingDir, "cloudpam.db") + "?mode=rwc",
	})
}

func assertStoreInitFailureExits(t *testing.T, mode string, env map[string]string) {
	t.Helper()
	if os.Getenv("CLOUDPAM_TEST_SELECT_STORE") == mode {
		for key, value := range env {
			t.Setenv(key, value)
		}
		selectStore(discardLogger())
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run="+t.Name())
	cmd.Env = append(os.Environ(), "CLOUDPAM_TEST_SELECT_STORE="+mode)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("selectStore returned successfully after persistent store init failure; want process exit")
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
