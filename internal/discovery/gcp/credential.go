package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

var (
	_ discovery.CredentialAware       = (*Collector)(nil)
	_ discovery.CredentialedCollector = (*serviceAccountCollector)(nil)
)

// WithCredential binds the collector to a service account.
func (c *Collector) WithCredential(ctx context.Context, _ domain.Account, cred discovery.CredentialPayload) (discovery.CredentialedCollector, error) {
	sa, ok := cred.(discovery.GCPServiceAccount)
	if !ok {
		return nil, fmt.Errorf("%w: %s credential on the gcp collector", discovery.ErrCredentialProviderMismatch, cred.Type())
	}
	conf := &jwt.Config{
		Email:        sa.ClientEmail(),
		PrivateKey:   sa.PrivateKey(),
		PrivateKeyID: sa.PrivateKeyID(),
		Scopes:       []string{computeScope},
		TokenURL:     google.JWTTokenURL,
	}
	// The jwt token source posts with a client taken from this context, not from the
	// Token() caller, so the probe context and the injected base client go in here.
	tokenHTTP := &tokenTransport{ctx: ctx, base: http.DefaultTransport}
	tokenClient := &http.Client{Transport: tokenHTTP}
	if c.httpClient != nil {
		if c.httpClient.Transport != nil {
			tokenHTTP.base = c.httpClient.Transport
		}
		tokenClient.Timeout = c.httpClient.Timeout
	}
	tokenCtx := context.WithValue(ctx, oauth2.HTTPClient, tokenClient)
	return &serviceAccountCollector{
		Collector: &Collector{tokenSource: conf.TokenSource(tokenCtx), httpClient: c.httpClient, limiter: c.limiter},
		email:     sa.ClientEmail(),
		token:     tokenHTTP,
	}, nil
}

// tokenTransport keeps the transport error, which the oauth2 jwt source otherwise
// flattens into text.
type tokenTransport struct {
	ctx     context.Context
	base    http.RoundTripper
	lastErr error
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// AfterFunc runs cancel on its own goroutine even when t.ctx is already done, so
	// without this check a request can leave before that cancel lands.
	if err := t.ctx.Err(); err != nil {
		t.lastErr = err
		return nil, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	release := func() { stop(); cancel() }
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	t.lastErr = err
	if err != nil {
		release()
		return nil, err
	}
	resp.Body = releaseOnClose{ReadCloser: resp.Body, release: release}
	return resp, nil
}

type releaseOnClose struct {
	io.ReadCloser
	release func()
}

func (b releaseOnClose) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

type serviceAccountCollector struct {
	*Collector
	email string
	token *tokenTransport
}

func (c *serviceAccountCollector) Identity(ctx context.Context, account domain.Account) (string, error) {
	project := ProjectID(account)
	if project == "" {
		return "", &discovery.ProbeError{Step: discovery.ProbeStepGetProject, Code: "no_project_id"}
	}
	if _, err := c.tokenSource.Token(); err != nil {
		return "", c.tokenProbeError(err)
	}
	client, err := c.getHTTPClient(ctx)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/projects/%s", computeBaseURL, project), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get_project: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &discovery.ProbeError{Step: discovery.ProbeStepGetProject, Status: resp.StatusCode}
	}
	return c.email, nil
}

// Discover reduces a rejected token request to a ProbeError, because the
// RetrieveError text carries the token endpoint's response body.
func (c *serviceAccountCollector) Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	resources, err := c.Collector.Discover(ctx, account)
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		return resources, c.tokenProbeError(err)
	}
	return resources, err
}

func (c *serviceAccountCollector) tokenProbeError(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		pe := &discovery.ProbeError{Step: discovery.ProbeStepToken, Code: re.ErrorCode}
		if re.Response != nil {
			pe.Status = re.Response.StatusCode
		}
		if pe.Code == "" {
			var body struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(re.Body, &body)
			pe.Code = body.Error
		}
		return pe
	}
	if c.token.lastErr != nil {
		return fmt.Errorf("token: %w", c.token.lastErr)
	}
	return fmt.Errorf("token: %w", err)
}
