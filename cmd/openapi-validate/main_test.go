package main

import (
	"strings"
	"testing"
)

func TestValidateSpecRejectsMissingSummary(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets:
    get:
      responses:
        "200":
          description: ok
components:
  schemas:
    Error:
      type: object
`
	_, _, err := validateSpec([]byte(spec))
	if err == nil || !strings.Contains(err.Error(), "missing summary") {
		t.Fatalf("expected missing summary error, got %v", err)
	}
}

func TestValidateSpecRejectsNonStringSummary(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets:
    get:
      summary: 123
      responses:
        "200":
          description: ok
components:
  schemas:
    Error:
      type: object
`
	_, _, err := validateSpec([]byte(spec))
	if err == nil || !strings.Contains(err.Error(), "missing summary") {
		t.Fatalf("expected missing summary error, got %v", err)
	}
}

func TestValidateSpecChecksAllOpenAPIMethods(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets:
    options:
      summary: Options widgets
components:
  schemas:
    Error:
      type: object
`
	_, _, err := validateSpec([]byte(spec))
	if err == nil || !strings.Contains(err.Error(), "missing responses") {
		t.Fatalf("expected missing responses error for options operation, got %v", err)
	}
}
