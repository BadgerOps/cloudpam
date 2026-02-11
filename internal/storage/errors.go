package storage

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for the storage layer.
// HTTP handlers should use errors.Is() to map these to appropriate HTTP status codes.
var (
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates the operation conflicts with existing state
	// (e.g., deleting a pool that has children, or a duplicate key).
	ErrConflict = errors.New("conflict")

	// ErrValidation indicates the input failed validation
	// (e.g., missing required fields).
	ErrValidation = errors.New("validation error")
)

// WrapIfConflict wraps a database error as ErrConflict if it represents a
// unique constraint violation. This detects UNIQUE errors from SQLite drivers.
func WrapIfConflict(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}
