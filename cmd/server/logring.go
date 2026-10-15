package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// logRing keeps the most recent log lines (ours AND the call library's) in memory, so the app can ask
// "what did the call server see during my call?" without anyone having to read Fly's logs.
type logRing struct {
	mu    sync.Mutex
	lines []ringLine
	part  string
}

type ringLine struct {
	T    int64  `json:"t"`
	Line string `json:"line"`
}

const ringMax = 1500

var callLog = &logRing{}

// Write implements io.Writer; it is teed next to stderr.
func (r *logRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.part += string(p)
	for {
		i := strings.IndexByte(r.part, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(r.part[:i], "\r")
		r.part = r.part[i+1:]
		if line == "" {
			continue
		}
		if len(line) > 400 {
			line = line[:400]
		}
		r.lines = append(r.lines, ringLine{T: time.Now().UnixMilli(), Line: line})
		if len(r.lines) > ringMax {
			r.lines = r.lines[len(r.lines)-ringMax:]
		}
	}
	return len(p), nil
}

// forCall returns the lines since `since` that belong to this call, or to nobody in particular. Lines
// that name another call, or carry a WhatsApp address (@s.whatsapp.net / @lid), are left out, so one
// user's debug view never shows another user's calls or contacts.
func (r *logRing) forCall(since int64, callID string, limit int) []ringLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ringLine{}
	for _, l := range r.lines {
		if l.T < since {
			continue
		}
		low := strings.ToLower(l.Line)
		if strings.Contains(low, "@s.whatsapp.net") || strings.Contains(low, "@lid") || strings.Contains(low, "@g.us") {
			continue
		}
		if i := strings.Index(l.Line, "call_id"); i >= 0 && callID != "" && !strings.Contains(l.Line, callID) {
			continue
		}
		out = append(out, l)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (s *server) handleDebugLog(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 400 {
		limit = 150
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"lines": callLog.forCall(since, r.URL.Query().Get("call"), limit)})
}
