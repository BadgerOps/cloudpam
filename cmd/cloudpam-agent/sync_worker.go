package main

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"
)

// syncWorker serializes discovery passes so the scheduler loop never blocks
// on one. Scheduled runs coalesce into a single pending flag; server-requested
// jobs queue FIFO because the server keeps each job open until an ingest names it.
type syncWorker struct {
	wake chan struct{}

	mu        sync.Mutex
	scheduled bool
	requested []uuid.UUID
	running   *uuid.UUID
	active    bool
}

func newSyncWorker() *syncWorker {
	return &syncWorker{wake: make(chan struct{}, 1)}
}

func (w *syncWorker) schedule() {
	w.mu.Lock()
	w.scheduled = true
	w.mu.Unlock()
	w.signal()
}

func (w *syncWorker) request(job uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if (w.running != nil && *w.running == job) || slices.Contains(w.requested, job) {
		return
	}
	w.requested = append(w.requested, job)
	w.signal()
}

func (w *syncWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// next claims the next pass: the oldest requested job, else a pending
// scheduled run (nil job). Any pass clears the scheduled flag because it
// refreshes the same inventory.
func (w *syncWorker) next() (*uuid.UUID, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.requested) > 0 {
		job := w.requested[0]
		w.requested = w.requested[1:]
		w.running = &job
		w.scheduled = false
		w.active = true
		return &job, true
	}
	if w.scheduled {
		w.scheduled = false
		w.active = true
		return nil, true
	}
	return nil, false
}

func (w *syncWorker) finish() {
	w.mu.Lock()
	w.running = nil
	w.active = false
	w.mu.Unlock()
}

// busy reports whether a pass is running or a requested job is waiting. A job
// claimed now would wait in memory only, and be lost if the agent stopped.
func (w *syncWorker) busy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active || len(w.requested) > 0
}

// run executes passes one at a time until ctx is cancelled. pass must itself
// observe ctx so run can return promptly.
func (w *syncWorker) run(ctx context.Context, pass func(syncJobID *uuid.UUID)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}
		for ctx.Err() == nil {
			job, ok := w.next()
			if !ok {
				break
			}
			pass(job)
			w.finish()
		}
	}
}
