package audit

import (
	"sync"
	"time"

	"cloudpam/internal/domain"
)

// deliveryHealth is the outcome of the latest sends to one sink. Sends run
// concurrently from request handlers, so the mutex keeps the counters and the
// latest timestamps consistent with each other.
type deliveryHealth struct {
	mu          sync.Mutex
	delivered   uint64
	failed      uint64
	lastSuccess time.Time
	lastFailure time.Time
	lastError   string
	lastFailed  bool
}

func (h *deliveryHealth) record(at time.Time, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastFailed = err != nil
	if err == nil {
		h.delivered++
		h.lastSuccess = at
		return
	}
	h.failed++
	h.lastFailure = at
	h.lastError = err.Error()
}

func (h *deliveryHealth) fill(d *domain.LogDestination) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d.Delivered = h.delivered
	d.Failed = h.failed
	d.LastError = h.lastError
	if !h.lastSuccess.IsZero() {
		t := h.lastSuccess
		d.LastSuccessAt = &t
	}
	if !h.lastFailure.IsZero() {
		t := h.lastFailure
		d.LastFailureAt = &t
	}
	switch {
	case h.delivered == 0 && h.failed == 0:
		d.State = domain.LogDestinationStateIdle
	case h.lastFailed:
		d.State = domain.LogDestinationStateFailing
	default:
		d.State = domain.LogDestinationStateOK
	}
}
