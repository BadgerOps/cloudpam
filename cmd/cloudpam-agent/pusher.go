package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
func (p *Pusher) PushResources(ctx context.Context, accountID int64, resources []domain.DiscoveredResource, syncJobID *uuid.UUID, maxRetries int, backoff time.Duration) error {
	req := domain.IngestRequest{
		AccountID: accountID,
		Resources: resources,
		AgentID:   &p.agentID,
		SyncJobID: syncJobID,
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
	if req.AgentID == "" {
		req.AgentID = p.agentID.String()
	}

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
			if ingestResp.AccountsProcessed == 0 && len(ingestResp.Errors) > 0 && len(req.Accounts) > 0 {
				return orgIngestRejected(len(req.Accounts), ingestResp.Errors)
			}
			// A server before 0.25.0 ignores failures and would complete the job.
			if len(req.Failures) > 0 && ingestResp.AccountsFailed != len(req.Failures) {
				return fmt.Errorf("server did not acknowledge %d account failure(s); upgrade the CloudPAM server to 0.25.0 or later before upgrading agents", len(req.Failures))
			}
			if len(ingestResp.Errors) > 0 {
				p.logger.Error("org ingest had errors", "errors", ingestResp.Errors)
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

// orgIngestRejected reports a 2xx bulk ingest that processed none of the
// accounts sent. A server older than 0.25.0 answers GCP entries this way,
// because it requires aws_account_id on every entry.
func orgIngestRejected(accounts int, errs []string) error {
	msg := fmt.Sprintf("server accepted none of the %d accounts: %s", accounts, strings.Join(errs[:min(3, len(errs))], "; "))
	for _, e := range errs {
		// Matches the old server's "skipped account with empty aws_account_id".
		if strings.Contains(e, "aws_account_id") {
			msg += " (upgrade the CloudPAM server to 0.25.0 or later before GCP agents)"
			break
		}
	}
	return errors.New(msg)
}

// Heartbeat sends a heartbeat to the server. claimSync false asks the server
// to keep any pending sync job for a later heartbeat.
func (p *Pusher) Heartbeat(ctx context.Context, name string, accountID int64, version, hostname string, claimSync bool) (*domain.AgentHeartbeatResponse, error) {
	req := domain.AgentHeartbeatRequest{
		AgentID:   p.agentID,
		Name:      name,
		AccountID: accountID,
		Version:   version,
		Hostname:  hostname,
		ClaimSync: &claimSync,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal heartbeat request: %w", err)
	}

	url := p.serverURL + "/api/v1/discovery/agents/heartbeat"
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
		var heartbeatResp domain.AgentHeartbeatResponse
		if err := json.NewDecoder(resp.Body).Decode(&heartbeatResp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		p.logger.Debug("heartbeat sent", "agent_id", p.agentID)
		return &heartbeatResp, nil
	}

	var errBody struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	return nil, fmt.Errorf("heartbeat failed (status %d): %s - %s", resp.StatusCode, errBody.Error, errBody.Detail)
}
