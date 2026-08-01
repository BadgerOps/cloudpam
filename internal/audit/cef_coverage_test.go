package audit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCovCEFFormatterNilEvent(t *testing.T) {
	if got := (CEFFormatter{}).Format(nil); got != "" {
		t.Fatalf("Format(nil) = %q, want empty", got)
	}
}

func TestCovCEFFormatterDefaultsVersionAndNames(t *testing.T) {
	got := (CEFFormatter{DeviceVersion: "   "}).Format(&AuditEvent{})
	if !strings.HasPrefix(got, "CEF:0|BadgerOps|CloudPAM|dev|audit|audit.audit|3|") {
		t.Fatalf("Format() = %q, want dev version and audit fallbacks", got)
	}
	if strings.Contains(got, "rt=") {
		t.Errorf("Format() = %q, want no rt for a zero timestamp", got)
	}
	if strings.Contains(got, "externalId=") {
		t.Errorf("Format() = %q, want no externalId when the ID is empty", got)
	}
	if strings.Contains(got, "cn1Label=http_status") {
		t.Errorf("Format() = %q, want no cn1 for a zero status code", got)
	}
	if strings.Contains(got, "src=") {
		t.Errorf("Format() = %q, want no src when there is no IP", got)
	}
}

func TestCovCEFFormatterUsesResourceTypeWhenActionMissing(t *testing.T) {
	got := (CEFFormatter{DeviceVersion: "1.0"}).Format(&AuditEvent{ResourceType: ResourcePool})
	if !strings.Contains(got, "|audit|pool.audit|") {
		t.Fatalf("Format() = %q, want signature audit and name pool.audit", got)
	}
}

func TestCovCEFFormatterUsesActionWhenResourceTypeMissing(t *testing.T) {
	got := (CEFFormatter{DeviceVersion: "1.0"}).Format(&AuditEvent{Action: ActionCreate})
	if !strings.Contains(got, "|create|audit.create|") {
		t.Fatalf("Format() = %q, want signature create and name audit.create", got)
	}
}

func TestCovCEFFormatterSkipsUnparsableIPAddress(t *testing.T) {
	got := (CEFFormatter{}).Format(&AuditEvent{IPAddress: "not-an-ip"})
	if strings.Contains(got, "src=") {
		t.Fatalf("Format() = %q, want the invalid IP dropped", got)
	}
}

func TestCovCEFFormatterNormalisesIPv6Address(t *testing.T) {
	got := (CEFFormatter{}).Format(&AuditEvent{IPAddress: "2001:0db8:0000:0000:0000:0000:0000:0001"})
	if !strings.Contains(got, "src=2001:db8::1") {
		t.Fatalf("Format() = %q, want the canonical IPv6 form", got)
	}
}

func TestCovCEFFormatterMarksFailureOutcome(t *testing.T) {
	got := (CEFFormatter{}).Format(&AuditEvent{Action: ActionCreate, StatusCode: 500})
	if !strings.Contains(got, "outcome=failure") {
		t.Fatalf("Format() = %q, want outcome=failure", got)
	}
	if !strings.Contains(got, "cn1Label=http_status cn1=500") {
		t.Fatalf("Format() = %q, want the status code extension", got)
	}
}

func TestCovOutcome(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{0, "success"},
		{200, "success"},
		{399, "success"},
		{400, "failure"},
		{404, "failure"},
		{500, "failure"},
	}
	for _, tc := range tests {
		if got := outcome(tc.status); got != tc.want {
			t.Errorf("outcome(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestCovCEFSeverity(t *testing.T) {
	tests := []struct {
		name  string
		event *AuditEvent
		want  int
	}{
		{"nil event", nil, 0},
		{"server error", &AuditEvent{StatusCode: 500}, 8},
		{"server error above 500", &AuditEvent{StatusCode: 503}, 8},
		{"client error", &AuditEvent{StatusCode: 400}, 6},
		{"client error 499", &AuditEvent{StatusCode: 499}, 6},
		{"login failed", &AuditEvent{Action: ActionLoginFailed, StatusCode: 200}, 7},
		{"account locked", &AuditEvent{Action: ActionAccountLocked, StatusCode: 200}, 7},
		{"delete", &AuditEvent{Action: ActionDelete, StatusCode: 204}, 5},
		{"read", &AuditEvent{Action: ActionRead, StatusCode: 200}, 1},
		{"create falls through to default", &AuditEvent{Action: ActionCreate, StatusCode: 201}, 3},
		{"unknown action", &AuditEvent{Action: "something-else"}, 3},
		{"status wins over action", &AuditEvent{Action: ActionRead, StatusCode: 500}, 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cefSeverity(tc.event); got != tc.want {
				t.Errorf("cefSeverity() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCovSyslogSeverity(t *testing.T) {
	tests := []struct {
		name  string
		event *AuditEvent
		want  int
	}{
		{"nil event", nil, 6},
		{"server error", &AuditEvent{StatusCode: 500}, 3},
		{"client error", &AuditEvent{StatusCode: 403}, 4},
		{"login failed", &AuditEvent{Action: ActionLoginFailed, StatusCode: 200}, 4},
		{"account locked", &AuditEvent{Action: ActionAccountLocked, StatusCode: 200}, 4},
		{"success", &AuditEvent{Action: ActionCreate, StatusCode: 201}, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := syslogSeverity(tc.event); got != tc.want {
				t.Errorf("syslogSeverity() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCovSyslogPriorityUsesLocal4Facility(t *testing.T) {
	tests := []struct {
		name  string
		event *AuditEvent
		want  int
	}{
		{"informational", &AuditEvent{StatusCode: 200}, 166},
		{"warning", &AuditEvent{StatusCode: 400}, 164},
		{"error", &AuditEvent{StatusCode: 500}, 163},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := syslogPriority(tc.event); got != tc.want {
				t.Errorf("syslogPriority() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCovSyslogToken(t *testing.T) {
	long := strings.Repeat("a", 60)
	tests := []struct {
		name     string
		in       string
		fallback string
		want     string
	}{
		{"empty uses fallback", "", "fb", "fb"},
		{"whitespace uses fallback", "   ", "fb", "fb"},
		{"spaces become underscores", "cloudpam test", "fb", "cloudpam_test"},
		{"brackets become underscores", "a]b", "fb", "a_b"},
		{"quotes become underscores", `a"b`, "fb", "a_b"},
		{"control chars become underscores", "a\tb", "fb", "a_b"},
		{"truncated at 48 chars", long, "fb", long[:48]},
		{"plain token preserved", "login", "fb", "login"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := syslogToken(tc.in, tc.fallback); got != tc.want {
				t.Errorf("syslogToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCovNewSyslogSinkAppliesDefaults(t *testing.T) {
	sink, err := NewSyslogSink(SyslogSinkConfig{Address: "127.0.0.1:514"})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if sink.network != "udp" {
		t.Errorf("network = %q, want the udp default", sink.network)
	}
	if sink.appName != "cloudpam" {
		t.Errorf("appName = %q, want the cloudpam default", sink.appName)
	}
	if sink.timeout != 2*time.Second {
		t.Errorf("timeout = %v, want the 2s default", sink.timeout)
	}
	if sink.pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", sink.pid, os.Getpid())
	}
	if sink.hostname == "" {
		t.Error("hostname = empty, want a resolved hostname or the localhost fallback")
	}
}

func TestCovNewSyslogSinkNormalisesNetworkAndTrimsAddress(t *testing.T) {
	sink, err := NewSyslogSink(SyslogSinkConfig{Network: "  TCP  ", Address: "  127.0.0.1:601  ", Timeout: -1})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if sink.network != "tcp" {
		t.Errorf("network = %q, want tcp", sink.network)
	}
	if sink.address != "127.0.0.1:601" {
		t.Errorf("address = %q, want the trimmed address", sink.address)
	}
	if sink.timeout != 2*time.Second {
		t.Errorf("timeout = %v, want the 2s default for a non-positive timeout", sink.timeout)
	}
}

func TestCovNewSyslogSinkSanitisesHostname(t *testing.T) {
	sink, err := NewSyslogSink(SyslogSinkConfig{Address: "127.0.0.1:514", Hostname: "my host]name"})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if sink.hostname != "my_host_name" {
		t.Fatalf("hostname = %q, want the sanitised form", sink.hostname)
	}
}

func TestCovSyslogSinkSendIgnoresNilEvent(t *testing.T) {
	// The address is never dialled because a nil event short-circuits Send.
	sink, err := NewSyslogSink(SyslogSinkConfig{Network: "tcp", Address: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if err := sink.Send(context.Background(), nil); err != nil {
		t.Fatalf("Send(nil) error = %v, want nil", err)
	}
}

func TestCovSyslogSinkSendReportsDialFailure(t *testing.T) {
	// Bind then release a port so nothing is listening on a known-free address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	sink, err := NewSyslogSink(SyslogSinkConfig{Network: "tcp", Address: addr, Timeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if err := sink.Send(context.Background(), &AuditEvent{Action: ActionCreate}); err == nil {
		t.Fatal("Send() error = nil, want a dial failure")
	}
}

func TestCovSyslogSinkSendOverTCPTerminatesWithNewline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			received <- ""
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			received <- ""
			return
		}
		received <- line
	}()

	sink, err := NewSyslogSink(SyslogSinkConfig{
		Network:   "tcp",
		Address:   ln.Addr().String(),
		AppName:   "cloudpam",
		Hostname:  "tcp-host",
		Formatter: CEFFormatter{DeviceVersion: "2.0.0"},
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	err = sink.Send(context.Background(), &AuditEvent{
		ID:           "evt-tcp",
		Timestamp:    time.Unix(1700000000, 0).UTC(),
		Action:       ActionDelete,
		ResourceType: ResourcePool,
		ResourceID:   "pool-1",
		StatusCode:   204,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case msg := <-received:
		if msg == "" {
			t.Fatal("no syslog message received over TCP")
		}
		if !strings.HasSuffix(msg, "\n") {
			t.Fatalf("TCP framing missing trailing newline: %q", msg)
		}
		if !strings.HasPrefix(msg, "<166>1 2023-11-14T22:13:20Z tcp-host cloudpam ") {
			t.Fatalf("unexpected syslog prefix: %q", msg)
		}
		if !strings.Contains(msg, "CEF:0|BadgerOps|CloudPAM|2.0.0|delete|pool.delete|5|") {
			t.Fatalf("unexpected CEF payload: %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the syslog message")
	}
}

func TestCovSyslogSinkSendHonoursContextDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- struct{}{}
		_ = conn.Close()
	}()

	sink, err := NewSyslogSink(SyslogSinkConfig{Network: "tcp", Address: ln.Addr().String(), Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	if err := sink.Send(ctx, &AuditEvent{Action: ActionCreate, StatusCode: 201}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was never accepted")
	}
}

func TestCovSyslogSinkUsesCurrentTimeForZeroTimestamp(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = conn.Close() }()

	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			received <- ""
			return
		}
		received <- string(buf[:n])
	}()

	sink, err := NewSyslogSink(SyslogSinkConfig{
		Network:  "udp",
		Address:  conn.LocalAddr().String(),
		Hostname: "zero-host",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	before := time.Now().UTC().Add(-time.Second)
	if err := sink.Send(context.Background(), &AuditEvent{Action: ActionCreate, StatusCode: 201}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case msg := <-received:
		if msg == "" {
			t.Fatal("no syslog message received")
		}
		fields := strings.SplitN(msg, " ", 3)
		if len(fields) < 2 {
			t.Fatalf("malformed syslog message: %q", msg)
		}
		stamp, err := time.Parse(time.RFC3339Nano, fields[1])
		if err != nil {
			t.Fatalf("timestamp %q is not RFC3339: %v", fields[1], err)
		}
		if stamp.Before(before) {
			t.Fatalf("timestamp %v predates the send, want the current time substituted for a zero timestamp", stamp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the syslog message")
	}
}

func TestCovSyslogSinkSanitisesActionInMessageID(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() { _ = conn.Close() }()

	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			received <- ""
			return
		}
		received <- string(buf[:n])
	}()

	sink, err := NewSyslogSink(SyslogSinkConfig{
		Network:  "udp",
		Address:  conn.LocalAddr().String(),
		Hostname: "h",
		AppName:  "app",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	if err := sink.Send(context.Background(), &AuditEvent{
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Action:     "",
		StatusCode: 200,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case msg := <-received:
		if msg == "" {
			t.Fatal("no syslog message received")
		}
		want := fmt.Sprintf("<166>1 2023-11-14T22:13:20Z h app %d audit - CEF:0|", os.Getpid())
		if !strings.HasPrefix(msg, want) {
			t.Fatalf("message = %q, want prefix %q (empty action must fall back to \"audit\")", msg, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the syslog message")
	}
}

// covStubLogger is a minimal AuditLogger used to assert delegation.
type covStubLogger struct {
	events   []*AuditEvent
	total    int
	listErr  error
	byRes    []*AuditEvent
	resErr   error
	gotOpts  ListOptions
	gotType  string
	gotResID string
	logErr   error
}

func (l *covStubLogger) Log(_ context.Context, event *AuditEvent) error {
	if l.logErr != nil {
		return l.logErr
	}
	l.events = append(l.events, event)
	return nil
}

func (l *covStubLogger) List(_ context.Context, opts ListOptions) ([]*AuditEvent, int, error) {
	l.gotOpts = opts
	return l.events, l.total, l.listErr
}

func (l *covStubLogger) GetByResource(_ context.Context, resourceType, resourceID string) ([]*AuditEvent, error) {
	l.gotType = resourceType
	l.gotResID = resourceID
	return l.byRes, l.resErr
}

func TestCovForwardingAuditLoggerDelegatesList(t *testing.T) {
	primary := &covStubLogger{
		events: []*AuditEvent{{ID: "evt-1", Action: ActionCreate}},
		total:  7,
	}
	logger := NewForwardingAuditLogger(primary, nil, nil)

	opts := ListOptions{Limit: 5, Offset: 10, Actor: "admin", Action: ActionCreate, ResourceType: ResourcePool}
	events, total, err := logger.List(context.Background(), opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
	if len(events) != 1 || events[0].ID != "evt-1" {
		t.Errorf("events = %+v", events)
	}
	if primary.gotOpts != opts {
		t.Errorf("primary received %+v, want %+v", primary.gotOpts, opts)
	}
}

func TestCovForwardingAuditLoggerPropagatesListError(t *testing.T) {
	wantErr := errors.New("query failed")
	logger := NewForwardingAuditLogger(&covStubLogger{listErr: wantErr}, nil, nil)

	if _, _, err := logger.List(context.Background(), ListOptions{}); !errors.Is(err, wantErr) {
		t.Fatalf("List() error = %v, want %v", err, wantErr)
	}
}

func TestCovForwardingAuditLoggerDelegatesGetByResource(t *testing.T) {
	primary := &covStubLogger{byRes: []*AuditEvent{{ID: "evt-2"}}}
	logger := NewForwardingAuditLogger(primary, nil, nil)

	events, err := logger.GetByResource(context.Background(), ResourceAccount, "acct-3")
	if err != nil {
		t.Fatalf("GetByResource() error = %v", err)
	}
	if len(events) != 1 || events[0].ID != "evt-2" {
		t.Errorf("events = %+v", events)
	}
	if primary.gotType != ResourceAccount || primary.gotResID != "acct-3" {
		t.Errorf("primary received %q/%q", primary.gotType, primary.gotResID)
	}
}

func TestCovForwardingAuditLoggerPropagatesGetByResourceError(t *testing.T) {
	wantErr := errors.New("lookup failed")
	logger := NewForwardingAuditLogger(&covStubLogger{resErr: wantErr}, nil, nil)

	if _, err := logger.GetByResource(context.Background(), ResourcePool, "pool-1"); !errors.Is(err, wantErr) {
		t.Fatalf("GetByResource() error = %v, want %v", err, wantErr)
	}
}

func TestCovForwardingAuditLoggerRequiresPrimary(t *testing.T) {
	logger := NewForwardingAuditLogger(nil, nil, nil)

	if err := logger.Log(context.Background(), &AuditEvent{Action: ActionCreate}); err == nil ||
		!strings.Contains(err.Error(), "primary audit logger is nil") {
		t.Errorf("Log() error = %v, want primary audit logger is nil", err)
	}
	if _, _, err := logger.List(context.Background(), ListOptions{}); err == nil ||
		!strings.Contains(err.Error(), "primary audit logger is nil") {
		t.Errorf("List() error = %v, want primary audit logger is nil", err)
	}
	if _, err := logger.GetByResource(context.Background(), ResourcePool, "p"); err == nil ||
		!strings.Contains(err.Error(), "primary audit logger is nil") {
		t.Errorf("GetByResource() error = %v, want primary audit logger is nil", err)
	}
}

func TestCovForwardingAuditLoggerIgnoresNilEventAndNilSinks(t *testing.T) {
	sink := &captureSink{}
	primary := &covStubLogger{}
	logger := NewForwardingAuditLogger(primary, []Sink{nil, sink, nil}, nil)

	if err := logger.Log(context.Background(), nil); err != nil {
		t.Fatalf("Log(nil) error = %v, want nil", err)
	}
	if len(sink.events) != 0 || len(primary.events) != 0 {
		t.Fatal("a nil event must not reach the primary logger or the sinks")
	}

	if err := logger.Log(context.Background(), &AuditEvent{ID: "evt-3", Action: ActionCreate}); err != nil {
		t.Fatalf("Log() error = %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("sink events = %d, want 1 (nil sinks must be filtered out)", len(sink.events))
	}
}

func TestCovForwardingAuditLoggerDoesNotForwardWhenPrimaryFails(t *testing.T) {
	wantErr := errors.New("persist failed")
	sink := &captureSink{}
	logger := NewForwardingAuditLogger(&covStubLogger{logErr: wantErr}, []Sink{sink}, nil)

	err := logger.Log(context.Background(), &AuditEvent{Action: ActionCreate})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Log() error = %v, want %v", err, wantErr)
	}
	if len(sink.events) != 0 {
		t.Fatalf("sink events = %d, want 0 when persistence failed", len(sink.events))
	}
}

func TestCovForwardingAuditLoggerSwallowsSinkErrorWithoutHandler(t *testing.T) {
	logger := NewForwardingAuditLogger(NewMemoryAuditLogger(), []Sink{&captureSink{err: errors.New("boom")}}, nil)

	if err := logger.Log(context.Background(), &AuditEvent{Action: ActionCreate}); err != nil {
		t.Fatalf("Log() error = %v, want nil when no error handler is configured", err)
	}
}
