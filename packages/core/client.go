// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNonceMismatch is returned when a pong does not echo the ping nonce.
var ErrNonceMismatch = errors.New("pong nonce mismatch")

// ErrBadFingerprintLength is returned when a TOFU fingerprint is not 32 bytes.
var ErrBadFingerprintLength = errors.New("want 32 bytes")

// SendPing posts a ping envelope to baseURL+"/ping" and returns the pong.
// The nonce echoes: a mismatched nonce fails closed. An error/UPDATE_REQUIRED
// reply becomes *UpdateRequiredError (errors.Is-mappable to ErrLocalOutdated
// or ErrPeerOutdated); any other error type is a plain error carrying the
// peer's message.
func SendPing(ctx context.Context, client *http.Client, baseURL, token string, sender SenderInfo, caps []string, sentAt time.Time) (PongPayload, error) {
	nonce, err := freshNonce()
	if err != nil {
		return PongPayload{}, err
	}
	env, err := NewEnvelope(TypePing, sender, caps, PingPayload{Nonce: nonce, SentAt: sentAt.Unix()})
	if err != nil {
		return PongPayload{}, err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return PongPayload{}, fmt.Errorf("marshal ping envelope: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/ping", bytes.NewReader(body))
	if err != nil {
		return PongPayload{}, fmt.Errorf("build ping request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return PongPayload{}, fmt.Errorf("post ping: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return PongPayload{}, fmt.Errorf("read ping reply: %w", err)
	}
	var reply Envelope
	if err := json.Unmarshal(respBody, &reply); err != nil {
		return PongPayload{}, fmt.Errorf("decode ping reply: %w", err)
	}
	if err := CheckProtocolVersion(reply.ProtocolV); err != nil {
		return PongPayload{}, fmt.Errorf("ping reply: %w", err)
	}
	if reply.Type == TypeError {
		return PongPayload{}, updateErrorFrom(reply)
	}
	if reply.Type != TypePong {
		return PongPayload{}, fmt.Errorf("unexpected ping reply type %q", reply.Type)
	}
	var pong PongPayload
	if err := DecodePayload(reply, &pong); err != nil {
		return PongPayload{}, err
	}
	if !VerifyToken(nonce, pong.Nonce) {
		return PongPayload{}, ErrNonceMismatch
	}
	return pong, nil
}

func updateErrorFrom(reply Envelope) error {
	var payload ErrorPayload
	if err := DecodePayload(reply, &payload); err != nil {
		return fmt.Errorf("peer error: %w", err)
	}
	if payload.Code == CodeUpdateRequired {
		return &UpdateRequiredError{
			Device:          payload.Device,
			RequiredBuild:   payload.RequiredBuild,
			RequiredVersion: payload.RequiredVersion,
			CurrentVersion:  payload.CurrentVersion,
			Message:         payload.Message,
			LocalOutdated:   payload.RequiredBuild > CurrentBuild,
		}
	}
	return fmt.Errorf("peer error %s: %s", payload.Code, payload.Message)
}

func freshNonce() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate ping nonce: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// NewTOFUClient returns an HTTPS client that pins the server's self-signed
// cert by SHA-256 fingerprint (trust on first use: the fingerprint arrives
// via the scanned QR). An invalid fingerprint fails closed at construction;
// a mismatched peer cert fails closed per handshake.
func NewTOFUClient(certFingerprint string) (*http.Client, error) {
	fingerprint, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(certFingerprint)))
	if err != nil {
		return nil, fmt.Errorf("invalid cert fingerprint: %w", err)
	}
	if len(fingerprint) != sha256.Size {
		return nil, fmt.Errorf("invalid cert fingerprint length %d: %w", len(fingerprint), ErrBadFingerprintLength)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// security: TOFU pinning replaces CA verification for LAN
				// self-signed certs. VerifyPeerCertificate below fails
				// closed on any fingerprint mismatch.
				InsecureSkipVerify: true, // #nosec G402 -- pinned TOFU, see above
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return errors.New("no peer certificate presented")
					}
					sum := sha256.Sum256(rawCerts[0])
					if subtle.ConstantTimeCompare(sum[:], fingerprint) != 1 {
						return errors.New("certificate fingerprint mismatch")
					}
					return nil
				},
			},
		},
	}, nil
}
