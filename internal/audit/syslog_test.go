package audit

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSyslogSinkSendsCEFOverUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
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
		AppName:  "cloudpam test",
		Hostname: "test-host",
		Formatter: CEFFormatter{
			DeviceVersion: "0.15.0",
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	err = sink.Send(context.Background(), &AuditEvent{
		ID:           "evt-1",
		Timestamp:    time.Unix(1700000000, 0).UTC(),
		Actor:        "admin",
		ActorType:    ActorTypeUser,
		Action:       ActionLogin,
		ResourceType: ResourceSession,
		ResourceID:   "session-1",
		StatusCode:   200,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	msg := <-received
	if !strings.HasPrefix(msg, "<166>1 2023-11-14T22:13:20Z test-host cloudpam_test ") {
		t.Fatalf("unexpected syslog prefix: %q", msg)
	}
	if !strings.Contains(msg, "CEF:0|BadgerOps|CloudPAM|0.15.0|login|session.login|3|") {
		t.Fatalf("missing CEF payload: %q", msg)
	}
}

func TestNewSyslogSinkRejectsInvalidConfig(t *testing.T) {
	if _, err := NewSyslogSink(SyslogSinkConfig{Address: "127.0.0.1:514", Network: "http"}); err == nil {
		t.Fatal("expected invalid network error")
	}
	if _, err := NewSyslogSink(SyslogSinkConfig{Network: "udp"}); err == nil {
		t.Fatal("expected missing address error")
	}
}

func TestSyslogSinkDestinationReportsDeliveryHealth(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	sink, err := NewSyslogSink(SyslogSinkConfig{
		Network: "udp",
		Address: conn.LocalAddr().String(),
		AppName: "cloudpam",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}

	idle := sink.Destination()
	if idle.State != "idle" || idle.Delivered != 0 || idle.Failed != 0 {
		t.Fatalf("fresh sink = %+v, want idle with zero counters", idle)
	}
	if idle.Type != "syslog" || idle.Format != "cef" || idle.Network != "udp" || idle.Address != conn.LocalAddr().String() || idle.AppName != "cloudpam" {
		t.Fatalf("destination config = %+v", idle)
	}

	event := &AuditEvent{Action: ActionCreate, ResourceType: ResourcePool, StatusCode: 201}
	if err := sink.Send(context.Background(), event); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	ok := sink.Destination()
	if ok.State != "ok" || ok.Delivered != 1 || ok.Failed != 0 || ok.LastSuccessAt == nil || ok.LastFailureAt != nil || ok.LastError != "" {
		t.Fatalf("after success = %+v, want ok with one delivery", ok)
	}

	sink.network = "tcp"
	if err := sink.Send(context.Background(), event); err == nil {
		t.Fatal("expected tcp dial to a udp listener to fail")
	}
	failing := sink.Destination()
	if failing.State != "failing" || failing.Delivered != 1 || failing.Failed != 1 || failing.LastFailureAt == nil || failing.LastError == "" {
		t.Fatalf("after failure = %+v, want failing with the dial error", failing)
	}
	if failing.LastSuccessAt == nil || !failing.LastSuccessAt.Equal(*ok.LastSuccessAt) {
		t.Fatalf("last success should survive a later failure, got %+v", failing)
	}

	sink.network = "udp"
	if err := sink.Send(context.Background(), event); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	recovered := sink.Destination()
	if recovered.State != "ok" || recovered.Delivered != 2 || recovered.Failed != 1 || recovered.LastError == "" {
		t.Fatalf("after recovery = %+v, want ok with the last error kept for reference", recovered)
	}
}

func TestSyslogSinkSkippedSendsLeaveHealthIdle(t *testing.T) {
	sink, err := NewSyslogSink(SyslogSinkConfig{Network: "udp", Address: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("NewSyslogSink() error = %v", err)
	}
	if err := sink.Send(context.Background(), nil); err != nil {
		t.Fatalf("Send(nil) error = %v", err)
	}
	if got := sink.Destination(); got.State != "idle" {
		t.Fatalf("nil event should not count as an attempt, got %+v", got)
	}
}
