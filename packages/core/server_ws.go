// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WSConn wraps a coder/websocket connection with thread-safe envelope writing
// and peer metadata for real-time bi-directional streaming.
type WSConn struct {
	conn    *websocket.Conn
	remote  string
	writeMu sync.Mutex
}

// RemoteAddr returns the network address of the connected peer.
func (c *WSConn) RemoteAddr() string {
	return c.remote
}

// WriteEnvelope marshals and sends an Envelope as a WebSocket text message.
// Thread-safe: writeMu prevents interleaved framing across concurrent callers.
func (c *WSConn) WriteEnvelope(ctx context.Context, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal ws envelope: %w", err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write ws message: %w", err)
	}
	return nil
}

// Close gracefully closes the underlying WebSocket connection.
func (c *WSConn) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "bye")
}

// WSHandler receives real-time connection events and incoming envelopes
// over the persistent /ws endpoint.
type WSHandler interface {
	OnWSConnect(conn *WSConn, remoteAddr string)
	OnWSEnvelope(conn *WSConn, env Envelope)
	OnWSDisconnect(conn *WSConn)
}

// SetWSHandler registers the handler for real-time WebSocket peer events.
func (s *Server) SetWSHandler(h WSHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsHandler = h
}

// getWSHandler retrieves the active WebSocket event handler.
func (s *Server) getWSHandler() WSHandler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wsHandler
}

// handleWS upgrades GET /ws to an authenticated, encrypted persistent WebSocket.
// Checks Authorization: Bearer <token> or ?token=<token> before upgrade.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if !VerifyToken(s.currentToken(), token) {
		s.logger.Warn("ws rejected", "reason", "unauthorized", "remote", r.RemoteAddr)
		s.writeErrorEnvelope(w, http.StatusForbidden, CodeUnauthorized, "invalid pair token")
		return
	}

	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow local LAN connections across dynamic IP re-assignments
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		s.logger.Warn("ws accept failed", "err", err, "remote", r.RemoteAddr)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	// Allow full envelopes up to MaxBodyBytes (8 MiB, e.g. photo thumbnails/chunks,
	// clipboard images) instead of coder/websocket's 32 KiB default.
	conn.SetReadLimit(MaxBodyBytes)

	wsPeer := &WSConn{
		conn:   conn,
		remote: r.RemoteAddr,
	}

	handler := s.getWSHandler()
	if handler != nil {
		handler.OnWSConnect(wsPeer, r.RemoteAddr)
	}

	s.logger.Info("ws client connected", "remote", r.RemoteAddr)

	ctx, cancelRead := context.WithCancel(r.Context())
	defer cancelRead()

	// Ping watchdog: detects silently dropped connections (e.g. peer switched networks,
	// walked out of Wi-Fi range without sending TCP FIN).
	// Uses a 15s interval with 10s timeout, requiring 2 consecutive failed pings
	// before terminating to tolerate transient heavy payload streaming.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		failedPings := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pCtx, pCancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pCtx)
				pCancel()
				if err != nil {
					failedPings++
					s.logger.Debug("ws peer ping failed", "err", err, "consecutive", failedPings, "remote", r.RemoteAddr)
					if failedPings >= 2 {
						s.logger.Debug("ws peer ping threshold reached, terminating half-open connection", "remote", r.RemoteAddr)
						_ = conn.CloseNow()
						return
					}
				} else {
					failedPings = 0
				}
			}
		}
	}()

	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			var closeErr websocket.CloseError
			if errors.As(err, &closeErr) {
				s.logger.Debug("ws peer closed normally", "code", closeErr.Code, "remote", r.RemoteAddr)
			} else {
				s.logger.Debug("ws read error", "err", err, "remote", r.RemoteAddr)
			}
			break
		}
		if msgType != websocket.MessageText {
			continue
		}

		hdr, err := ParseAndGateHeader(data)
		if err != nil {
			s.logger.Warn("ws envelope rejected", "err", err, "remote", r.RemoteAddr)
			switch {
			case errors.Is(err, ErrUnsupportedProtocol):
				_ = s.writeWSError(ctx, wsPeer, CodeBadRequest, "unsupported protocol")
			case errors.Is(err, ErrLocalOutdated):
				_ = s.writeWSUpdateRequired(ctx, wsPeer, s.platform, hdr.Sender.MinPeerBuild)
			case errors.Is(err, ErrPeerOutdated):
				_ = s.writeWSUpdateRequired(ctx, wsPeer, hdr.Sender.Platform, CurrentMinPeerBuild)
			default:
				_ = s.writeWSError(ctx, wsPeer, CodeBadRequest, "malformed envelope")
			}
			continue
		}

		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			_ = s.writeWSError(ctx, wsPeer, CodeBadRequest, "malformed json")
			continue
		}

		// Ping type handled natively: immediate pong response with echoed nonce.
		if env.Type == TypePing {
			var ping PingPayload
			if err := DecodePayload(env, &ping); err == nil && ping.Nonce != "" {
				pong, err := NewEnvelope(TypePong, s.Sender(), s.caps, PongPayload{
					Nonce:      ping.Nonce,
					ReceivedAt: time.Now().Unix(),
				})
				if err == nil {
					_ = wsPeer.WriteEnvelope(ctx, pong)
				}
			}
		}

		// Forward envelope to registered handler (feature syncs, device facts, etc.)
		if handler != nil {
			handler.OnWSEnvelope(wsPeer, env)
		}
	}

	s.logger.Info("ws client disconnected", "remote", r.RemoteAddr)
	if handler != nil {
		handler.OnWSDisconnect(wsPeer)
	}
}

func (s *Server) writeWSError(ctx context.Context, peer *WSConn, code, msg string) error {
	env, err := NewEnvelope(TypeError, s.Sender(), s.caps, ErrorPayload{Code: code, Message: msg})
	if err != nil {
		return err
	}
	return peer.WriteEnvelope(ctx, env)
}

func (s *Server) writeWSUpdateRequired(ctx context.Context, peer *WSConn, device string, requiredBuild int) error {
	payload := NewUpdateRequiredPayload(device, requiredBuild)
	env, err := NewEnvelope(TypeError, s.Sender(), s.caps, payload)
	if err != nil {
		return err
	}
	return peer.WriteEnvelope(ctx, env)
}
