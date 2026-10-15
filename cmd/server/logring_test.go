package main

import (
	"strings"
	"testing"
)

func TestLogRingFiltersOtherCallsAndAddresses(t *testing.T) {
	r := &logRing{}
	r.Write([]byte("relay connect timed out call_id=MINE\n"))
	r.Write([]byte("something about call_id=OTHER\n"))
	r.Write([]byte("peer 2348012345678@s.whatsapp.net called call_id=MINE\n"))
	r.Write([]byte("generic line with no call id\nsplit "))
	r.Write([]byte("across writes\n"))
	got := r.forCall(0, "MINE", 50)
	var joined []string
	for _, l := range got {
		joined = append(joined, l.Line)
	}
	all := strings.Join(joined, "|")
	if !strings.Contains(all, "relay connect timed out") || !strings.Contains(all, "generic line") || !strings.Contains(all, "split across writes") {
		t.Fatalf("missing own/generic lines: %s", all)
	}
	if strings.Contains(all, "OTHER") || strings.Contains(all, "whatsapp.net") {
		t.Fatalf("leaked another call or an address: %s", all)
	}
}
