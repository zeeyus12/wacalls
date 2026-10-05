package main

import (
	"time"

	meowcaller "github.com/purpshell/meowcaller"
)

const (
	// A call that has not reached "connecting" this long after it was registered is dead.
	maxPreConnectAge = 2 * time.Minute
	// Nothing legitimate runs this long; it is a leaked entry.
	maxCallAge = 6 * time.Hour
)

// staleReason says why a registry entry can no longer be a live call ("" = it is fine).
func staleReason(ac *activeCall, now time.Time) string {
	if ac == nil || ac.call == nil {
		return "no call object"
	}
	ph := ac.call.State()
	if ph == meowcaller.CallPhaseEnded {
		return "call library says it ended"
	}
	age := now.Sub(ac.startedAt)
	if age > maxCallAge {
		return "older than the maximum call length"
	}
	if ph < meowcaller.CallPhaseConnecting && age > maxPreConnectAge {
		return "never got past ringing"
	}
	return ""
}

// reapStaleCalls removes registry entries that are not live calls, so they stop counting against
// max-calls-per-session and the one-call-per-client rule. Returns how many were removed.
func (s *Session) reapStaleCalls() int {
	n := 0
	now := time.Now()
	for id, ac := range s.reg.snapshot() {
		why := staleReason(ac, now)
		if why == "" {
			continue
		}
		s.log.Warn("removing a stale call that was still counted as active", "call_id", id, "why", why)
		if ac != nil && ac.call != nil && ac.call.State() != meowcaller.CallPhaseEnded {
			_ = ac.call.Hangup()
		}
		s.removeCall(id)
		s.mgr.broker.endCall(id, "stale")
		n++
	}
	return n
}

// clearAllCalls hangs up and forgets every call on this session. It is the "unstick me" button: the
// app uses it when WaCalls refuses a new call because old ones are still counted.
func (s *Session) clearAllCalls(reason string) int {
	n := 0
	for id, ac := range s.reg.snapshot() {
		if ac != nil && ac.call != nil && ac.call.State() != meowcaller.CallPhaseEnded {
			_ = ac.call.Hangup()
		}
		s.removeCall(id)
		s.mgr.broker.endCall(id, reason)
		n++
	}
	return n
}
