//go:build postgres && !sqlite

package main

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"cloudpam/internal/observability"
)

func TestPostgresStoreInitFailureExits(t *testing.T) {
	if os.Getenv("CLOUDPAM_TEST_SELECT_STORE") == "postgres" {
		t.Setenv("DATABASE_URL", "://not-a-postgres-url")
		selectStore(discardLogger())
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestPostgresStoreInitFailureExits")
	cmd.Env = append(os.Environ(), "CLOUDPAM_TEST_SELECT_STORE=postgres")
	if err := cmd.Run(); err == nil {
		t.Fatal("selectStore returned successfully after postgres init failure; want process exit")
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
