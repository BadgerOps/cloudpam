package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

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
	fallbacks := make([]domain.AIFallbackProviderResponse, 0, len(resolved.Fallbacks))
	for _, fb := range resolved.Fallbacks {
		fallbacks = append(fallbacks, domain.AIFallbackProviderResponse{
			ID:               fb.ID,
			Provider:         fb.Config.Provider,
			Endpoint:         llm.RedactEndpoint(fb.Config.Endpoint),
			Model:            fb.Config.Model,
			APIKeyConfigured: fb.APIKeyConfigured,
		})
	}
	resp := &domain.AIProviderSettingsResponse{
		Provider:               cfg.Provider,
		Fallbacks:              fallbacks,
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
	if !ss.decodeStrictJSON(w, r, &req) {
		return
	}
	if field, detail := validateAIProviderUpdate(&req); field != "" {
		ss.writeErr(ctx, w, http.StatusBadRequest, "invalid "+field, detail)
		return
	}
	deps := ss.aiDeps()
	if (req.APIKey != nil || fallbackKeysSupplied(req.Fallbacks)) && !deps.storageAvailable {
		ss.writeErr(ctx, w, http.StatusConflict, errEphemeralAPIKey, "")
		return
	}

	encrypt := func(key string) (string, bool) {
		ct, err := secrets.Encrypt(strings.TrimSpace(key), deps.encKey)
		if err != nil {
			// The error never contains the key; keep the response generic anyway.
			ss.writeErr(ctx, w, http.StatusInternalServerError, "failed to encrypt api key", "")
			return "", false
		}
		return ct, true
	}
	var encrypted string
	if req.APIKey != nil {
		var ok bool
		if encrypted, ok = encrypt(*req.APIKey); !ok {
			return
		}
	}
	var encryptedFallbacks []string
	if req.Fallbacks != nil {
		encryptedFallbacks = make([]string, len(*req.Fallbacks))
		for i, fb := range *req.Fallbacks {
			if fb.APIKey == nil {
				continue
			}
			var ok bool
			if encryptedFallbacks[i], ok = encrypt(*fb.APIKey); !ok {
				return
			}
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
		// The stored key is bound to the stored endpoint and kind: moving either
		// must not silently send that key to the new host.
		endpointMoves := req.Endpoint != nil && strings.TrimSpace(*req.Endpoint) != doc.Endpoint
		kindMoves := req.Provider != nil && strings.ToLower(strings.TrimSpace(*req.Provider)) != doc.Provider
		if (endpointMoves || kindMoves) && doc.APIKeyEncrypted != "" && req.APIKey == nil && !req.ClearAPIKey {
			field := "endpoint"
			if kindMoves {
				field = "provider"
			}
			ss.writeErr(ctx, w, http.StatusBadRequest, "invalid "+field, errEndpointNeedsKey)
			return
		}
		asRead := doc.UpdatedAt
		changed, material := applyAIProviderUpdate(doc, &req, encrypted)
		if req.Fallbacks != nil {
			fallbacksChanged, detail := applyFallbackUpdate(doc, *req.Fallbacks, encryptedFallbacks)
			if detail != "" {
				ss.writeErr(ctx, w, http.StatusBadRequest, "invalid fallbacks", detail)
				return
			}
			if fallbacksChanged {
				changed = append(changed, "fallbacks")
			}
		}
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
	if req.Provider != nil {
		if v := strings.ToLower(strings.TrimSpace(*req.Provider)); v != doc.Provider {
			doc.Provider = v
			changed, material = append(changed, "provider"), true
		}
	}
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

// fallbackKeysSupplied reports whether any entry of a PATCH fallback list
// carries an api_key, which needs durable key storage.
func fallbackKeysSupplied(fallbacks *[]domain.AIFallbackProviderUpdate) bool {
	if fallbacks == nil {
		return false
	}
	for _, fb := range *fallbacks {
		if fb.APIKey != nil {
			return true
		}
	}
	return false
}

// applyFallbackUpdate replaces doc.Fallbacks with the request list, in its
// order. An entry addressed by id keeps its stored key unless the request
// replaces or clears it; a new entry gets an id. encrypted holds the
// ciphertext for each entry that supplied a key. It returns whether the
// stored list changed, or an actionable detail when the request cannot be
// applied against the stored document (an unknown id, a key that would move
// to another host, or a provider that cannot run without a key).
func applyFallbackUpdate(doc *domain.AIProviderSettings, updates []domain.AIFallbackProviderUpdate, encrypted []string) (bool, string) {
	stored := make(map[string]domain.AIFallbackProvider, len(doc.Fallbacks))
	for _, fb := range doc.Fallbacks {
		stored[fb.ID] = fb
	}
	next := make([]domain.AIFallbackProvider, 0, len(updates))
	for i, u := range updates {
		entry := domain.AIFallbackProvider{
			ID:       strings.TrimSpace(u.ID),
			Provider: strings.ToLower(strings.TrimSpace(u.Provider)),
			Endpoint: strings.TrimSpace(u.Endpoint),
			Model:    strings.TrimSpace(u.Model),
		}
		if entry.ID == "" {
			entry.ID = uuid.New().String()
		} else {
			prev, ok := stored[entry.ID]
			if !ok {
				return false, fmt.Sprintf("fallback %d: unknown id %q; omit the id to add a new fallback", i+1, entry.ID)
			}
			delete(stored, entry.ID)
			moves := prev.Endpoint != entry.Endpoint || prev.Provider != entry.Provider
			if moves && prev.APIKeyEncrypted != "" && u.APIKey == nil && !u.ClearAPIKey {
				return false, fmt.Sprintf("fallback %d: re-enter api_key or set clear_api_key when changing the endpoint or provider of a fallback with a stored key", i+1)
			}
			if u.APIKey == nil && !u.ClearAPIKey {
				entry.APIKeyEncrypted = prev.APIKeyEncrypted
			}
		}
		if u.APIKey != nil {
			entry.APIKeyEncrypted = encrypted[i]
		}
		switch {
		case entry.Provider == llm.ProviderAnthropic && entry.APIKeyEncrypted == "":
			return false, fmt.Sprintf("fallback %d: an anthropic fallback needs api_key", i+1)
		case entry.Endpoint == "" && entry.APIKeyEncrypted == "":
			return false, fmt.Sprintf("fallback %d: needs api_key, or an endpoint for an authless OpenAI-compatible server", i+1)
		}
		next = append(next, entry)
	}
	changed := len(next) != len(doc.Fallbacks)
	for i := 0; !changed && i < len(next); i++ {
		changed = next[i] != doc.Fallbacks[i]
	}
	if changed {
		doc.Fallbacks = next
	}
	return changed, ""
}
