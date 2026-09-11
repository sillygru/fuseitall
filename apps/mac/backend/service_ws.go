// SPDX-License-Identifier: AGPL-3.0-only

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
	// Resync-on-connect: any sms/contacts push lost while offline left the
	// caches stale. Invalidate and notify so panes refetch on the live
	// socket instead of serving the stale fast-path.
	s.messagesMu.Lock()
	s.smsGen++
	s.threadsCache = nil
	s.messagesCache = nil
	s.messagesMu.Unlock()
	s.contactsMu.Lock()
	s.contactsGen++
	s.contactsCache = nil
	s.contactsMu.Unlock()
	s.emitMessagesChanged()
	s.emitContactsChanged()
	if s.clipWatcher != nil {
		go s.clipWatcher.TriggerNow()
	}
}

// OnWSPing refreshes presence freshness on every successful WS control ping.
// Silent (no emit, no log): the 5s watchdog would otherwise spam the UI.
// Keeps the HTTP return path fresh while the socket is healthy so idle-but-
// connected peers never flirt with TTL expiry.
func (s *Service) OnWSPing(conn *core.WSConn) {
	s.mu.Lock()
	if s.activeWS == conn {
		s.lastSeen = time.Now()
	}
	s.mu.Unlock()
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
	case core.TypeNotifAppsResp:
		raw, err := json.Marshal(env)
		if err == nil {
			// Resolves the pending inventory page waiter; no UI event:
			// the awaiting RequestPhoneNotifApps call returns and the
			// frontend refresh picks up the merged rows.
			s.ingestNotifBody(raw)
		}
	case core.TypeClipPush:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestClipBody(raw)
			s.emitClipChanged()
		}
	case core.TypeClipManifest:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestClipManifestBody(raw)
		}
	case core.TypeClipChunk:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestClipChunkBody(raw)
			s.emitClipChanged()
		}
	case core.TypeSettingsSync:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestSettingsBody(raw)
			s.emitSettingsChanged()
		}
	case core.TypePlaybackState:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestPlaybackBody(raw)
			s.emitPlaybackChanged()
		}
	case core.TypeDNDState:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestDNDBody(raw)
		}
	case core.TypeUnpair:
		s.ingestUnpairBody()
		s.emitStateChanged()
	case core.TypeFileListResp, core.TypeFileChunk, core.TypeFileAck, core.TypeFileCancel, core.TypeFileStatResp:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestFileBody(raw)
			// Coalesced download progress: every Nth chunk plus the final
			// one (same counter rule as uploads). Non-chunk file types
			// always emit. Push-only, no timers — the UI still converges
			// on every completion.
			if shouldEmitDownloadProgress(env) {
				s.emitTransfersChanged()
			}
		}
	case core.TypePhotoListResp, core.TypePhotoThumbResp, core.TypePhotoChunk, core.TypePhotoDeleteResp:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestPhotoBody(raw)
			s.emitPhotoTransfersChanged()
		}
	case core.TypeContactsListResp, core.TypeContactAvatarResp, core.TypeContactDeleteResp, core.TypeContactsChanged:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestContactsBody(raw)
			// Resp pages resolve their waiter via the return path; only
			// push/invalidate events fan out to panes. Emitting on every
			// resp caused stale fast-path refetch churn.
			if env.Type == core.TypeContactsChanged {
				s.emitContactsChanged()
			}
		}
	case core.TypeSMSThreadsResp, core.TypeSMSMessagesResp, core.TypeSMSSendResp, core.TypeSMSMarkReadResp, core.TypeSMSPush, core.TypeSMSChanged:
		raw, err := json.Marshal(env)
		if err == nil {
			s.ingestMessagesBody(raw)
			if env.Type == core.TypeSMSPush || env.Type == core.TypeSMSChanged {
				s.emitMessagesChanged()
			}
		}
	}
}

// shouldEmitDownloadProgress reports whether an inbound file envelope
// warrants a transfers:changed push. File chunks follow the shared
// counter rule (first, every Nth, last); every other file type always
// emits. Unparseable chunk payloads emit fail-open: a progress frame is
// cheap, a stuck bar is not. Pure.
func shouldEmitDownloadProgress(env core.Envelope) bool {
	if env.Type != core.TypeFileChunk {
		return true
	}
	var payload struct {
		ChunkIndex  int `json:"chunk_index"`
		TotalChunks int `json:"total_chunks"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return true
	}
	if payload.TotalChunks < 1 {
		return true
	}
	return shouldEmitProgress(payload.ChunkIndex, payload.TotalChunks)
}

// OnWSDisconnect is called immediately when the phone disconnects (e.g. app closed,
// Wi-Fi lost, socket EOF). Flips IsPaired false instantly via push.
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
	s.failPendingNotifApps(errors.New("phone is offline — reconnect first"))
	s.failPendingSyncRequests(errors.New("phone is offline — reconnect first"))
	s.appendLine("phone disconnected from websocket")
	s.emitStateChanged()
}

// NotifyLocalNetworkDown drops the live peer immediately when the Mac itself
// loses LAN (frontend online/offline push, not polling). A half-open socket
// would otherwise ghost Connected for ~10-15s until the watchdog fires;
// this flips offline instantly. Idempotent: already-offline is a no-op.
// Thin adapter: no link monitoring here, the OS event arrives via the UI.
func (s *Service) NotifyLocalNetworkDown() (string, error) {
	s.mu.Lock()
	ws := s.activeWS
	hasEphemeral := ws != nil || s.peerHost != ""
	s.mu.Unlock()
	if ws != nil {
		_ = ws.Close()
		s.OnWSDisconnect(ws)
		s.appendLine("local network down — phone peer dropped")
		return "Phone marked offline (local network down).", nil
	}
	if hasEphemeral {
		s.clearPeer()
		s.appendLine("local network down — phone peer dropped")
		s.emitStateChanged()
		return "Phone marked offline (local network down).", nil
	}
	return "Already offline.", nil
}

// WriteActiveWS attempts to send an envelope directly over the active WebSocket.
// Large envelopes (file/photo chunks) get a 60-second deadline to accommodate
// multi-megabyte transfers and pipelined queueing without timing out under heavy bursts.
// Returns true if sent, false if no active WebSocket is connected or write failed.
func (s *Service) WriteActiveWS(env core.Envelope) bool {
	s.mu.Lock()
	ws := s.activeWS
	s.mu.Unlock()

	if ws == nil {
		return false
	}
	writeTimeout := 15 * time.Second
	if env.Type == core.TypeFileChunk || env.Type == core.TypePhotoChunk || len(env.Payload) > 64*1024 {
		writeTimeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
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

func (s *Service) emitPlaybackChanged() {
	emitWailsEvent("playback:changed", s.GetPlayback())
}

func (s *Service) emitTransfersChanged() {
	emitWailsEvent("transfers:changed", s.GetTransfers())
}

func (s *Service) emitContactsChanged() {
	emitWailsEvent("contacts:changed", map[string]any{"ok": true})
}

func (s *Service) emitMessagesChanged() {
	emitWailsEvent("messages:changed", map[string]any{"ok": true})
}
