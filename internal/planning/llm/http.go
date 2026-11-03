package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// postJSON sends body as JSON to url with headers and returns the response
// when its status is 200. Any other status is reported as a StatusError with
// the body discarded, so provider error bodies never reach callers. The
// discard is bounded in time: once headers arrive ResponseHeaderTimeout no
// longer applies, and a 5xx whose body stalls would otherwise hold the
// request until ctx ends, after which the chain no longer falls back.
func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body any) (*http.Response, error) {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		drainStream(resp.Body)
		_ = resp.Body.Close()
		return nil, &StatusError{Status: resp.StatusCode}
	}
	return resp, nil
}

// readSSE calls fn with the payload of every "data:" line in r, in order,
// until fn returns false or r ends. Other SSE lines (event names, comments,
// blank separators) are skipped: both providers put the event type in the
// payload. It returns nil when fn stopped the read or r ended cleanly, and
// the read error otherwise.
func readSSE(r io.Reader, fn func(data string) bool) error {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			if !fn(strings.TrimSpace(data)) {
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
