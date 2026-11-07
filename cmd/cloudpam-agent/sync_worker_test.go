package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func jobOf(t *testing.T, w *syncWorker) *uuid.UUID {
	t.Helper()
	job, ok := w.next()
	if !ok {
		t.Fatal("next() = no pass, want one")
	}
	return job
}

func TestSyncWorkerCoalescesScheduledRuns(t *testing.T) {
	w := newSyncWorker()
	w.schedule()
	if job := jobOf(t, w); job != nil {
		t.Fatalf("first pass job = %v, want scheduled (nil)", job)
	}
	for range 5 {
		w.schedule()
	}
	w.finish()
	if job := jobOf(t, w); job != nil {
		t.Fatalf("second pass job = %v, want scheduled (nil)", job)
	}
	w.finish()
	if _, ok := w.next(); ok {
		t.Fatal("ticks during one pass produced more than one follow-up pass")
	}
}

func TestSyncWorkerRunsRequestedJobsFIFOBeforeScheduled(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	w := newSyncWorker()
	w.schedule()
	_ = jobOf(t, w)
	w.schedule()
	w.request(a)
	w.request(a)
	w.request(b)
	w.finish()

	if job := jobOf(t, w); job == nil || *job != a {
		t.Fatalf("pass = %v, want %s", job, a)
	}
	w.request(a)
	w.finish()
	if job := jobOf(t, w); job == nil || *job != b {
		t.Fatalf("pass = %v, want %s", job, b)
	}
	w.finish()
	if _, ok := w.next(); ok {
		t.Fatal("a pending scheduled run survived requested passes, want it cleared")
	}
}

func TestSyncWorkerRequestedJobClearsPendingScheduled(t *testing.T) {
	a := uuid.New()
	w := newSyncWorker()
	w.schedule()
	w.request(a)
	if job := jobOf(t, w); job == nil || *job != a {
		t.Fatalf("pass = %v, want %s", job, a)
	}
	w.finish()
	if _, ok := w.next(); ok {
		t.Fatal("scheduled run still pending after a requested pass refreshed the inventory")
	}
}

func TestSyncWorkerRunsOnePassAtATimeAndExitsOnCancel(t *testing.T) {
	w := newSyncWorker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan *uuid.UUID, 8)
	release := make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	pass := func(job *uuid.UUID) {
		n := inFlight.Add(1)
		if n > maxInFlight.Load() {
			maxInFlight.Store(n)
		}
		started <- job
		<-release
		inFlight.Add(-1)
	}
	exited := make(chan struct{})
	go func() {
		w.run(ctx, pass)
		close(exited)
	}()

	a := uuid.New()
	w.schedule()
	if job := <-started; job != nil {
		t.Fatalf("first pass job = %v, want scheduled", job)
	}
	w.request(a)
	w.schedule()
	release <- struct{}{}
	if job := <-started; job == nil || *job != a {
		t.Fatalf("second pass job = %v, want %s", job, a)
	}
	cancel()
	close(release)

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit after cancellation")
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("max concurrent passes = %d, want 1", got)
	}
}
