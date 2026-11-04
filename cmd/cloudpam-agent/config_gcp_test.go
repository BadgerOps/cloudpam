package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func covBaseConfig(provider string) *Config {
	return &Config{
		ServerURL: "https://pam.example.com", APIKey: "cpk", AgentName: "agent", AccountID: 1,
		SyncInterval: time.Minute, HeartbeatInterval: 10 * time.Second,
		Provider: provider,
	}
}

func TestValidateRequiresAProvider(t *testing.T) {
	err := covBaseConfig("").Validate()
	if err == nil || err.Error() != "provider is required (aws or gcp); set CLOUDPAM_PROVIDER or yaml" {
		t.Fatalf("Validate() error = %v", err)
	}
	err = covBaseConfig("azure").Validate()
	if err == nil || !strings.Contains(err.Error(), `unsupported provider "azure" (aws or gcp)`) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateGCPSettings(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"aws org on gcp", func(c *Config) { c.GCPProject = "p"; c.AWSOrg.Enabled = true }, "aws_org.enabled requires provider aws"},
		{"gcp org on aws", func(c *Config) { c.Provider = "aws"; c.GCPOrg = GCPOrg{Enabled: true, Parent: "organizations/1"} }, "gcp_org.enabled requires provider gcp"},
		{"missing project", func(c *Config) {}, "gcp_project is required"},
		{"bad parent", func(c *Config) { c.GCPOrg = GCPOrg{Enabled: true, Parent: "projects/1"} }, "gcp_org.parent"},
		{"missing parent", func(c *Config) { c.GCPOrg = GCPOrg{Enabled: true} }, "gcp_org.parent"},
		{"bad template", func(c *Config) {
			c.GCPOrg = GCPOrg{Enabled: true, Parent: "folders/2", ImpersonateServiceAccount: "nope"}
		}, "gcp_org.impersonate_service_account"},
		{"bad rps", func(c *Config) { c.GCPProject = "p"; c.DiscoveryGCPRPS = -1 }, "discovery_gcp_rps"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := covBaseConfig("gcp")
			tc.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAcceptsGCPModes(t *testing.T) {
	single := covBaseConfig("gcp")
	single.GCPProject = "my-project"
	if err := single.Validate(); err != nil {
		t.Fatalf("single-project Validate() = %v", err)
	}
	org := covBaseConfig("gcp")
	org.GCPOrg = GCPOrg{Enabled: true, Parent: "organizations/123", ImpersonateServiceAccount: "cloudpam-discovery@{project}.iam.gserviceaccount.com"}
	if err := org.Validate(); err != nil {
		t.Fatalf("org Validate() = %v", err)
	}
}

func TestLoadConfigReadsGCPEnv(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "gcp-agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "3")
	t.Setenv("CLOUDPAM_PROVIDER", "gcp")
	t.Setenv("CLOUDPAM_GCP_REGIONS", "us-central1, europe-west1")
	t.Setenv("CLOUDPAM_DISCOVERY_GCP_RPS", "4")
	t.Setenv("CLOUDPAM_GCP_ORG_ENABLED", "1")
	t.Setenv("CLOUDPAM_GCP_ORG_PARENT", "folders/42")
	t.Setenv("CLOUDPAM_GCP_ORG_REGIONS", "us-east1")
	t.Setenv("CLOUDPAM_GCP_ORG_EXCLUDE_PROJECTS", "sandbox, ,legacy")
	t.Setenv("CLOUDPAM_GCP_ORG_IMPERSONATE_SA", "d@{project}.iam.gserviceaccount.com")

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Provider != "gcp" || cfg.DiscoveryGCPRPS != 4 {
		t.Fatalf("provider/rps = %q/%v", cfg.Provider, cfg.DiscoveryGCPRPS)
	}
	if !reflect.DeepEqual(cfg.GCPRegions, []string{"us-central1", "europe-west1"}) {
		t.Fatalf("GCPRegions = %#v", cfg.GCPRegions)
	}
	want := GCPOrg{
		Enabled: true, Parent: "folders/42", Regions: []string{"us-east1"},
		ExcludeProjects:           []string{"sandbox", "legacy"},
		ImpersonateServiceAccount: "d@{project}.iam.gserviceaccount.com",
	}
	if !reflect.DeepEqual(cfg.GCPOrg, want) {
		t.Fatalf("GCPOrg = %#v, want %#v", cfg.GCPOrg, want)
	}
}

func TestLoadConfigReadsGCPYAML(t *testing.T) {
	covIsolateAgentEnv(t)
	path := covWriteAgentConfig(t, `
server_url: https://yaml.example.com
api_key: cpk
agent_name: yaml-gcp
account_id: 9
provider: gcp
gcp_project: my-project
gcp_regions: [us-central1]
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Provider != "gcp" || cfg.GCPProject != "my-project" || !reflect.DeepEqual(cfg.GCPRegions, []string{"us-central1"}) {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigRejectsMissingProvider(t *testing.T) {
	covIsolateAgentEnv(t)
	t.Setenv("CLOUDPAM_SERVER_URL", "https://pam.example.com")
	t.Setenv("CLOUDPAM_API_KEY", "cpk")
	t.Setenv("CLOUDPAM_AGENT_NAME", "agent")
	t.Setenv("CLOUDPAM_ACCOUNT_ID", "1")
	if _, err := LoadConfig(""); err == nil || !strings.Contains(err.Error(), "provider is required") {
		t.Fatalf("LoadConfig() error = %v, want provider required", err)
	}
}
