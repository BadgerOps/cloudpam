package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

const (
	aiProviderSettingsID = "ai_provider"

	// aiSettingsPatchAttempts bounds the compare-and-swap retries of one PATCH.
	aiSettingsPatchAttempts = 3

	errEphemeralAPIKey  = "cannot store an api key: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key or use CLOUDPAM_LLM_API_KEY"
	errUnreadableKey    = "stored api key cannot be decrypted; check CLOUDPAM_OIDC_ENCRYPTION_KEY or save the api key again"
	errEndpointNeedsKey = "re-enter api_key or set clear_api_key when changing the endpoint while a stored key exists"
)

// aiProviderDeps is the AI provider wiring for the settings endpoints.
type aiProviderDeps struct {
	providers        llm.ProviderSource
	resolver         *llm.Resolver
	encKey           []byte
	storageAvailable bool
}

// SetAIProvider wires the AI provider settings endpoints. storageAvailable is
// false when the encryption key is ephemeral, so API keys are refused.
func (ss *SettingsServer) SetAIProvider(src llm.ProviderSource, resolver *llm.Resolver, encKey []byte, storageAvailable bool) {
	ss.ai = aiProviderDeps{providers: src, resolver: resolver, encKey: encKey, storageAvailable: storageAvailable}
}

// aiDeps returns the configured wiring, defaulting to an environment-backed
// resolver over the settings store that cannot decrypt or store keys.
func (ss *SettingsServer) aiDeps() aiProviderDeps {
	deps := ss.ai
	if deps.resolver == nil {
		deps.resolver = llm.NewResolver(llm.ConfigFromEnv(), ss.settingsStore, nil)
	}
	if deps.providers == nil {
		deps.providers = llm.NewManager(deps.resolver)
	}
	return deps
}

func (ss *SettingsServer) handleGetAIProviderSettings(w http.ResponseWriter, r *http.Request) {
	ss.writeAISettings(w, r)
}

// writeAISettings writes the current GET representation.
func (ss *SettingsServer) writeAISettings(w http.ResponseWriter, r *http.Request) {
	resp, err := ss.aiSettingsResponse(r)
	if err != nil {
		ss.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to load ai provider settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// aiSettingsResponse resolves the effective configuration from the store.
func (ss *SettingsServer) aiSettingsResponse(r *http.Request) (*domain.AIProviderSettingsResponse, error) {
	deps := ss.aiDeps()
	resolved, err := deps.resolver.Resolve(r.Context())
	keyUnreadable := errors.Is(err, llm.ErrStoredKeyUnreadable)
	if err != nil && !keyUnreadable {
		return nil, err
	}
	cfg := resolved.Config
	resp := &domain.AIProviderSettingsResponse{
		Endpoint:               llm.RedactEndpoint(cfg.Endpoint),
		Model:                  cfg.Model,
		MaxTokens:              cfg.MaxTokens,
		Temperature:            cfg.Temperature,
		APIKeyConfigured:       resolved.APIKeyConfigured,
		APIKeyStorageAvailable: deps.storageAvailable,
		Sources:                resolved.Sources,
		LastTestedAt:           resolved.Stored.LastTestedAt,
		LastTestError:          resolved.Stored.LastTestError,
		LastTestModel:          resolved.Stored.LastTestModel,
	}
	switch {
	case keyUnreadable:
		resp.State = domain.AIProviderStateFailing
		resp.LastTestError = errUnreadableKey
	case !resolved.APIKeyConfigured && strings.TrimSpace(cfg.Endpoint) == "":
		resp.State = domain.AIProviderStateNotConfigured
	case testResultStale(resolved):
		// The last test ran against another endpoint, model or key source
		// (typically CLOUDPAM_LLM_* changed across a restart), so its verdict
		// does not describe the current provider. When it was recorded is
		// still worth showing.
		resp.State = domain.AIProviderStateConfigured
		resp.LastTestError = ""
	case resolved.Stored.State != "":
		resp.State = resolved.Stored.State
	default:
		resp.State = domain.AIProviderStateConfigured
	}
	return resp, nil
}

// testResultStale reports a stored verified or failing result whose
// fingerprint no longer matches the effective configuration. Documents written
// before fingerprints were recorded have none and keep their state.
func testResultStale(resolved llm.Resolved) bool {
	stored := resolved.Stored
	if stored.State != domain.AIProviderStateVerified && stored.State != domain.AIProviderStateFailing {
		return false
	}
	return stored.LastTestFingerprint != "" && stored.LastTestFingerprint != resolved.Fingerprint()
}

func (ss *SettingsServer) handleUpdateAIProviderSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req domain.UpdateAIProviderSettingsRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	// A misspelled field ("apikey") would otherwise be ignored and the caller
	// would believe the value was saved.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		detail := "expected a json object"
		if msg := err.Error(); strings.HasPrefix(msg, "json: unknown field") {
			detail = strings.TrimPrefix(msg, "json: ") // unknown field "apikey"
		}
		ss.writeErr(ctx, w, http.StatusBadRequest, "invalid request body", detail)
		return
	}
	if field, detail := validateAIProviderUpdate(&req); field != "" {
		ss.writeErr(ctx, w, http.StatusBadRequest, "invalid "+field, detail)
		return
	}
	deps := ss.aiDeps()
	if req.APIKey != nil && !deps.storageAvailable {
		ss.writeErr(ctx, w, http.StatusConflict, errEphemeralAPIKey, "")
		return
	}

	var encrypted string
	if req.APIKey != nil {
		var err error
		if encrypted, err = secrets.Encrypt(strings.TrimSpace(*req.APIKey), deps.encKey); err != nil {
			// The error never contains the key; keep the response generic anyway.
			ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to encrypt api key", "")
			return
		}
	}

	// Read, apply and write with a compare-and-swap on UpdatedAt, so a PATCH
	// or test result that lands in between is re-applied over rather than
	// overwritten. A non-material PATCH would otherwise erase a just-recorded
	// test result.
	for attempt := 1; ; attempt++ {
		doc, err := ss.settingsStore.GetAIProviderSettings(ctx)
		if err != nil {
			ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to load ai provider settings", err.Error())
			return
		}
		// The stored key is bound to the stored endpoint: moving the endpoint must
		// not silently send that key to the new host.
		if req.Endpoint != nil && strings.TrimSpace(*req.Endpoint) != doc.Endpoint &&
			doc.APIKeyEncrypted != "" && req.APIKey == nil && !req.ClearAPIKey {
			ss.writeErr(ctx, w, http.StatusBadRequest, "invalid endpoint", errEndpointNeedsKey)
			return
		}
		asRead := doc.UpdatedAt
		changed, material := applyAIProviderUpdate(doc, &req, encrypted)
		if len(changed) == 0 {
			// Nothing to store: keep UpdatedAt (and so any in-flight test result)
			// and the audit log untouched.
			ss.writeAISettings(w, r)
			return
		}
		stampAIProviderUpdate(doc, material)
		err = ss.settingsStore.UpdateAIProviderSettingsIfUnchanged(ctx, asRead, doc)
		switch {
		case err == nil:
			ss.logAuditWithChanges(ctx, audit.ActionUpdate, audit.ResourceSettings, aiProviderSettingsID, aiProviderSettingsID,
				&audit.Changes{After: map[string]any{"changed": changed}}, http.StatusOK)
			ss.writeAISettings(w, r)
			return
		case errors.Is(err, storage.ErrConflict) && attempt < aiSettingsPatchAttempts:
			continue
		case errors.Is(err, storage.ErrConflict):
			ss.writeErr(ctx, w, http.StatusConflict, "ai provider settings changed concurrently", "reload and retry")
			return
		default:
			ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to save ai provider settings", err.Error())
			return
		}
	}
}

// stampAIProviderUpdate resets the test result when the change affects which
// provider is reached and moves UpdatedAt forward. UpdatedAt is the
// compare-and-swap token for PATCH and test results, so every stored change
// moves it, even within the same clock tick.
func stampAIProviderUpdate(doc *domain.AIProviderSettings, material bool) {
	if material {
		doc.State = domain.AIProviderStateConfigured
		doc.LastTestedAt = nil
		doc.LastTestError = ""
		doc.LastTestModel = ""
		doc.LastTestFingerprint = ""
	}
	now := time.Now().UTC()
	if !now.After(doc.UpdatedAt) {
		now = doc.UpdatedAt.Add(time.Nanosecond)
	}
	doc.UpdatedAt = now
}

// applyAIProviderUpdate applies req to doc. It returns the names of the fields
// whose stored value changed (never the values) and whether any change affects
// which provider is reached (endpoint, model, key), which invalidates the last
// test result.
func applyAIProviderUpdate(doc *domain.AIProviderSettings, req *domain.UpdateAIProviderSettingsRequest, encryptedKey string) ([]string, bool) {
	changed := []string{}
	material := false
	if req.Endpoint != nil {
		if v := strings.TrimSpace(*req.Endpoint); v != doc.Endpoint {
			doc.Endpoint = v
			changed, material = append(changed, "endpoint"), true
		}
	}
	if req.Model != nil {
		if v := strings.TrimSpace(*req.Model); v != doc.Model {
			doc.Model = v
			changed, material = append(changed, "model"), true
		}
	}
	if req.MaxTokens != nil && *req.MaxTokens != doc.MaxTokens {
		doc.MaxTokens = *req.MaxTokens
		changed = append(changed, "max_tokens")
	}
	switch {
	case req.ClearTemperature && doc.Temperature != nil:
		doc.Temperature = nil
		changed = append(changed, "temperature")
	case req.Temperature != nil && (doc.Temperature == nil || *doc.Temperature != *req.Temperature):
		t := *req.Temperature
		doc.Temperature = &t
		changed = append(changed, "temperature")
	}
	switch {
	case req.APIKey != nil:
		doc.APIKeyEncrypted = encryptedKey
		changed, material = append(changed, "api_key"), true
	case req.ClearAPIKey && doc.APIKeyEncrypted != "":
		doc.APIKeyEncrypted = ""
		changed, material = append(changed, "api_key"), true
	}
	return changed, material
}
