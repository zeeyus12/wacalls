package main

import (
	"testing"
	"time"
)

// A registry entry with no call object can never be a live call: it must be reported stale so it
// stops counting against max-calls-per-session.
func TestStaleReasonNoCall(t *testing.T) {
	if staleReason(nil, time.Now()) == "" {
		t.Fatal("nil entry should be stale")
	}
	if staleReason(&activeCall{startedAt: time.Now()}, time.Now()) == "" {
		t.Fatal("entry without a call should be stale")
	}
}

func TestRegistrySnapshotIsACopy(t *testing.T) {
	r := newCallRegistry()
	r.add("a", &activeCall{startedAt: time.Now()})
	r.add("b", &activeCall{startedAt: time.Now()})
	snap := r.snapshot()
	if len(snap) != 2 {
		t.Fatalf("want 2 entries, got %d", len(snap))
	}
	r.remove("a")
	if len(snap) != 2 {
		t.Fatal("snapshot must not change when the registry does")
	}
	if r.count() != 1 {
		t.Fatalf("registry should have 1 entry, has %d", r.count())
	}
}
