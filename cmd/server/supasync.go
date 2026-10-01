package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Persists the SQLite session DB in Supabase Storage so sessions survive
// restarts/redeploys. Enabled when SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY are set.

const supaBucket = "wacalls-state"
const supaObject = "wacalls.db"

func supaCfg() (base, key string, ok bool) {
	base = strings.TrimRight(os.Getenv("SUPABASE_URL"), "/")
	key = os.Getenv("SUPABASE_SERVICE_ROLE_KEY")
	return base, key, base != "" && key != ""
}

func supaReq(method, url, key string, body []byte, extra map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("apikey", key)
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return (&http.Client{Timeout: 60 * time.Second}).Do(req)
}

// restoreFromSupabase pulls the saved session DB before the server opens it.
// It returns true when it is SAFE to start backing up afterwards: either the
// restore worked, or Supabase positively said there is no saved DB yet.
// If Supabase could not be reached it returns false, so a fresh empty DB can
// never overwrite the good copy that is still sitting in Supabase.
func restoreFromSupabase(dbPath string, log *slog.Logger) bool {
	base, key, ok := supaCfg()
	if !ok {
		log.Warn("supabase sync disabled (SUPABASE_URL / SUPABASE_SERVICE_ROLE_KEY not set) - sessions will NOT survive a restart")
		return false
	}
	// make sure the private bucket exists
	if r, err := supaReq("POST", base+"/storage/v1/bucket", key,
		[]byte(`{"id":"`+supaBucket+`","name":"`+supaBucket+`","public":false}`),
		map[string]string{"Content-Type": "application/json"}); err == nil {
		r.Body.Close()
	}
	const attempts = 6
	for i := 1; i <= attempts; i++ {
		resp, err := supaReq("GET", fmt.Sprintf("%s/storage/v1/object/%s/%s", base, supaBucket, supaObject), key, nil, nil)
		if err != nil {
			log.Error("supabase restore failed, will retry", "attempt", i, "err", err)
			time.Sleep(time.Duration(i) * 2 * time.Second)
			continue
		}
		body, rerr := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == 200 && rerr == nil && len(body) > 0:
			_ = os.Remove(dbPath + "-wal")
			_ = os.Remove(dbPath + "-shm")
			if err := os.WriteFile(dbPath, body, 0o600); err != nil {
				log.Error("writing restored db failed", "err", err)
				return false
			}
			log.Info("sessions restored from supabase", "bytes", len(body))
			return true
		case resp.StatusCode == 404 || (resp.StatusCode == 400 && strings.Contains(strings.ToLower(string(body)), "not found")):
			log.Info("no saved sessions in supabase yet", "status", resp.StatusCode)
			return true
		default:
			log.Error("supabase restore got unexpected response, will retry", "attempt", i, "status", resp.StatusCode)
			time.Sleep(time.Duration(i) * 2 * time.Second)
		}
	}
	log.Error("could not restore sessions from supabase - backups are paused so the saved copy is not overwritten")
	return false
}

func backupToSupabase(dbPath string, log *slog.Logger, last *[32]byte) {
	base, key, ok := supaCfg()
	if !ok {
		return
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return
	}
	defer db.Close()
	tmp := dbPath + ".bak"
	_ = os.Remove(tmp)
	if _, err := db.Exec("VACUUM INTO ?", tmp); err != nil {
		log.Error("backup snapshot failed", "err", err)
		return
	}
	defer os.Remove(tmp)
	data, err := os.ReadFile(tmp)
	if err != nil {
		return
	}
	sum := sha256.Sum256(data)
	if sum == *last {
		return
	}
	resp, err := supaReq("POST", fmt.Sprintf("%s/storage/v1/object/%s/%s", base, supaBucket, supaObject), key, data,
		map[string]string{"Content-Type": "application/octet-stream", "x-upsert": "true"})
	if err != nil {
		log.Error("supabase backup failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		log.Error("supabase backup rejected", "status", resp.StatusCode, "body", string(b))
		return
	}
	*last = sum
}

// backupSoon asks the backup loop to upload within a couple of seconds instead
// of waiting for the next tick. Called right after a WhatsApp link succeeds, so
// a restart just after linking cannot lose the new session.
var backupRequests = make(chan struct{}, 1)

func backupSoon() {
	select {
	case backupRequests <- struct{}{}:
	default:
	}
}

func startSupabaseBackup(ctx context.Context, dbPath string, log *slog.Logger, safe bool) (final func()) {
	if !safe {
		return func() {}
	}
	var last [32]byte
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-backupRequests:
				time.Sleep(2 * time.Second) // let the DB writes from the event settle
				backupToSupabase(dbPath, log, &last)
			case <-t.C:
				backupToSupabase(dbPath, log, &last)
			}
		}
	}()
	return func() { backupToSupabase(dbPath, log, &last) }
}
