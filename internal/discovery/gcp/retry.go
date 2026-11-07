package gcp

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"cloudpam/internal/discovery"
)

const (
	maxAttempts   = 4
	maxRetryAfter = 30 * time.Second
	maxBodyBytes  = 10 << 20
)

// retryBaseDelay is the first backoff; tests shorten it.
var retryBaseDelay = time.Second

// doRetrying sends req and decodes a 200 response's JSON body into dest,
// retrying rate limiting, 5xx gateway failures and transport errors up to
// maxAttempts times. A body cut off midway counts as a transport error; one
// that arrives whole but does not decode is not retried. Every attempt waits
// on limiter first. It returns nil, or the last error: a *discovery.HTTPError
// for a status, else the transport, decode, limiter or context error. req's
// body must be replayable through GetBody, as http.NewRequest arranges for
// in-memory readers.
func doRetrying(client *http.Client, limiter *discovery.Limiter, req *http.Request, dest any) error {
	ctx := req.Context()
	for attempt := 1; ; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
		attemptReq := req
		if attempt > 1 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return err
			}
			attemptReq = req.Clone(ctx)
			attemptReq.Body = body
		}

		resp, err := client.Do(attemptReq)
		var body []byte
		if err == nil && resp.StatusCode == http.StatusOK {
			body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
			_ = resp.Body.Close()
			if err != nil {
				err = fmt.Errorf("read response: %w", err)
			}
		}
		wait := time.Duration(-1)
		switch {
		case err != nil:
			if ctx.Err() != nil || attempt == maxAttempts {
				return err
			}
		case resp.StatusCode == http.StatusOK:
			if len(body) > maxBodyBytes {
				return fmt.Errorf("response body exceeds %d bytes", maxBodyBytes)
			}
			if err := json.Unmarshal(body, dest); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			return nil
		default:
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			if !retryableStatus(resp.StatusCode) || attempt == maxAttempts {
				return &discovery.HTTPError{Status: resp.StatusCode}
			}
			wait = retryAfter(resp.Header)
		}
		if wait < 0 {
			wait = retryBaseDelay << (attempt - 1)
			wait += time.Duration(rand.Int64N(int64(wait)/5 + 1))
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter reads a Retry-After given in seconds, capped at maxRetryAfter. It
// returns -1 when the header is absent or is an HTTP date.
func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs < 0 {
		return -1
	}
	return time.Duration(min(secs, int(maxRetryAfter/time.Second))) * time.Second
}
