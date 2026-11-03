package domain

// ConnectivitySource names the identity a connectivity test ran as.
type ConnectivitySource string

const (
	// ConnectivitySourceHost is the server's own cloud identity (AWS default chain, GCP ADC).
	ConnectivitySourceHost ConnectivitySource = "host"
	// ConnectivitySourceStoredCredential is the account's stored credential.
	ConnectivitySourceStoredCredential ConnectivitySource = "stored_credential"
)

// ConnectivityTestResult is the response of POST /api/v1/accounts/{id}/test.
// The test runs as the identity a sync of the account would use.
type ConnectivityTestResult struct {
	CredentialTest
	LatencyMS int64              `json:"latency_ms"`
	Identity  string             `json:"identity,omitempty"` // empty on failure
	Source    ConnectivitySource `json:"source"`
}
