package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// Pusher handles HTTP communication with the CloudPAM server.
type Pusher struct {
	serverURL string
	apiKey    string
	agentID   uuid.UUID
	client    *http.Client
	logger    *slog.Logger
}

// NewPusher creates a new Pusher instance.
func NewPusher(serverURL, apiKey string, agentID uuid.UUID, timeout time.Duration, logger *slog.Logger) *Pusher {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &Pusher{
		serverURL: serverURL,
		apiKey:    apiKey,
		agentID:   agentID,
		client: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// Register calls the server's agent registration endpoint.
// This is used on first startup when the agent was configured via a bootstrap token.
func (p *Pusher) Register(ctx context.Context, name string, accountID int64, version, hostname string) (*domain.AgentRegisterResponse, error) {
	req := domain.AgentRegisterRequest{
		AgentID:   p.agentID,
		Name:      name,
		AccountID: accountID,
		Version:   version,
		Hostname:  hostname,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}

	url := p.serverURL + "/api/v1/discovery/agents/register"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var regResp domain.AgentRegisterResponse
		if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		return &regResp, nil
	}

	var errBody struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	return nil, fmt.Errorf("registration failed (status %d): %s - %s", resp.StatusCode, errBody.Error, errBody.Detail)
}

// PushResources sends discovered resources to the server with retry logic.
func (p *Pusher) PushResources(ctx context.Context, accountID int64, resources []domain.DiscoveredResource, maxRetries int, backoff time.Duration) error {
	req := domain.IngestRequest{
		AccountID: accountID,
		Resources: resources,
		AgentID:   &p.agentID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal ingest request: %w", err)
	}

	url := p.serverURL + "/api/v1/discovery/ingest"
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry (exponential backoff)
			delay := backoff * time.Duration(1<<uint(attempt-1))
			p.logger.Info("retrying push", "attempt", attempt, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			p.logger.Error("push failed", "error", lastErr, "attempt", attempt+1)
			continue
		}

		// Success!
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var ingestResp domain.IngestResponse
			if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
				_ = resp.Body.Close()
				return fmt.Errorf("decode response: %w", err)
			}
			_ = resp.Body.Close()

			p.logger.Info("pushed resources",
				"job_id", ingestResp.JobID,
				"found", ingestResp.ResourcesFound,
				"created", ingestResp.ResourcesCreated,
				"updated", ingestResp.ResourcesUpdated,
				"deleted", ingestResp.ResourcesDeleted,
			)
			return nil
		}

		// Client error (4xx) - don't retry
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			var errBody struct {
				Error  string `json:"error"`
				Detail string `json:"detail"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&errBody)
			_ = resp.Body.Close()
			return fmt.Errorf("server rejected request (status %d): %s - %s", resp.StatusCode, errBody.Error, errBody.Detail)
		}

		// Server error (5xx) - retry
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("server error (status %d)", resp.StatusCode)
		p.logger.Error("push failed", "error", lastErr, "attempt", attempt+1)
	}

	return fmt.Errorf("push failed after %d attempts: %w", maxRetries+1, lastErr)
}

// PushOrgResources sends bulk org ingest request to the server with retry logic.
func (p *Pusher) PushOrgResources(ctx context.Context, req domain.BulkIngestRequest, maxRetries int, backoff time.Duration) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal org ingest request: %w", err)
	}

	url := p.serverURL + "/api/v1/discovery/ingest/org"
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoff * time.Duration(1<<uint(attempt-1))
			p.logger.Info("retrying org push", "attempt", attempt, "delay", delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("http request: %w", err)
			p.logger.Error("org push failed", "error", lastErr, "attempt", attempt+1)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var ingestResp domain.BulkIngestResponse
			if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
				_ = resp.Body.Close()
				return fmt.Errorf("decode response: %w", err)
			}
			_ = resp.Body.Close()

			p.logger.Info("pushed org resources",
				"accounts_processed", ingestResp.AccountsProcessed,
				"accounts_created", ingestResp.AccountsCreated,
				"total_resources", ingestResp.TotalResources,
			)
			if len(ingestResp.Errors) > 0 {
				p.logger.Warn("org ingest had errors", "errors", ingestResp.Errors)
			}
			return nil
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			var errBody struct {
				Error  string `json:"error"`
				Detail string `json:"detail"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&errBody)
			_ = resp.Body.Close()
			return fmt.Errorf("server rejected request (status %d): %s - %s", resp.StatusCode, errBody.Error, errBody.Detail)
		}

		_ = resp.Body.Close()
		lastErr = fmt.Errorf("server error (status %d)", resp.StatusCode)
		p.logger.Error("org push failed", "error", lastErr, "attempt", attempt+1)
	}

	return fmt.Errorf("org push failed after %d attempts: %w", maxRetries+1, lastErr)
}

// Heartbeat sends a heartbeat to the server.
func (p *Pusher) Heartbeat(ctx context.Context, name string, accountID int64, version, hostname string) error {
	req := domain.AgentHeartbeatRequest{
		AgentID:   p.agentID,
		Name:      name,
		AccountID: accountID,
		Version:   version,
		Hostname:  hostname,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal heartbeat request: %w", err)
	}

	url := p.serverURL + "/api/v1/discovery/agents/heartbeat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		p.logger.Debug("heartbeat sent", "agent_id", p.agentID)
		return nil
	}

	var errBody struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	return fmt.Errorf("heartbeat failed (status %d): %s - %s", resp.StatusCode, errBody.Error, errBody.Detail)
}
