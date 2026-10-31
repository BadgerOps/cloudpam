package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
)

// aiProviderTestTimeout bounds one provider test, completion and stream together.
const aiProviderTestTimeout = 15 * time.Second

var aiProviderTestMessages = []llm.Message{{Role: "user", Content: "Reply with the single word OK."}}

// handleTestAIProvider runs a short completion and a streaming completion
// against the effective provider and records the outcome. Provider failures
// are reported with 200 and ok:false; only store failures return 5xx.
func (ss *SettingsServer) handleTestAIProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider, resolved, err := ss.aiDeps().providers.Provider(ctx)
	if err != nil && !errors.Is(err, llm.ErrStoredKeyUnreadable) {
		ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to load ai provider settings", err.Error())
		return
	}
	resp := domain.AIProviderTestResponse{
		Endpoint: llm.RedactEndpoint(resolved.Config.Endpoint),
		Model:    resolved.Config.Model,
		TestedAt: time.Now().UTC(),
	}
	status := http.StatusOK
	defer func() {
		ss.logAudit(ctx, audit.ActionTest, audit.ResourceSettings, aiProviderSettingsID, aiProviderSettingsID, status)
	}()

	if provider == nil || !provider.Available() {
		resp.State = domain.AIProviderStateNotConfigured
		resp.Error = "no provider configured"
		writeJSON(w, status, resp)
		return
	}

	runAIProviderTest(ctx, provider, &resp)
	if err := ss.recordAIProviderTest(ctx, resolved.Stored, &resp); err != nil {
		status = http.StatusInternalServerError
		ss.writeErr(ctx, w, status, "failed to save ai provider test result", err.Error())
		return
	}
	writeJSON(w, status, resp)
}

// runAIProviderTest fills resp with the outcome. Errors are reduced to
// sanitized diagnostics; response bodies and keys never reach resp.
func runAIProviderTest(ctx context.Context, provider llm.Provider, resp *domain.AIProviderTestResponse) {
	ctx, cancel := context.WithTimeout(ctx, aiProviderTestTimeout)
	defer cancel()

	start := time.Now()
	_, err := provider.Complete(ctx, aiProviderTestMessages, llm.Options{MaxTokens: 8})
	resp.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		resp.State = domain.AIProviderStateFailing
		resp.Error = llm.DiagnoseError(err, aiProviderTestTimeout)
		return
	}
	resp.OK = true
	resp.State = domain.AIProviderStateVerified

	events, err := provider.StreamComplete(ctx, aiProviderTestMessages, llm.Options{MaxTokens: 8})
	if err != nil {
		return
	}
	received, failed := 0, false
	for evt := range events {
		if evt.Err != nil {
			failed = true
		} else {
			received++
		}
		if evt.Done {
			break
		}
	}
	resp.StreamingSupported = received > 0 && !failed
}

// recordAIProviderTest stores the outcome, unless the provider settings changed
// while the test ran: then the result describes a configuration that is no
// longer current and is not recorded.
func (ss *SettingsServer) recordAIProviderTest(ctx context.Context, tested domain.AIProviderSettings, resp *domain.AIProviderTestResponse) error {
	doc, err := ss.settingsStore.GetAIProviderSettings(ctx)
	if err != nil {
		return err
	}
	if doc.Endpoint != tested.Endpoint || doc.Model != tested.Model || doc.APIKeyEncrypted != tested.APIKeyEncrypted {
		return nil
	}
	testedAt := resp.TestedAt
	doc.State = resp.State
	doc.LastTestedAt = &testedAt
	doc.LastTestError = resp.Error
	doc.LastTestModel = resp.Model
	return ss.settingsStore.UpdateAIProviderSettings(ctx, doc)
}
