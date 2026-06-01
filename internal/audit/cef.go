package audit

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	cefVendor  = "BadgerOps"
	cefProduct = "CloudPAM"
)

// CEFFormatter formats audit events as Common Event Format payloads.
type CEFFormatter struct {
	DeviceVersion string
}

// Format returns a CEF event for an audit event.
func (f CEFFormatter) Format(event *AuditEvent) string {
	if event == nil {
		return ""
	}
	version := strings.TrimSpace(f.DeviceVersion)
	if version == "" {
		version = "dev"
	}

	signature := strings.TrimSpace(event.Action)
	if signature == "" {
		signature = "audit"
	}
	name := strings.TrimSpace(event.ResourceType)
	if name == "" {
		name = "audit"
	}
	name = name + "." + signature

	extensions := []string{
		"act=" + cefEscapeExtension(event.Action),
		"outcome=" + cefEscapeExtension(outcome(event.StatusCode)),
	}
	if !event.Timestamp.IsZero() {
		extensions = append(extensions, "rt="+strconv.FormatInt(event.Timestamp.UnixMilli(), 10))
	}
	if event.ID != "" {
		extensions = append(extensions, "externalId="+cefEscapeExtension(event.ID))
	}
	if event.Actor != "" {
		extensions = append(extensions, "suser="+cefEscapeExtension(event.Actor))
	}
	if event.ActorType != "" {
		extensions = append(extensions, "cs1Label=actor_type", "cs1="+cefEscapeExtension(event.ActorType))
	}
	if ip := net.ParseIP(event.IPAddress); ip != nil {
		extensions = append(extensions, "src="+ip.String())
	}
	if event.RequestID != "" {
		extensions = append(extensions, "request="+cefEscapeExtension(event.RequestID))
	}
	if event.ResourceType != "" {
		extensions = append(extensions, "cs2Label=resource_type", "cs2="+cefEscapeExtension(event.ResourceType))
	}
	if event.ResourceID != "" {
		extensions = append(extensions, "cs3Label=resource_id", "cs3="+cefEscapeExtension(event.ResourceID))
	}
	if event.ResourceName != "" {
		extensions = append(extensions, "cs4Label=resource_name", "cs4="+cefEscapeExtension(event.ResourceName))
	}
	if event.StatusCode != 0 {
		extensions = append(extensions, "cn1Label=http_status", "cn1="+strconv.Itoa(event.StatusCode))
	}

	return fmt.Sprintf("CEF:0|%s|%s|%s|%s|%s|%d|%s",
		cefEscapeHeader(cefVendor),
		cefEscapeHeader(cefProduct),
		cefEscapeHeader(version),
		cefEscapeHeader(signature),
		cefEscapeHeader(name),
		cefSeverity(event),
		strings.Join(extensions, " "),
	)
}

func outcome(statusCode int) string {
	if statusCode >= 400 {
		return "failure"
	}
	return "success"
}

func cefSeverity(event *AuditEvent) int {
	if event == nil {
		return 0
	}
	if event.StatusCode >= 500 {
		return 8
	}
	if event.StatusCode >= 400 {
		return 6
	}
	switch event.Action {
	case ActionLoginFailed, ActionAccountLocked:
		return 7
	case ActionDelete:
		return 5
	case ActionRead:
		return 1
	default:
		return 3
	}
}

func cefEscapeHeader(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func cefEscapeExtension(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "=", `\=`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

// SyslogSinkConfig configures a generic syslog audit-event sink.
type SyslogSinkConfig struct {
	Network   string
	Address   string
	AppName   string
	Hostname  string
	Formatter CEFFormatter
	Timeout   time.Duration
}

// SyslogSink sends one CEF payload per syslog message.
type SyslogSink struct {
	network   string
	address   string
	appName   string
	hostname  string
	formatter CEFFormatter
	timeout   time.Duration
	pid       int
}

// NewSyslogSink creates a syslog sink for CEF audit events.
func NewSyslogSink(cfg SyslogSinkConfig) (*SyslogSink, error) {
	network := strings.ToLower(strings.TrimSpace(cfg.Network))
	if network == "" {
		network = "udp"
	}
	if network != "udp" && network != "tcp" {
		return nil, fmt.Errorf("unsupported syslog network %q", cfg.Network)
	}
	address := strings.TrimSpace(cfg.Address)
	if address == "" {
		return nil, fmt.Errorf("syslog address is required")
	}
	appName := syslogToken(cfg.AppName, "cloudpam")
	hostname := syslogToken(cfg.Hostname, "")
	if hostname == "" {
		if h, err := os.Hostname(); err == nil {
			hostname = syslogToken(h, "localhost")
		} else {
			hostname = "localhost"
		}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &SyslogSink{
		network:   network,
		address:   address,
		appName:   appName,
		hostname:  hostname,
		formatter: cfg.Formatter,
		timeout:   timeout,
		pid:       os.Getpid(),
	}, nil
}

// Send sends a formatted audit event to the configured syslog endpoint.
func (s *SyslogSink) Send(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return nil
	}
	payload := s.formatter.Format(event)
	if payload == "" {
		return nil
	}

	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, s.network, s.address)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(s.timeout))
	}

	message := s.rfc5424Message(event, payload)
	if s.network == "tcp" {
		message += "\n"
	}
	_, err = conn.Write([]byte(message))
	return err
}

func (s *SyslogSink) rfc5424Message(event *AuditEvent, payload string) string {
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	return fmt.Sprintf("<%d>1 %s %s %s %d %s - %s",
		syslogPriority(event),
		timestamp.UTC().Format(time.RFC3339Nano),
		s.hostname,
		s.appName,
		s.pid,
		syslogToken(event.Action, "audit"),
		payload,
	)
}

func syslogPriority(event *AuditEvent) int {
	const local4Facility = 20
	return local4Facility*8 + syslogSeverity(event)
}

func syslogSeverity(event *AuditEvent) int {
	if event == nil {
		return 6
	}
	if event.StatusCode >= 500 {
		return 3
	}
	if event.StatusCode >= 400 || event.Action == ActionLoginFailed || event.Action == ActionAccountLocked {
		return 4
	}
	return 6
}

func syslogToken(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	var b strings.Builder
	for _, r := range s {
		if r <= ' ' || r == ']' || r == '"' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return fallback
	}
	if len(out) > 48 {
		return out[:48]
	}
	return out
}
