//go:build !sqlite && !postgres

package main

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func TestDefaultStoreRequiresDevMode(t *testing.T) {
	if os.Getenv("CLOUDPAM_TEST_SELECT_STORE") == "default" {
		selectStore(discardLogger())
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestDefaultStoreRequiresDevMode")
	cmd.Env = withoutDevMode(os.Environ())
	cmd.Env = append(cmd.Env, "CLOUDPAM_TEST_SELECT_STORE=default")
	if err := cmd.Run(); err == nil {
		t.Fatal("selectStore returned successfully without dev mode; want process exit")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("selectStore exit = %v; want non-zero exit", err)
	}
}

func TestDefaultStoreAllowsExplicitDevMode(t *testing.T) {
	t.Setenv("DEV_MODE", "1")
	t.Setenv("SQLITE_DSN", "")
	t.Setenv("DATABASE_URL", "")
	st := selectStore(discardLogger())
	if _, ok := st.(*storage.MemoryStore); !ok {
		t.Fatalf("selectStore returned %T; want *storage.MemoryStore", st)
	}
}

func TestDefaultStoreRejectsConfiguredSQLite(t *testing.T) {
	assertDefaultStoreExits(t, "sqlite-configured", map[string]string{
		"DEV_MODE":   "1",
		"SQLITE_DSN": "file:cloudpam.db?cache=shared&_fk=1",
	})
}

func TestDefaultStoreRejectsConfiguredPostgres(t *testing.T) {
	assertDefaultStoreExits(t, "postgres-configured", map[string]string{
		"DEV_MODE":     "1",
		"DATABASE_URL": "postgres://cloudpam:cloudpam@localhost:5432/cloudpam?sslmode=disable",
	})
}

func assertDefaultStoreExits(t *testing.T, mode string, env map[string]string) {
	t.Helper()
	if os.Getenv("CLOUDPAM_TEST_SELECT_STORE") == mode {
		for key, value := range env {
			t.Setenv(key, value)
		}
		selectStore(discardLogger())
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run="+t.Name())
	cmd.Env = withoutDevMode(os.Environ())
	cmd.Env = append(cmd.Env, "CLOUDPAM_TEST_SELECT_STORE="+mode)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("selectStore returned successfully with persistent storage configured on no-tag binary; want process exit")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() == 0 {
		t.Fatalf("selectStore exit = %v; want non-zero exit", err)
	}
}

func withoutDevMode(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		switch {
		case hasEnvPrefix(entry, "DEV_MODE="):
		case hasEnvPrefix(entry, "CLOUDPAM_DEV_MODE="):
		case hasEnvPrefix(entry, "CLOUDPAM_STORAGE="):
		default:
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func hasEnvPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func discardLogger() observability.Logger {
	return observability.NewLogger(observability.Config{
		Level:  "error",
		Format: "json",
		Output: io.Discard,
	})
}
