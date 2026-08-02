package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
)

// bufferedLogger returns a logger writing JSON records into buf so tests can
// assert on what the process reported.
func bufferedLogger(buf *bytes.Buffer) observability.Logger {
	return observability.NewLogger(observability.Config{Level: "debug", Format: "json", Output: buf})
}

func TestCleanAppVersionNormalisation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "strips leading v", in: "v1.2.3", want: "1.2.3"},
		{name: "keeps bare semver", in: "1.2.3", want: "1.2.3"},
		{name: "trims surrounding space", in: "  v0.15.0  ", want: "0.15.0"},
		{name: "empty falls back to dev", in: "", want: "dev"},
		{name: "whitespace only falls back to dev", in: "   ", want: "dev"},
		{name: "bare v falls back to dev", in: "v", want: "dev"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanAppVersion(tc.in); got != tc.want {
				t.Errorf("cleanAppVersion(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnvOrFallback(t *testing.T) {
	t.Setenv("CLOUDPAM_TEST_ENVOR", "")
	if got := envOr("CLOUDPAM_TEST_ENVOR", "fallback"); got != "fallback" {
		t.Errorf("envOr with empty value = %q, want fallback", got)
	}
	t.Setenv("CLOUDPAM_TEST_ENVOR", "set")
	if got := envOr("CLOUDPAM_TEST_ENVOR", "fallback"); got != "set" {
		t.Errorf("envOr with set value = %q, want set", got)
	}
}

// TestConfigureAuditSyslogForwardingDeliversCEF asserts the wiring in
// configureAuditSyslogForwarding actually reaches the syslog endpoint and that
// the device version carried in the CEF header is the cleaned app version.
func TestConfigureAuditSyslogForwardingDeliversCEF(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = pc.Close() }()

	t.Setenv("CLOUDPAM_AUDIT_SYSLOG_ADDR", pc.LocalAddr().String())
	t.Setenv("CLOUDPAM_AUDIT_SYSLOG_NETWORK", "udp")
	t.Setenv("CLOUDPAM_AUDIT_SYSLOG_APP_NAME", "cloudpam-test")

	base := audit.NewMemoryAuditLogger()
	var logs bytes.Buffer
	forwarding := configureAuditSyslogForwarding(bufferedLogger(&logs), base, "v9.9.9")
	if forwarding == base {
		t.Fatal("expected a forwarding audit logger when a syslog address is configured")
	}
	if !strings.Contains(logs.String(), "audit syslog forwarding enabled") {
		t.Fatalf("expected enable log line, got %s", logs.String())
	}

	event := &audit.AuditEvent{
		ID:           "evt-1",
		Timestamp:    time.Now().UTC(),
		Actor:        "tester",
		Action:       "create",
		ResourceType: "pool",
		ResourceID:   "42",
	}
	if err := forwarding.Log(context.Background(), event); err != nil {
		t.Fatalf("Log: %v", err)
	}

	// The primary logger must still receive the event.
	events, _, err := base.List(context.Background(), audit.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("primary audit logger recorded %d events, want 1", len(events))
	}

	buf := make([]byte, 4096)
	if err := pc.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read forwarded syslog datagram: %v", err)
	}
	msg := string(buf[:n])
	if !strings.Contains(msg, "cloudpam-test") {
		t.Errorf("syslog message missing configured app name: %s", msg)
	}
	// cleanAppVersion("v9.9.9") == "9.9.9" and lands in the CEF device version field.
	if !strings.Contains(msg, "|9.9.9|") {
		t.Errorf("syslog message missing cleaned device version 9.9.9: %s", msg)
	}
	if strings.Contains(msg, "|v9.9.9|") {
		t.Errorf("device version was not cleaned of its v prefix: %s", msg)
	}
}

// TestConfigureAuditSyslogForwardingReportsSendFailure drives the onError
// callback that configureAuditSyslogForwarding installs.
func TestConfigureAuditSyslogForwardingReportsSendFailure(t *testing.T) {
	// Bind then immediately release a port so dialling it is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	deadAddr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	t.Setenv("CLOUDPAM_AUDIT_SYSLOG_ADDR", deadAddr)
	t.Setenv("CLOUDPAM_AUDIT_SYSLOG_NETWORK", "tcp")

	var logs bytes.Buffer
	base := audit.NewMemoryAuditLogger()
	forwarding := configureAuditSyslogForwarding(bufferedLogger(&logs), base, "0.1.0")
	if forwarding == base {
		t.Fatal("expected a forwarding audit logger")
	}

	err = forwarding.Log(context.Background(), &audit.AuditEvent{
		ID:           "evt-2",
		Timestamp:    time.Now().UTC(),
		Action:       "delete",
		ResourceType: "account",
	})
	// Forwarding failures are best effort and must not fail the primary write.
	if err != nil {
		t.Fatalf("Log returned error on sink failure: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "audit syslog forwarding failed") {
		t.Fatalf("expected forwarding failure warning, got %s", out)
	}
	if !strings.Contains(out, "evt-2") || !strings.Contains(out, "delete") {
		t.Errorf("failure log should carry event context, got %s", out)
	}
}

func TestCloseIfPossible(t *testing.T) {
	t.Run("non closer is ignored", func(t *testing.T) {
		var logs bytes.Buffer
		closeIfPossible(bufferedLogger(&logs), struct{}{}, "plain")
		if logs.Len() != 0 {
			t.Errorf("expected no log output for non-closer, got %s", logs.String())
		}
	})

	t.Run("successful close is silent", func(t *testing.T) {
		var logs bytes.Buffer
		c := &recordingCloser{}
		closeIfPossible(bufferedLogger(&logs), c, "store")
		if !c.closed {
			t.Error("Close was not called")
		}
		if logs.Len() != 0 {
			t.Errorf("expected no log output on clean close, got %s", logs.String())
		}
	})

	t.Run("close error is warned with component name", func(t *testing.T) {
		var logs bytes.Buffer
		c := &recordingCloser{err: errBoom}
		closeIfPossible(bufferedLogger(&logs), c, "keystore")
		if !c.closed {
			t.Error("Close was not called")
		}
		out := logs.String()
		if !strings.Contains(out, "close failed") || !strings.Contains(out, "keystore") {
			t.Errorf("expected close failure warning naming the component, got %s", out)
		}
	})
}

type recordingCloser struct {
	closed bool
	err    error
}

func (r *recordingCloser) Close() error {
	r.closed = true
	return r.err
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }

func TestBootstrapAdminCreatesUser(t *testing.T) {
	t.Setenv("CLOUDPAM_ADMIN_EMAIL", "")
	ctx := context.Background()
	userStore := auth.NewMemoryUserStore()
	var logs bytes.Buffer

	bootstrapAdmin(bufferedLogger(&logs), userStore, "rootadmin", "BootstrapPass123!")

	user, err := userStore.GetByUsername(ctx, "rootadmin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if user == nil {
		t.Fatal("bootstrap admin was not created")
	}
	if user.Role != auth.RoleAdmin {
		t.Errorf("Role = %q, want %q", user.Role, auth.RoleAdmin)
	}
	if !user.IsActive {
		t.Error("bootstrap admin should be active")
	}
	if user.Email != "rootadmin@localhost" {
		t.Errorf("Email = %q, want default rootadmin@localhost", user.Email)
	}
	if len(user.PasswordHash) == 0 || string(user.PasswordHash) == "BootstrapPass123!" {
		t.Errorf("password must be stored hashed, got %q", user.PasswordHash)
	}
	if err := auth.VerifyPassword("BootstrapPass123!", user.PasswordHash); err != nil {
		t.Errorf("stored hash does not verify the bootstrap password: %v", err)
	}
	if !strings.Contains(logs.String(), "bootstrap admin user created") {
		t.Errorf("expected creation log, got %s", logs.String())
	}
}

func TestBootstrapAdminHonoursConfiguredEmail(t *testing.T) {
	t.Setenv("CLOUDPAM_ADMIN_EMAIL", "ops@example.com")
	userStore := auth.NewMemoryUserStore()
	var logs bytes.Buffer

	bootstrapAdmin(bufferedLogger(&logs), userStore, "rootadmin", "BootstrapPass123!")

	user, err := userStore.GetByUsername(context.Background(), "rootadmin")
	if err != nil || user == nil {
		t.Fatalf("GetByUsername: user=%v err=%v", user, err)
	}
	if user.Email != "ops@example.com" {
		t.Errorf("Email = %q, want ops@example.com", user.Email)
	}
}

func TestBootstrapAdminIsIdempotent(t *testing.T) {
	t.Setenv("CLOUDPAM_ADMIN_EMAIL", "")
	ctx := context.Background()
	userStore := auth.NewMemoryUserStore()

	bootstrapAdmin(bufferedLogger(&bytes.Buffer{}), userStore, "rootadmin", "BootstrapPass123!")
	first, err := userStore.GetByUsername(ctx, "rootadmin")
	if err != nil || first == nil {
		t.Fatalf("first GetByUsername: user=%v err=%v", first, err)
	}

	var logs bytes.Buffer
	// A second run with a different password must not overwrite the account.
	bootstrapAdmin(bufferedLogger(&logs), userStore, "rootadmin", "DifferentPass456!")

	second, err := userStore.GetByUsername(ctx, "rootadmin")
	if err != nil || second == nil {
		t.Fatalf("second GetByUsername: user=%v err=%v", second, err)
	}
	if second.ID != first.ID {
		t.Errorf("existing admin was replaced: %q -> %q", first.ID, second.ID)
	}
	if !bytes.Equal(second.PasswordHash, first.PasswordHash) {
		t.Error("existing admin password must not be rewritten by bootstrap")
	}
	if !strings.Contains(logs.String(), "bootstrap admin already exists") {
		t.Errorf("expected already-exists log, got %s", logs.String())
	}
}

func TestBootstrapAdminRejectsWeakPassword(t *testing.T) {
	userStore := auth.NewMemoryUserStore()
	var logs bytes.Buffer

	bootstrapAdmin(bufferedLogger(&logs), userStore, "rootadmin", "short")

	user, err := userStore.GetByUsername(context.Background(), "rootadmin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if user != nil {
		t.Fatal("weak bootstrap password must not create an admin account")
	}
	if !strings.Contains(logs.String(), "bootstrap admin password does not meet requirements") {
		t.Errorf("expected password policy error log, got %s", logs.String())
	}
}

func TestResetPasswordInputFromEnv(t *testing.T) {
	t.Setenv("CLOUDPAM_RESET_PASSWORD", "FromEnvPass123!")
	got, err := resetPasswordInput()
	if err != nil {
		t.Fatalf("resetPasswordInput: %v", err)
	}
	if got != "FromEnvPass123!" {
		t.Errorf("resetPasswordInput() = %q, want FromEnvPass123!", got)
	}
}

func TestResetPasswordInputFromStdin(t *testing.T) {
	tests := []struct {
		name    string
		piped   string
		want    string
		wantErr bool
	}{
		{name: "trailing newline trimmed", piped: "PipedPass123!\n", want: "PipedPass123!"},
		{name: "crlf trimmed", piped: "PipedPass123!\r\n", want: "PipedPass123!"},
		{name: "no trailing newline", piped: "PipedPass123!", want: "PipedPass123!"},
		{name: "internal spaces preserved", piped: "a pass phrase 123!\n", want: "a pass phrase 123!"},
		{name: "empty input rejected", piped: "\n", wantErr: true},
		{name: "no input rejected", piped: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUDPAM_RESET_PASSWORD", "")
			restore := pipeStdin(t, tc.piped)
			defer restore()

			got, err := resetPasswordInput()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resetPasswordInput() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resetPasswordInput: %v", err)
			}
			if got != tc.want {
				t.Errorf("resetPasswordInput() = %q, want %q", got, tc.want)
			}
		})
	}
}

// pipeStdin replaces os.Stdin with a pipe carrying content and returns a
// restore function. Tests using it must not run in parallel.
func pipeStdin(t *testing.T, content string) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stdin
	os.Stdin = r
	go func() {
		_, _ = w.WriteString(content)
		_ = w.Close()
	}()
	return func() {
		os.Stdin = original
		_ = r.Close()
	}
}

func TestResetUserPasswordRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	userStore := auth.NewMemoryUserStore()
	sessionStore := auth.NewMemorySessionStore()

	hash, err := auth.HashPassword("OriginalPass123!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := userStore.Create(ctx, &auth.User{
		ID:           "user-reset",
		Username:     "operator",
		Role:         auth.RoleAdmin,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "NewPassword123!"},
		{name: "whitespace username", username: "   ", password: "NewPassword123!"},
		{name: "password below policy", username: "operator", password: "short"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := resetUserPassword(ctx, userStore, sessionStore, tc.username, tc.password); err == nil {
				t.Fatal("expected error")
			}
			// The stored credential must be untouched by a rejected reset.
			user, err := userStore.GetByUsername(ctx, "operator")
			if err != nil || user == nil {
				t.Fatalf("GetByUsername: user=%v err=%v", user, err)
			}
			if err := auth.VerifyPassword("OriginalPass123!", user.PasswordHash); err != nil {
				t.Errorf("original password no longer verifies after rejected reset: %v", err)
			}
		})
	}
}

func TestResetUserPasswordTrimsUsername(t *testing.T) {
	ctx := context.Background()
	userStore := auth.NewMemoryUserStore()
	sessionStore := auth.NewMemorySessionStore()

	hash, err := auth.HashPassword("OriginalPass123!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := userStore.Create(ctx, &auth.User{
		ID:           "user-trim",
		Username:     "operator",
		Role:         auth.RoleAdmin,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := resetUserPassword(ctx, userStore, sessionStore, "  operator  ", "NewPassword123!"); err != nil {
		t.Fatalf("resetUserPassword: %v", err)
	}
	user, err := userStore.GetByUsername(ctx, "operator")
	if err != nil || user == nil {
		t.Fatalf("GetByUsername: user=%v err=%v", user, err)
	}
	if err := auth.VerifyPassword("NewPassword123!", user.PasswordHash); err != nil {
		t.Errorf("new password does not verify: %v", err)
	}
}

func TestParseOIDCEncryptionKeyUsesConfiguredKey(t *testing.T) {
	want := bytes.Repeat([]byte{0xAB}, 32)
	t.Setenv("CLOUDPAM_OIDC_ENCRYPTION_KEY", hex.EncodeToString(want))

	var logs bytes.Buffer
	got := parseOIDCEncryptionKey(bufferedLogger(&logs))
	if !bytes.Equal(got, want) {
		t.Errorf("parseOIDCEncryptionKey() = %x, want %x", got, want)
	}
	if strings.Contains(logs.String(), "auto-generated key") {
		t.Errorf("should not warn about auto-generation when a key is configured: %s", logs.String())
	}
}

func TestParseOIDCEncryptionKeyGeneratesAndWarns(t *testing.T) {
	t.Setenv("CLOUDPAM_OIDC_ENCRYPTION_KEY", "")

	var logs bytes.Buffer
	first := parseOIDCEncryptionKey(bufferedLogger(&logs))
	if len(first) != 32 {
		t.Fatalf("generated key length = %d, want 32", len(first))
	}
	if !strings.Contains(logs.String(), "auto-generated key") {
		t.Errorf("expected auto-generation warning, got %s", logs.String())
	}

	second := parseOIDCEncryptionKey(bufferedLogger(&bytes.Buffer{}))
	if bytes.Equal(first, second) {
		t.Error("auto-generated keys must be random, got two identical keys")
	}
}

// TestParseOIDCEncryptionKeyExitsOnBadKey asserts the process refuses to start
// with a malformed or wrong-length encryption key rather than silently falling
// back to a generated one.
func TestParseOIDCEncryptionKeyExitsOnBadKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "not hex", key: "zzzz-not-hex"},
		{name: "too short", key: hex.EncodeToString(bytes.Repeat([]byte{1}, 16))},
		{name: "too long", key: hex.EncodeToString(bytes.Repeat([]byte{1}, 48))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv("CLOUDPAM_TEST_OIDC_KEY_EXIT") == "1" {
				parseOIDCEncryptionKey(bufferedLogger(&bytes.Buffer{}))
				return
			}
			cmd := exec.Command(os.Args[0], "-test.run="+strings.ReplaceAll(t.Name(), " ", "_"))
			cmd.Env = append(os.Environ(),
				"CLOUDPAM_TEST_OIDC_KEY_EXIT=1",
				"CLOUDPAM_OIDC_ENCRYPTION_KEY="+tc.key,
			)
			err := cmd.Run()
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() == 0 {
				t.Fatalf("parseOIDCEncryptionKey with key %q exited %v; want non-zero exit", tc.key, err)
			}
		})
	}
}
