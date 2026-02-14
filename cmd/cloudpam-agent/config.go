package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the agent configuration.
type Config struct {
	ServerURL         string        `yaml:"server_url"`
	APIKey            string        `yaml:"api_key"`
	AgentName         string        `yaml:"agent_name"`
	AccountID         int64         `yaml:"account_id"`
	SyncInterval      time.Duration `yaml:"sync_interval"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	AWSRegions        []string      `yaml:"aws_regions"`
	MaxRetries        int           `yaml:"max_retries"`
	RetryBackoff      time.Duration `yaml:"retry_backoff"`
	RequestTimeout    time.Duration `yaml:"request_timeout"`
}

// LoadConfig loads configuration from a YAML file and environment variables.
// Environment variables override YAML values.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		// Defaults
		SyncInterval:      15 * time.Minute,
		HeartbeatInterval: 1 * time.Minute,
		MaxRetries:        3,
		RetryBackoff:      5 * time.Second,
		RequestTimeout:    30 * time.Second,
	}

	// Load from YAML file if provided
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config file: %w", err)
		}
	}

	// Override with environment variables
	if v := os.Getenv("CLOUDPAM_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	if v := os.Getenv("CLOUDPAM_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("CLOUDPAM_AGENT_NAME"); v != "" {
		cfg.AgentName = v
	}
	if v := os.Getenv("CLOUDPAM_ACCOUNT_ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.AccountID = id
		}
	}
	if v := os.Getenv("CLOUDPAM_SYNC_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SyncInterval = d
		}
	}
	if v := os.Getenv("CLOUDPAM_HEARTBEAT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.HeartbeatInterval = d
		}
	}
	if v := os.Getenv("CLOUDPAM_AWS_REGIONS"); v != "" {
		cfg.AWSRegions = strings.Split(v, ",")
	}

	// Validate required fields
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required configuration fields are set.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return errors.New("server_url is required (set CLOUDPAM_SERVER_URL or yaml)")
	}
	if c.APIKey == "" {
		return errors.New("api_key is required (set CLOUDPAM_API_KEY or yaml)")
	}
	if c.AgentName == "" {
		return errors.New("agent_name is required (set CLOUDPAM_AGENT_NAME or yaml)")
	}
	if c.AccountID < 1 {
		return errors.New("account_id must be a positive integer (set CLOUDPAM_ACCOUNT_ID or yaml)")
	}
	if c.SyncInterval < 1*time.Minute {
		return errors.New("sync_interval must be at least 1 minute")
	}
	if c.HeartbeatInterval < 10*time.Second {
		return errors.New("heartbeat_interval must be at least 10 seconds")
	}
	return nil
}
