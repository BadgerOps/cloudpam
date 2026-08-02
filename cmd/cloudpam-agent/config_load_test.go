package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

// covAgentEnvKeys lists every environment variable LoadConfig consults. Tests
// blank them all so an ambient CLOUDPAM_* value cannot change the outcome.
var covAgentEnvKeys = []string{
	"CLOUDPAM_SERVER_URL",
	"CLOUDPAM_API_KEY",
	"CLOUDPAM_AGENT_NAME",
	"CLOUDPAM_AGENT_ID",
	"CLOUDPAM_AGENT_ID_FILE",
	"CLOUDPAM_ACCOUNT_ID",
	"CLOUDPAM_SYNC_INTERVAL",
	"CLOUDPAM_HEARTBEAT_INTERVAL",
	"CLOUDPAM_AWS_REGIONS",
	"CLOUDPAM_BOOTSTRAP_TOKEN",
	"CLOUDPAM_AWS_ORG_ENABLED",
	"CLOUDPAM_AWS_ORG_ROLE_NAME",
	"CLOUDPAM_AWS_ORG_EXTERNAL_ID",
	"CLOUDPAM_AWS_ORG_REGIONS",
	"CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS",
}

func covIsolateAgentEnv(t *testing.T) {
	t.Helper()
	for _, key := range covAgentEnvKeys {
		t.Setenv(key, "")
	}
}

func covWriteAgentConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func covBootstrapToken(t *testing.T, bundle domain.AgentProvisionBundle) string {
	t.Helper()
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestCovLoadConfigAppliesDefaultsWithEnvOnly(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk_env")
	t.Setenv("CLOUDPAM_AGENT_NAME", "env-agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", " 4242 ")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.ServerURL != "https://pam.example.com" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.APIKey != "cpk_env" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.AgentName != "env-agent" {
		t.Errorf("AgentName = %q", cfg.AgentName)
	}
	if cfg.AccountID != 4242 {
		t.Errorf("AccountID = %d, want 4242 (surrounding spaces must be trimmed)", cfg.AccountID)
	}
	if cfg.SyncInterval != 15*time.Minute {
		t.Errorf("SyncInterval = %v, want 15m", cfg.SyncInterval)
	}
	if cfg.HeartbeatInterval != time.Minute {
		t.Errorf("HeartbeatInterval = %v, want 1m", cfg.HeartbeatInterval)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.RetryBackoff != 5*time.Second {
		t.Errorf("RetryBackoff = %v, want 5s", cfg.RetryBackoff)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want 30s", cfg.RequestTimeout)
	}
	if cfg.Bootstrapped {
		t.Error("Bootstrapped = true, want false when no bootstrap token is used")
	}
}

func TestCovLoadConfigReadsYAMLFile(t *testing.T) {
	covIsolateAgentEnv(t)
	path := covWriteAgentConfig(t, `
server_url: https://yaml.example.com
api_key: cpk_yaml
agent_name: yaml-agent
account_id: 77
sync_interval: 2m
heartbeat_interval: 30s
aws_regions:
  - us-east-1
  - eu-west-1
max_retries: 7
retry_backoff: 250ms
request_timeout: 9s
aws_org:
  enabled: true
  external_id: ext-1
  regions:
    - us-east-2
  exclude_accounts:
    - "111111111111"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.ServerURL != "https://yaml.example.com" || cfg.APIKey != "cpk_yaml" || cfg.AgentName != "yaml-agent" {
		t.Fatalf("yaml identity fields not loaded: %+v", cfg)
	}
	if cfg.AccountID != 77 {
		t.Errorf("AccountID = %d, want 77", cfg.AccountID)
	}
	if cfg.SyncInterval != 2*time.Minute {
		t.Errorf("SyncInterval = %v, want 2m", cfg.SyncInterval)
	}
	if cfg.HeartbeatInterval != 30*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 30s", cfg.HeartbeatInterval)
	}
	if !reflect.DeepEqual(cfg.AWSRegions, []string{"us-east-1", "eu-west-1"}) {
		t.Errorf("AWSRegions = %v", cfg.AWSRegions)
	}
	if cfg.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want 7 (yaml must override the default)", cfg.MaxRetries)
	}
	if cfg.RetryBackoff != 250*time.Millisecond {
		t.Errorf("RetryBackoff = %v, want 250ms", cfg.RetryBackoff)
	}
	if cfg.RequestTimeout != 9*time.Second {
		t.Errorf("RequestTimeout = %v, want 9s", cfg.RequestTimeout)
	}
	if !cfg.AWSOrg.Enabled {
		t.Error("AWSOrg.Enabled = false, want true")
	}
	if cfg.AWSOrg.RoleName != "CloudPAMDiscoveryRole" {
		t.Errorf("AWSOrg.RoleName = %q, want the CloudPAMDiscoveryRole default", cfg.AWSOrg.RoleName)
	}
	if cfg.AWSOrg.ExternalID != "ext-1" {
		t.Errorf("AWSOrg.ExternalID = %q", cfg.AWSOrg.ExternalID)
	}
	if !reflect.DeepEqual(cfg.AWSOrg.Regions, []string{"us-east-2"}) {
		t.Errorf("AWSOrg.Regions = %v", cfg.AWSOrg.Regions)
	}
	if !reflect.DeepEqual(cfg.AWSOrg.ExcludeAccounts, []string{"111111111111"}) {
		t.Errorf("AWSOrg.ExcludeAccounts = %v", cfg.AWSOrg.ExcludeAccounts)
	}
}

func TestCovLoadConfigEnvOverridesYAML(t *testing.T) {
	covIsolateAgentEnv(t)
	path := covWriteAgentConfig(t, `
server_url: https://yaml.example.com
api_key: cpk_yaml
agent_name: yaml-agent
account_id: 1
agent_id_file: /yaml/agent-id
aws_regions:
  - yaml-region
aws_org:
  role_name: YamlRole
  external_id: yaml-ext
  regions:
    - yaml-org-region
  exclude_accounts:
    - "999999999999"
`)

	t.Setenv("CLOUDPAM_SERVER_URL", "https://env.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk_env")
	t.Setenv("CLOUDPAM_AGENT_NAME", "env-agent")
	t.Setenv("CLOUDPAM_AGENT_ID", "1b4e28ba-2fa1-11d2-883f-0016d3cca427")
	t.Setenv("CLOUDPAM_AGENT_ID_FILE", "/env/agent-id")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "99")
	t.Setenv("CLOUDPAM_SYNC_INTERVAL", "3m")
	t.Setenv("CLOUDPAM_HEARTBEAT_INTERVAL", "45s")
	t.Setenv("CLOUDPAM_AWS_REGIONS", "env-a,env-b")
	t.Setenv("CLOUDPAM_AWS_ORG_ENABLED", "1")
	t.Setenv("CLOUDPAM_AWS_ORG_ROLE_NAME", "EnvRole")
	t.Setenv("CLOUDPAM_AWS_ORG_EXTERNAL_ID", "env-ext")
	t.Setenv("CLOUDPAM_AWS_ORG_REGIONS", "env-org-a,env-org-b")
	t.Setenv("CLOUDPAM_AWS_ORG_EXCLUDE_ACCOUNTS", "222222222222,333333333333")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ServerURL", cfg.ServerURL, "https://env.example.com"},
		{"APIKey", cfg.APIKey, "cpk_env"},
		{"AgentName", cfg.AgentName, "env-agent"},
		{"AgentID", cfg.AgentID, "1b4e28ba-2fa1-11d2-883f-0016d3cca427"},
		{"AgentIDFile", cfg.AgentIDFile, "/env/agent-id"},
		{"AccountID", cfg.AccountID, int64(99)},
		{"SyncInterval", cfg.SyncInterval, 3 * time.Minute},
		{"HeartbeatInterval", cfg.HeartbeatInterval, 45 * time.Second},
		{"AWSRegions", cfg.AWSRegions, []string{"env-a", "env-b"}},
		{"AWSOrg.Enabled", cfg.AWSOrg.Enabled, true},
		{"AWSOrg.RoleName", cfg.AWSOrg.RoleName, "EnvRole"},
		{"AWSOrg.ExternalID", cfg.AWSOrg.ExternalID, "env-ext"},
		{"AWSOrg.Regions", cfg.AWSOrg.Regions, []string{"env-org-a", "env-org-b"}},
		{"AWSOrg.ExcludeAccounts", cfg.AWSOrg.ExcludeAccounts, []string{"222222222222", "333333333333"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestCovLoadConfigOrgEnabledAcceptsTrueLiteral(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")

	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"1", true},
		{"yes", false},
		{"TRUE", false},
		{"", false},
	} {
		t.Setenv("CLOUDPAM_AWS_ORG_ENABLED", tc.value)
		cfg, err := LoadConfig("")
		if err != nil {
			t.Fatalf("LoadConfig(%q) error = %v", tc.value, err)
		}
		if cfg.AWSOrg.Enabled != tc.want {
			t.Errorf("CLOUDPAM_AWS_ORG_ENABLED=%q -> Enabled = %v, want %v", tc.value, cfg.AWSOrg.Enabled, tc.want)
		}
	}
}

func TestCovLoadConfigIgnoresUnparsableDurations(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")
	t.Setenv("CLOUDPAM_SYNC_INTERVAL", "not-a-duration")
	t.Setenv("CLOUDPAM_HEARTBEAT_INTERVAL", "also-bad")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.SyncInterval != 15*time.Minute {
		t.Errorf("SyncInterval = %v, want the 15m default to survive a bad env value", cfg.SyncInterval)
	}
	if cfg.HeartbeatInterval != time.Minute {
		t.Errorf("HeartbeatInterval = %v, want the 1m default to survive a bad env value", cfg.HeartbeatInterval)
	}
}

func TestCovLoadConfigFileErrors(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadConfig(filepath.Join(t.TempDir(), "nope.yaml"))
		if err == nil || !strings.Contains(err.Error(), "read config file") {
			t.Fatalf("error = %v, want read config file error", err)
		}
	})

	t.Run("malformed yaml", func(t *testing.T) {
		path := covWriteAgentConfig(t, "server_url: [unterminated\n")
		_, err := LoadConfig(path)
		if err == nil || !strings.Contains(err.Error(), "parse config file") {
			t.Fatalf("error = %v, want parse config file error", err)
		}
	})
}

func TestCovLoadConfigRejectsNonNumericAccountID(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "twelve")

	cfg, err := LoadConfig("")
	if err == nil || !strings.Contains(err.Error(), "invalid CLOUDPAM_ACCOUNT_ID") {
		t.Fatalf("error = %v, want invalid CLOUDPAM_ACCOUNT_ID", err)
	}
	if cfg != nil {
		t.Fatalf("config = %+v, want nil on error", cfg)
	}
}

func TestCovLoadConfigPropagatesValidationFailure(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")

	if _, err := LoadConfig(""); err == nil || !strings.Contains(err.Error(), "server_url is required") {
		t.Fatalf("error = %v, want server_url required", err)
	}
}

func TestCovValidateRejectsIncompleteConfigs(t *testing.T) {
	base := func() *Config {
		return &Config{
			ServerURL:         "https://pam.example.com",
			APIKey:            "cpk",
			AgentName:         "agent",
			AccountID:         1,
			SyncInterval:      time.Minute,
			HeartbeatInterval: 10 * time.Second,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"missing server url", func(c *Config) { c.ServerURL = "" }, "server_url is required"},
		{"missing api key", func(c *Config) { c.APIKey = "" }, "api_key is required"},
		{"missing agent name", func(c *Config) { c.AgentName = "" }, "agent_name is required"},
		{"zero account id", func(c *Config) { c.AccountID = 0 }, "account_id must be a positive integer"},
		{"negative account id", func(c *Config) { c.AccountID = -5 }, "account_id must be a positive integer"},
		{"sync interval too small", func(c *Config) { c.SyncInterval = 59 * time.Second }, "sync_interval must be at least 1 minute"},
		{"heartbeat interval too small", func(c *Config) { c.HeartbeatInterval = 9 * time.Second }, "heartbeat_interval must be at least 10 seconds"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestCovValidateAcceptsBoundaryIntervals(t *testing.T) {
	cfg := &Config{
		ServerURL:         "https://pam.example.com",
		APIKey:            "cpk",
		AgentName:         "agent",
		AccountID:         1,
		SyncInterval:      time.Minute,
		HeartbeatInterval: 10 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil at the exact minimum intervals", err)
	}
}

func TestCovValidateDefaultsOrgRoleNameOnlyWhenOrgEnabled(t *testing.T) {
	enabled := &Config{
		ServerURL: "https://pam.example.com", APIKey: "cpk", AgentName: "agent", AccountID: 1,
		SyncInterval: time.Minute, HeartbeatInterval: 10 * time.Second,
		AWSOrg: AWSOrg{Enabled: true},
	}
	if err := enabled.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if enabled.AWSOrg.RoleName != "CloudPAMDiscoveryRole" {
		t.Errorf("RoleName = %q, want CloudPAMDiscoveryRole", enabled.AWSOrg.RoleName)
	}

	explicit := &Config{
		ServerURL: "https://pam.example.com", APIKey: "cpk", AgentName: "agent", AccountID: 1,
		SyncInterval: time.Minute, HeartbeatInterval: 10 * time.Second,
		AWSOrg: AWSOrg{Enabled: true, RoleName: "CustomRole"},
	}
	if err := explicit.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if explicit.AWSOrg.RoleName != "CustomRole" {
		t.Errorf("RoleName = %q, want the explicit CustomRole to be preserved", explicit.AWSOrg.RoleName)
	}

	disabled := &Config{
		ServerURL: "https://pam.example.com", APIKey: "cpk", AgentName: "agent", AccountID: 1,
		SyncInterval: time.Minute, HeartbeatInterval: 10 * time.Second,
	}
	if err := disabled.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if disabled.AWSOrg.RoleName != "" {
		t.Errorf("RoleName = %q, want empty when org mode is disabled", disabled.AWSOrg.RoleName)
	}
}

func TestCovValidateAppliesBootstrapToken(t *testing.T) {
	token := covBootstrapToken(t, domain.AgentProvisionBundle{
		AgentName: "bootstrapped-agent",
		APIKey:    "cpk_bootstrap",
		ServerURL: "https://bootstrap.example.com",
	})

	cfg := &Config{
		ServerURL:         "https://stale.example.com",
		AgentName:         "stale-agent",
		AccountID:         5,
		SyncInterval:      time.Minute,
		HeartbeatInterval: 10 * time.Second,
		BootstrapToken:    token,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !cfg.Bootstrapped {
		t.Error("Bootstrapped = false, want true after decoding a bootstrap token")
	}
	if cfg.ServerURL != "https://bootstrap.example.com" {
		t.Errorf("ServerURL = %q, want the bundle value to replace the stale one", cfg.ServerURL)
	}
	if cfg.APIKey != "cpk_bootstrap" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.AgentName != "bootstrapped-agent" {
		t.Errorf("AgentName = %q, want the bundle value to replace the stale one", cfg.AgentName)
	}
}

func TestCovValidateIgnoresBootstrapTokenWhenAPIKeySet(t *testing.T) {
	token := covBootstrapToken(t, domain.AgentProvisionBundle{
		AgentName: "bundle-agent",
		APIKey:    "cpk_bundle",
		ServerURL: "https://bundle.example.com",
	})

	cfg := &Config{
		ServerURL:         "https://explicit.example.com",
		APIKey:            "cpk_explicit",
		AgentName:         "explicit-agent",
		AccountID:         5,
		SyncInterval:      time.Minute,
		HeartbeatInterval: 10 * time.Second,
		BootstrapToken:    token,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Bootstrapped {
		t.Error("Bootstrapped = true, want false when an explicit api_key wins")
	}
	if cfg.APIKey != "cpk_explicit" || cfg.ServerURL != "https://explicit.example.com" || cfg.AgentName != "explicit-agent" {
		t.Fatalf("explicit config was overwritten by the bundle: %+v", cfg)
	}
}

func TestCovValidateRejectsBadBootstrapToken(t *testing.T) {
	cfg := &Config{
		AccountID:         1,
		SyncInterval:      time.Minute,
		HeartbeatInterval: 10 * time.Second,
		BootstrapToken:    "!!!not-base64!!!",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid bootstrap token") {
		t.Fatalf("Validate() error = %v, want invalid bootstrap token", err)
	}
}

func TestCovDecodeBootstrapTokenErrors(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr string
	}{
		{"not base64", "%%%%", "base64 decode"},
		{"not json", base64.StdEncoding.EncodeToString([]byte("not json")), "json decode"},
		{
			"missing api key",
			covBootstrapToken(t, domain.AgentProvisionBundle{AgentName: "a", ServerURL: "https://x"}),
			"bundle missing required fields",
		},
		{
			"missing server url",
			covBootstrapToken(t, domain.AgentProvisionBundle{AgentName: "a", APIKey: "k"}),
			"bundle missing required fields",
		},
		{
			"missing agent name",
			covBootstrapToken(t, domain.AgentProvisionBundle{APIKey: "k", ServerURL: "https://x"}),
			"bundle missing required fields",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := decodeBootstrapToken(tc.token)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("decodeBootstrapToken() error = %v, want %q", err, tc.wantErr)
			}
			if bundle != nil {
				t.Fatalf("bundle = %+v, want nil on error", bundle)
			}
		})
	}
}

func TestCovDecodeBootstrapTokenRoundTrip(t *testing.T) {
	want := domain.AgentProvisionBundle{
		AgentName: "round-trip",
		APIKey:    "cpk_rt",
		ServerURL: "https://rt.example.com",
	}
	got, err := decodeBootstrapToken(covBootstrapToken(t, want))
	if err != nil {
		t.Fatalf("decodeBootstrapToken() error = %v", err)
	}
	if *got != want {
		t.Fatalf("bundle = %+v, want %+v", *got, want)
	}
}

func TestCovLoadConfigBootstrapTokenFromEnv(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_BOOTSTRAP_TOKEN", covBootstrapToken(t, domain.AgentProvisionBundle{
		AgentName: "env-bootstrapped",
		APIKey:    "cpk_env_bootstrap",
		ServerURL: "https://envboot.example.com",
	}))
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "12")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.Bootstrapped {
		t.Error("Bootstrapped = false, want true")
	}
	if cfg.AgentName != "env-bootstrapped" || cfg.APIKey != "cpk_env_bootstrap" || cfg.ServerURL != "https://envboot.example.com" {
		t.Fatalf("bootstrap fields not applied: %+v", cfg)
	}
	if cfg.AccountID != 12 {
		t.Errorf("AccountID = %d, want 12", cfg.AccountID)
	}
}

func TestCovResolveAgentIDRejectsMalformedID(t *testing.T) {
	cfg := &Config{AgentID: "not-a-uuid"}
	if _, err := cfg.ResolveAgentID(); err == nil || !strings.Contains(err.Error(), "invalid agent_id") {
		t.Fatalf("ResolveAgentID() error = %v, want invalid agent_id", err)
	}
}

func TestCovResolveAgentIDRejectsCorruptIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-id")
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := (&Config{AgentIDFile: path}).ResolveAgentID(); err == nil || !strings.Contains(err.Error(), "parse agent_id_file") {
		t.Fatalf("ResolveAgentID() error = %v, want parse agent_id_file", err)
	}
}

func TestCovResolveAgentIDReportsUnreadableIDFile(t *testing.T) {
	// A directory in place of the ID file yields a non-ErrNotExist read error.
	dir := filepath.Join(t.TempDir(), "agent-id")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := (&Config{AgentIDFile: dir}).ResolveAgentID()
	if err == nil {
		t.Fatal("ResolveAgentID() error = nil, want a read failure for a directory path")
	}
	if !strings.Contains(err.Error(), "agent_id_file") {
		t.Fatalf("ResolveAgentID() error = %v, want it to name agent_id_file", err)
	}
}

func TestCovDeterministicAgentIDVariesWithIdentity(t *testing.T) {
	base := &Config{ServerURL: "https://a.example.com", AgentName: "agent", AccountID: 1}
	baseID := deterministicAgentID(base)

	if v := baseID.Version(); v != 4 {
		t.Errorf("uuid version = %v, want 4", v)
	}
	if v := baseID.Variant().String(); v != "RFC4122" {
		t.Errorf("uuid variant = %v, want RFC4122", v)
	}

	for _, tc := range []struct {
		name string
		cfg  *Config
	}{
		{"different server url", &Config{ServerURL: "https://b.example.com", AgentName: "agent", AccountID: 1}},
		{"different agent name", &Config{ServerURL: "https://a.example.com", AgentName: "other", AccountID: 1}},
		{"different account id", &Config{ServerURL: "https://a.example.com", AgentName: "agent", AccountID: 2}},
	} {
		if got := deterministicAgentID(tc.cfg); got == baseID {
			t.Errorf("%s: id = %s, want it to differ from the base id", tc.name, got)
		}
	}
}
