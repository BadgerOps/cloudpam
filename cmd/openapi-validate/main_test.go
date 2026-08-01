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

func TestValidateSpecAcceptsSpecWithoutComponents(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets:
    get:
      summary: List widgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: string
`
	paths, groups, err := validateSpec([]byte(spec))
	if err != nil {
		t.Fatalf("expected inline-schema spec to validate, got %v", err)
	}
	if paths != 1 {
		t.Errorf("paths = %d, want 1", paths)
	}
	if groups != 0 {
		t.Errorf("component groups = %d, want 0", groups)
	}
}

func TestValidateSpecAcceptsSupportedComponentNamespaces(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets/{id}:
    get:
      summary: Get widget
      parameters:
        - $ref: "#/components/parameters/WidgetID"
      responses:
        "200":
          description: ok
          headers:
            X-Rate-Limit:
              $ref: "#/components/headers/RateLimit"
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Widget"
        "404":
          $ref: "#/components/responses/NotFound"
    put:
      summary: Replace widget
      security:
        - $ref: "#/components/securitySchemes/BearerAuth"
      requestBody:
        $ref: "#/components/requestBodies/WidgetBody"
      responses:
        "200":
          description: ok
components:
  schemas:
    Widget:
      type: object
  responses:
    NotFound:
      description: missing
  parameters:
    WidgetID:
      name: id
      in: path
      required: true
  requestBodies:
    WidgetBody:
      content:
        application/json:
          schema:
            type: object
  headers:
    RateLimit:
      schema:
        type: integer
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
`
	paths, groups, err := validateSpec([]byte(spec))
	if err != nil {
		t.Fatalf("expected component references to validate, got %v", err)
	}
	if paths != 1 {
		t.Errorf("paths = %d, want 1", paths)
	}
	if groups != 6 {
		t.Errorf("component groups = %d, want 6", groups)
	}
}

func TestValidateSpecRejectsMissingComponents(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr string
	}{
		{name: "schema", ref: "#/components/schemas/Missing", wantErr: `references missing schema "Missing"`},
		{name: "response", ref: "#/components/responses/Missing", wantErr: `references missing response "Missing"`},
		{name: "parameter", ref: "#/components/parameters/Missing", wantErr: `references missing parameter "Missing"`},
		{name: "request body", ref: "#/components/requestBodies/Missing", wantErr: `references missing request body "Missing"`},
		{name: "header", ref: "#/components/headers/Missing", wantErr: `references missing header "Missing"`},
		{name: "security scheme", ref: "#/components/securitySchemes/Missing", wantErr: `references missing security scheme "Missing"`},
		{name: "unknown namespace", ref: "#/components/gadgets/Missing", wantErr: `unsupported ref "#/components/gadgets/Missing"`},
		{name: "external ref", ref: "other.yaml#/Widget", wantErr: `unsupported ref "other.yaml#/Widget"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := `
openapi: 3.1.0
paths:
  /widgets:
    get:
      summary: List widgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "` + tt.ref + `"
components:
  schemas:
    Widget:
      type: object
`
			_, _, err := validateSpec([]byte(spec))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateSpecRejectsNonMappingComponents(t *testing.T) {
	spec := `
openapi: 3.1.0
paths:
  /widgets:
    get:
      summary: List widgets
      responses:
        "200":
          description: ok
components: "nope"
`
	_, _, err := validateSpec([]byte(spec))
	if err == nil || !strings.Contains(err.Error(), "components must be a mapping") {
		t.Fatalf("expected components mapping error, got %v", err)
	}
}
