package api

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning/llm"
)

// validateAIProviderUpdate checks a PATCH body. It returns the offending field
// and an actionable detail, or "" when the request is valid. Details never echo
// the submitted api key.
func validateAIProviderUpdate(req *domain.UpdateAIProviderSettingsRequest) (string, string) {
	if req.Endpoint != nil {
		if detail := validateAIEndpoint(strings.TrimSpace(*req.Endpoint)); detail != "" {
			return "endpoint", detail
		}
	}
	if req.Model != nil && len(strings.TrimSpace(*req.Model)) > domain.MaxAIProviderModelLength {
		return "model", fmt.Sprintf("must be at most %d characters", domain.MaxAIProviderModelLength)
	}
	if req.MaxTokens != nil && (*req.MaxTokens < 0 || *req.MaxTokens > domain.MaxAIProviderMaxTokens) {
		return "max_tokens", fmt.Sprintf("must be 0 (use the default) or between 1 and %d", domain.MaxAIProviderMaxTokens)
	}
	if req.Temperature != nil {
		if req.ClearTemperature {
			return "temperature", "send either temperature or clear_temperature, not both"
		}
		if t := *req.Temperature; math.IsNaN(t) || t < 0 || t > domain.MaxAIProviderTemperature {
			return "temperature", "must be between 0 and 2"
		}
	}
	if req.APIKey != nil {
		if req.ClearAPIKey {
			return "api_key", "send either api_key or clear_api_key, not both"
		}
		if strings.TrimSpace(*req.APIKey) == "" {
			return "api_key", "must not be empty; omit it to keep the stored key or set clear_api_key to remove it"
		}
	}
	return "", ""
}

// validateAIEndpoint accepts "" (clear) or an absolute http(s) URL without a
// query, fragment or embedded credentials.
func validateAIEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	const want = "must be an absolute http or https url without a query or fragment, e.g. http://ollama:11434/v1"
	u, err := url.Parse(endpoint)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return want
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return want
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(endpoint, "#") {
		return want
	}
	if u.User != nil {
		return "must not embed credentials; set api_key instead"
	}
	if isMetadataOrLinkLocal(u.Hostname()) {
		return "link-local and metadata addresses are not allowed"
	}
	return ""
}

// isMetadataOrLinkLocal reports a literal IP host that llm.IsBlockedAddr
// refuses (link-local ranges, which hold the cloud metadata services, the EC2
// IPv6 metadata address and the unspecified address) or the GCE metadata host
// name. Loopback and private ranges stay allowed for local Ollama or vLLM.
// Other host names are not resolved here; the provider's dialer refuses them
// at connect time when they resolve to a blocked address.
func isMetadataOrLinkLocal(host string) bool {
	return llm.IsBlockedHost(host)
}
