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
