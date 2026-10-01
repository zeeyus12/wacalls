package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
)

// ensureConnected makes sure the WhatsApp websocket is up and logged in before
// we try to place a call. A linked session can be sitting there with a dead
// socket (restart, network blip, "stream replaced"), and the call then fails
// with "usync ...: websocket not connected". Instead of failing, reconnect and
// wait for the login to finish.
func (s *Session) ensureConnected(ctx context.Context) error {
	if s.client.IsConnected() && s.client.IsLoggedIn() {
		return nil
	}
	if s.client.Store.ID == nil {
		return errors.New("whatsapp is not linked on this session yet")
	}
	s.log.Warn("whatsapp websocket is down, reconnecting before the call")
	if err := s.client.Connect(); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return fmt.Errorf("could not reconnect to whatsapp: %w", err)
	}
	deadline := time.After(15 * time.Second)
	for {
		if s.client.IsConnected() && s.client.IsLoggedIn() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.New("whatsapp is still reconnecting - try again in a few seconds")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// watchdog keeps a linked session connected for as long as it exists, so
// incoming calls keep ringing and outgoing calls never find a dead socket.
// It only acts on sessions that are already linked (Store.ID set); sessions
// still waiting for a QR / pairing code are left alone.
func (s *Session) watchdog() {
	defer func() { _ = recover() }()
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.mgr.appCtx.Done():
			return
		case <-t.C:
		}
		if cur, ok := s.mgr.Get(s.id); !ok || cur != s {
			return // session was deleted
		}
		if s.client == nil || s.client.Store.ID == nil || s.client.IsConnected() {
			continue
		}
		s.log.Warn("whatsapp websocket dropped, reconnecting")
		if err := s.client.Connect(); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			s.log.Error("reconnect failed, will retry", "err", err)
		}
	}
}
