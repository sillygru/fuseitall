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

// PeerInfo is the authenticated peer identity learned from a reply envelope
// header. It is transport metadata, never wire payload: SendPing and
// SendFeature return it alongside the pong so callers can refresh a cached
// peer version without an extra round trip. It is populated on every decoded
// reply, including error/UPDATE_REQUIRED (the rejector stamps its own
// sender); zero only when no reply was decoded.
type PeerInfo struct {
	Platform     string
	AppBuild     int
	AppVersion   string
	Capabilities []string
}

// peerInfoFromReply extracts the sender identity from a decoded reply
// envelope. Capabilities are copied so callers own the slice.
func peerInfoFromReply(reply Envelope) PeerInfo {
	return PeerInfo{
		Platform:     reply.Sender.Platform,
		AppBuild:     reply.Sender.AppBuild,
		AppVersion:   reply.Sender.AppVersion,
		Capabilities: append([]string{}, reply.Capabilities...),
	}
}

// SendPing posts a ping envelope to baseURL+"/ping" and returns the pong
// plus the peer identity from the reply header. The nonce echoes: a
// mismatched nonce fails closed. An error/UPDATE_REQUIRED reply becomes
// *UpdateRequiredError (errors.Is-mappable to ErrLocalOutdated or
// ErrPeerOutdated); any other error type is a plain error carrying the
// peer's message.
func SendPing(ctx context.Context, client *http.Client, baseURL, token string, sender SenderInfo, caps []string, sentAt time.Time) (PongPayload, PeerInfo, error) {
	nonce, err := freshNonce()
	if err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	env, err := NewEnvelope(TypePing, sender, caps, PingPayload{Nonce: nonce, SentAt: sentAt.Unix()})
	if err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("marshal ping envelope: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/ping", bytes.NewReader(body))
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("build ping request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("post ping: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("read ping reply: %w", err)
	}
	var reply Envelope
	if err := json.Unmarshal(respBody, &reply); err != nil {
		// Include status and truncated body for diagnosis; bodies never contain secrets beyond nonce.
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "…"
		}
		return PongPayload{}, PeerInfo{}, fmt.Errorf("decode ping reply: status=%d ct=%q body=%q: %w", resp.StatusCode, resp.Header.Get("Content-Type"), snippet, err)
	}
	if err := CheckProtocolVersion(reply.ProtocolV); err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("ping reply: %w", err)
	}
	if reply.Type == TypeError {
		return PongPayload{}, peerInfoFromReply(reply), updateErrorFrom(reply)
	}
	if reply.Type != TypePong {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("unexpected ping reply type %q", reply.Type)
	}
	var pong PongPayload
	if err := DecodePayload(reply, &pong); err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	if !VerifyToken(nonce, pong.Nonce) {
		return PongPayload{}, PeerInfo{}, ErrNonceMismatch
	}
	return pong, peerInfoFromReply(reply), nil
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

// FeaturePath maps a feature message type to its HTTP route.
func FeaturePath(msgType string) string {
	switch msgType {
	case TypeNotifPost, TypeNotifDismiss, TypeNotifAppsReq, TypeNotifAppsResp:
		return "/notif"
	case TypeClipPush, TypeClipManifest, TypeClipChunk:
		return "/clip"
	case TypeSettingsSync:
		return "/settings"
	case TypeUnpair:
		return "/unpair"
	case TypeFileList, TypeFileListResp, TypeFileMkdir, TypeFileDelete, TypeFileRename, TypeFileChunk, TypeFilePullReq, TypeFileAck, TypeFileCancel, TypeFileStatReq, TypeFileStatResp:
		return "/files"
	case TypePhotoList, TypePhotoListResp, TypePhotoThumbReq, TypePhotoThumbResp, TypePhotoPullReq, TypePhotoChunk, TypePhotoDelete, TypePhotoDeleteResp:
		return "/photos"
	default:
		return "/ping"
	}
}

// SendFeature posts a feature envelope (notifications, clipboard, settings)
// and returns the ack pong plus the peer identity from the reply header.
// The payload must be a pointer to one of the feature payload structs; its
// Nonce field is stamped here so callers never mint nonces themselves. The
// ack nonce must echo or the call fails closed with ErrNonceMismatch. An
// error/UPDATE_REQUIRED reply becomes *UpdateRequiredError like SendPing.
func SendFeature(ctx context.Context, client *http.Client, baseURL, token string, sender SenderInfo, caps []string, msgType string, payload any) (PongPayload, PeerInfo, error) {
	nonce, err := freshNonce()
	if err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	if err := stampFeatureNonce(payload, nonce); err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	env, err := NewEnvelope(msgType, sender, caps, payload)
	if err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("marshal %s envelope: %w", msgType, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+FeaturePath(msgType), bytes.NewReader(body))
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("build %s request: %w", msgType, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("post %s: %w", msgType, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("read %s reply: %w", msgType, err)
	}
	var reply Envelope
	if err := json.Unmarshal(respBody, &reply); err != nil {
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "…"
		}
		return PongPayload{}, PeerInfo{}, fmt.Errorf("decode %s reply: status=%d ct=%q body=%q: %w", msgType, resp.StatusCode, resp.Header.Get("Content-Type"), snippet, err)
	}
	if err := CheckProtocolVersion(reply.ProtocolV); err != nil {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("%s reply: %w", msgType, err)
	}
	if reply.Type == TypeError {
		return PongPayload{}, peerInfoFromReply(reply), updateErrorFrom(reply)
	}
	if reply.Type != TypePong {
		return PongPayload{}, PeerInfo{}, fmt.Errorf("unexpected %s reply type %q", msgType, reply.Type)
	}
	var pong PongPayload
	if err := DecodePayload(reply, &pong); err != nil {
		return PongPayload{}, PeerInfo{}, err
	}
	if !VerifyToken(nonce, pong.Nonce) {
		return PongPayload{}, PeerInfo{}, ErrNonceMismatch
	}
	return pong, peerInfoFromReply(reply), nil
}

// stampFeatureNonce sets the Nonce field of a feature payload pointer.
func stampFeatureNonce(payload any, nonce string) error {
	switch p := payload.(type) {
	case *NotifPostPayload:
		p.Nonce = nonce
	case *NotifDismissPayload:
		p.Nonce = nonce
	case *NotifAppsReqPayload:
		p.Nonce = nonce
	case *NotifAppsRespPayload:
		p.Nonce = nonce
	case *ClipPushPayload:
		p.Nonce = nonce
	case *ClipManifestPayload:
		p.Nonce = nonce
	case *ClipChunkPayload:
		p.Nonce = nonce
	case *SettingsSyncPayload:
		p.Nonce = nonce
	case *UnpairPayload:
		p.Nonce = nonce
	case *FileListPayload:
		p.Nonce = nonce
	case *FileListRespPayload:
		p.Nonce = nonce
	case *FileMkdirPayload:
		p.Nonce = nonce
	case *FileDeletePayload:
		p.Nonce = nonce
	case *FileRenamePayload:
		p.Nonce = nonce
	case *FileChunkPayload:
		p.Nonce = nonce
	case *FilePullReqPayload:
		p.Nonce = nonce
	case *FileAckPayload:
		p.Nonce = nonce
	case *FileCancelPayload:
		p.Nonce = nonce
	case *FileStatReqPayload:
		p.Nonce = nonce
	case *FileStatRespPayload:
		p.Nonce = nonce
	case *PhotoListPayload:
		p.Nonce = nonce
	case *PhotoListRespPayload:
		p.Nonce = nonce
	case *PhotoThumbReqPayload:
		p.Nonce = nonce
	case *PhotoThumbRespPayload:
		p.Nonce = nonce
	case *PhotoPullReqPayload:
		p.Nonce = nonce
	case *PhotoChunkPayload:
		p.Nonce = nonce
	case *PhotoDeletePayload:
		p.Nonce = nonce
	case *PhotoDeleteRespPayload:
		p.Nonce = nonce
	default:
		return fmt.Errorf("unsupported feature payload %T", payload)
	}
	return nil
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
