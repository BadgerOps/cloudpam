package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/storage"
)

// aiProviderTestTimeout bounds one provider test, completion and stream together.
const aiProviderTestTimeout = 15 * time.Second

var aiProviderTestMessages = []llm.Message{{Role: "user", Content: "Reply with the single word OK."}}

// handleTestAIProvider runs a short completion and a streaming completion
// against the effective provider and records the outcome. Provider failures
// are reported with 200 and ok:false; only store failures return 5xx.
func (ss *SettingsServer) handleTestAIProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Read the stored document before resolving the provider: its UpdatedAt is
	// the compare-and-swap token that keeps the result from overwriting a PATCH
	// that lands while the test runs.
	before, err := ss.settingsStore.GetAIProviderSettings(ctx)
	if err != nil {
		ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to load ai provider settings", err.Error())
		return
	}
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
	if ctx.Err() != nil {
		// The client went away; the result may reflect the cancellation
		// rather than the provider, so it is returned but not stored.
		writeJSON(w, status, resp)
		return
	}
	if err := ss.recordAIProviderTest(ctx, before, resolved.Fingerprint(), &resp); err != nil {
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

// recordAIProviderTest stores the outcome on the document read before the
// test, with a compare-and-swap on its UpdatedAt. If the settings changed while
// the test ran, the result describes a configuration that is no longer current
// and the write would revert that change, so it is not recorded. fingerprint
// is llm.Resolved.Fingerprint of the configuration the test used, so the
// result is reported only while that configuration is still in effect.
func (ss *SettingsServer) recordAIProviderTest(ctx context.Context, before *domain.AIProviderSettings, fingerprint string, resp *domain.AIProviderTestResponse) error {
	doc := before.Clone()
	testedAt := resp.TestedAt
	doc.State = resp.State
	doc.LastTestedAt = &testedAt
	doc.LastTestError = resp.Error
	doc.LastTestModel = resp.Model
	doc.LastTestFingerprint = fingerprint
	err := ss.settingsStore.UpdateAIProviderSettingsIfUnchanged(ctx, before.UpdatedAt, doc)
	if errors.Is(err, storage.ErrConflict) {
		ss.logger.InfoContext(ctx, "ai provider settings changed during test; result not stored")
		return nil
	}
	return err
}
