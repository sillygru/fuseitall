// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"fuseitall/core"
)

// OnWSConnect is called immediately when an authenticated phone establishes
// a persistent TLS WebSocket connection. Flips IsPaired true and notifies the UI.
func (s *Service) OnWSConnect(conn *core.WSConn, remoteAddr string) {
	host := SplitRemoteHost(remoteAddr)
	s.mu.Lock()
	old := s.activeWS
	s.activeWS = conn
	s.lastSeen = time.Now()
	s.peerHost = host
	if s.peerPort <= 0 && s.lastPort > 0 {
		s.peerPort = s.lastPort
	}
	s.mu.Unlock()

	if old != nil && old != conn {
		_ = old.Close()
	}

	s.appendLine("phone connected via websocket remote=" + host)
	s.emitStateChanged()
	s.flushPendingToPhone()
}

// OnWSEnvelope handles real-time inbound wire envelopes over the persistent WebSocket.
func (s *Service) OnWSEnvelope(conn *core.WSConn, env core.Envelope) {
	s.mu.Lock()
	s.lastSeen = time.Now()
	s.mu.Unlock()
	// Every envelope is authenticated: its sender refreshes the cached peer
	// version (and clears a satisfied update notice) via learnPeer.
	s.learnPeer(env.Sender.Platform, env.Sender.AppBuild, env.Sender.AppVersion, env.Capabilities)

	switch env.Type {
	case core.TypePing:
		raw, err := json.Marshal(env)
		if err == nil {
			facts := ParsePeerDevice(raw)
			if port, fp, ok := ParsePeerPingFull(raw); ok {
				host := SplitRemoteHost(conn.RemoteAddr())
				s.setPeerWithFacts(host, port, fp, facts)
			} else {
				// Port-less presence push (e.g. battery-only): the return
				// path is already known, just fold in the facts.
				s.mergeFacts(facts)
			}
			s.emitStateChanged()
		}
	case core.TypeNotifPost, core.TypeNotifDismiss:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestNotifBody(raw)
			s.emitNotifsChanged()
		}
	case core.TypeClipPush:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestClipBody(raw)
			s.emitClipChanged()
		}
	case core.TypeSettingsSync:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestSettingsBody(raw)
			s.emitSettingsChanged()
		}
	case core.TypeUnpair:
		s.ingestUnpairBody()
		s.emitStateChanged()
	case core.TypeFileListResp, core.TypeFileChunk:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestFileBody(raw)
			s.emitTransfersChanged()
		}
	case core.TypePhotoListResp, core.TypePhotoThumbResp, core.TypePhotoChunk, core.TypePhotoDeleteResp:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestPhotoBody(raw)
			s.emitPhotoTransfersChanged()
		}
	}
}

// OnWSDisconnect is called immediately when the phone disconnects (e.g. app closed,
// Wi-Fi lost, socket EOF). Flips IsPaired false with zero polling delay.
func (s *Service) OnWSDisconnect(conn *core.WSConn) {
	s.mu.Lock()
	if s.activeWS == conn {
		s.activeWS = nil
		s.peerHost = ""
		s.peerPort = 0
		s.lastSeen = time.Time{}
	}
	s.mu.Unlock()

	s.failPendingPhotoRequests(errors.New("phone is offline — reconnect first"))
	s.appendLine("phone disconnected from websocket")
	s.emitStateChanged()
}

// WriteActiveWS attempts to send an envelope directly over the active WebSocket.
// Returns true if sent, false if no active WebSocket is connected or write failed.
func (s *Service) WriteActiveWS(env core.Envelope) bool {
	s.mu.Lock()
	ws := s.activeWS
	s.mu.Unlock()

	if ws == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := ws.WriteEnvelope(ctx, env); err != nil {
		s.appendLine("ws write failed: " + err.Error())
		_ = ws.Close()
		s.OnWSDisconnect(ws)
		return false
	}
	return true
}

func (s *Service) emitStateChanged() {
	emitWailsEvent("state:changed", map[string]any{
		"paired":     s.IsPaired(),
		"peerDevice": s.GetPeerDevice(),
		"lastDevice": s.GetLastDevice(),
	})
}

func (s *Service) emitNotifsChanged() {
	emitWailsEvent("notifs:changed", s.GetNotifications())
}

func (s *Service) emitSettingsChanged() {
	emitWailsEvent("settings:changed", s.GetSettings())
}

func (s *Service) emitClipChanged() {
	emitWailsEvent("clipboard:changed", s.GetClipboard())
}

func (s *Service) emitTransfersChanged() {
	emitWailsEvent("transfers:changed", s.GetTransfers())
}
