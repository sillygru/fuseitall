// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MaxBodyBytes caps inbound request bodies (8 MiB to allow 5 MiB images with
// base64 overhead + envelope). Exported so the Mac/bridge adapters reuse the
// exact same cap instead of triplicating the literal.
const MaxBodyBytes = 8 << 20

// Server is the TLS self-signed (TOFU) HTTP server for the BASE milestone:
// POST /ping (gated) and GET /health (liveness only).
type Server struct {
	platform    string
	caps        []string
	cert        tls.Certificate
	fingerprint string
	logger      *slog.Logger
	mux         *http.ServeMux

	// mu guards token: forget-and-rotate paths swap it live while
	// handlers verify against it on every request.
	mu        sync.RWMutex
	token     string
	wsHandler WSHandler
}

// NewServer builds a gated ping server and mints its self-signed cert. The
// returned CertFingerprint pins the cert into the QR payload for TOFU.
func NewServer(token, platform string, caps []string, logger *slog.Logger) (*Server, error) {
	cert, fingerprint, err := GenerateSelfSignedCert()
	if err != nil {
		return nil, err
	}
	return NewServerWithCert(token, platform, caps, logger, cert, fingerprint)
}

// NewServerWithCert builds the same gated ping server but reuses a persisted
// TLS cert (stable QR across restarts). fingerprint must be the hex SHA-256
// of cert's leaf DER; mismatches fail closed so a corrupt pair file never
// serves a half-pinned identity.
func NewServerWithCert(token, platform string, caps []string, logger *slog.Logger, cert tls.Certificate, fingerprint string) (*Server, error) {
	if token == "" {
		return nil, errors.New("pair token must not be empty")
	}
	if platform == "" {
		return nil, errors.New("platform must not be empty")
	}
	if len(cert.Certificate) == 0 {
		return nil, errors.New("tls certificate must not be empty")
	}
	want, err := TLSCertFingerprint(cert)
	if err != nil {
		return nil, err
	}
	normalized := strings.ToLower(strings.TrimSpace(fingerprint))
	if normalized != want {
		return nil, errors.New("tls fingerprint does not match certificate")
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		platform:    platform,
		token:       token,
		caps:        append([]string{}, caps...),
		cert:        cert,
		fingerprint: normalized,
		logger:      logger,
		mux:         http.NewServeMux(),
	}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/ping", s.handlePing)
	s.mux.HandleFunc("/notif", s.handleNotif)
	s.mux.HandleFunc("/clip", s.handleClip)
	s.mux.HandleFunc("/settings", s.handleSettings)
	s.mux.HandleFunc("/playback", s.handlePlayback)
	s.mux.HandleFunc("/unpair", s.handleUnpair)
	s.mux.HandleFunc("/files", s.handleFiles)
	s.mux.HandleFunc("/photos", s.handlePhotos)
	s.mux.HandleFunc("/ws", s.handleWS)
	return s, nil
}

// SetToken swaps the pair token live (forget-and-rotate): subsequent
// requests verify against the new token, old-token peers get 403. Empty
// tokens fail closed without touching state.
func (s *Server) SetToken(token string) error {
	if token == "" {
		return errors.New("pair token must not be empty")
	}
	s.mu.Lock()
	s.token = token
	s.mu.Unlock()
	return nil
}

// currentToken reads the live pair token for request verification.
func (s *Server) currentToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token
}

// Handler exposes the routes for embedding or tests.
func (s *Server) Handler() http.Handler { return s.mux }

// CertFingerprint is the hex SHA-256 of the server cert (TOFU pin).
func (s *Server) CertFingerprint() string { return s.fingerprint }

// TLSCertificate returns the server cert for listeners.
func (s *Server) TLSCertificate() tls.Certificate { return s.cert }

// Sender returns this server's advertised sender info.
func (s *Server) Sender() SenderInfo {
	return CurrentSender(s.platform)
}

// Serve listens with HTTPS using the self-signed cert.
func (s *Server) Serve(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Slowloris hardening: hijacked WS conns are unaffected after upgrade.
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{s.cert},
		},
	}
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		return fmt.Errorf("serve tls: %w", err)
	}
	return nil
}

// GenerateSelfSignedCert mints a fresh self-signed TLS cert for TOFU pinning.
// Exported so adapters can persist and reuse it (stable QR across restarts).
func GenerateSelfSignedCert() (tls.Certificate, string, error) {
	return generateSelfSignedCert()
}

// TLSCertFingerprint returns the hex SHA-256 of the leaf DER (the TOFU pin).
func TLSCertFingerprint(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", errors.New("tls certificate must not be empty")
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:]), nil
}

// EncodeTLSCertPEM exports a server cert as PEM blocks for disk persistence.
func EncodeTLSCertPEM(cert tls.Certificate) (certPEM, keyPEM []byte, err error) {
	if len(cert.Certificate) == 0 {
		return nil, nil, errors.New("tls certificate must not be empty")
	}
	// Only ECDSA P-256 is ever minted here; fail closed on anything else.
	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("tls private key must be ecdsa")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal cert key: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// ParseTLSCertPEM rebuilds a persisted cert and returns its fingerprint pin.
func ParseTLSCertPEM(certPEM, keyPEM []byte) (tls.Certificate, string, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("load key pair: %w", err)
	}
	fp, err := TLSCertFingerprint(cert)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return cert, fp, nil
}

func generateSelfSignedCert() (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate cert key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate cert serial: %w", err)
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "FuseItAll"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("marshal cert key: %w", err)
	}
	cert, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("load key pair: %w", err)
	}
	sum := sha256.Sum256(der)
	return cert, hex.EncodeToString(sum[:]), nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErrorEnvelope(w, http.StatusMethodNotAllowed, CodeBadRequest, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Warn("health encode failed", "err", err)
	}
}

// handlePing gates in strict order: protocol_v -> min_peer_build ->
// capability -> token -> logic. Every path writes a reply; nothing is
// silently dropped.
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.reject(w, http.StatusMethodNotAllowed, "wrong_method", "method not allowed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		s.reject(w, http.StatusBadRequest, "bad_body", "unreadable request body")
		return
	}
	hdr, err := ParseAndGateHeader(body)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnsupportedProtocol):
			// Best-effort lower bound: we cannot know the newer side's
			// build, so name our own platform and current build.
			s.rejectUpdate(w, s.platform, CurrentBuild)
		case errors.Is(err, ErrLocalOutdated):
			s.rejectUpdate(w, s.platform, hdr.Sender.MinPeerBuild)
		case errors.Is(err, ErrPeerOutdated):
			s.rejectUpdate(w, hdr.Sender.Platform, CurrentMinPeerBuild)
		default:
			s.reject(w, http.StatusBadRequest, "bad_envelope", "malformed request")
		}
		return
	}
	if hdr.Type != TypePing {
		s.reject(w, http.StatusBadRequest, "wrong_type", "expected ping message")
		return
	}
	if !IsCapabilitySupported(hdr.Capabilities, CapabilityPing) {
		s.rejectUpdate(w, hdr.Sender.Platform, CurrentBuild)
		return
	}
	if !VerifyToken(s.currentToken(), bearerToken(r)) {
		s.reject(w, http.StatusForbidden, "unauthorized", "invalid pair token")
		return
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		s.reject(w, http.StatusBadRequest, "bad_envelope", "malformed request")
		return
	}
	var ping PingPayload
	if err := DecodePayload(env, &ping); err != nil {
		s.reject(w, http.StatusBadRequest, "bad_payload", "malformed ping payload")
		return
	}
	if ping.Nonce == "" {
		s.reject(w, http.StatusBadRequest, "bad_payload", "malformed ping payload")
		return
	}
	pong, err := NewEnvelope(TypePong, s.Sender(), s.caps, PongPayload{
		Nonce:      ping.Nonce,
		ReceivedAt: time.Now().Unix(),
	})
	if err != nil {
		s.reject(w, http.StatusInternalServerError, "internal", "could not build reply")
		return
	}
	s.logger.Debug("ping handled", "peer_platform", hdr.Sender.Platform, "result", "pong")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pong); err != nil {
		s.logger.Warn("pong encode failed", "err", err)
	}
}

// reject logs server-side (stable low-cardinality reason, no tokens/PII)
// and writes a generic client-facing error envelope. Log-OR-write: the error
// is consumed here, never returned up the stack.
func (s *Server) reject(w http.ResponseWriter, status int, reason, clientMsg string) {
	s.logger.Warn("ping rejected", "reason", reason)
	code := CodeBadRequest
	if reason == "unauthorized" {
		code = CodeUnauthorized
	}
	s.writeErrorEnvelope(w, status, code, clientMsg)
}

// rejectUpdate replies error/UPDATE_REQUIRED with the canonical message.
func (s *Server) rejectUpdate(w http.ResponseWriter, device string, requiredBuild int) {
	s.logger.Warn("ping rejected", "reason", "update_required")
	payload := NewUpdateRequiredPayload(device, requiredBuild)
	env, err := NewEnvelope(TypeError, s.Sender(), s.caps, payload)
	if err != nil {
		s.writeErrorEnvelope(w, http.StatusInternalServerError, CodeBadRequest, "could not build reply")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUpgradeRequired)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		s.logger.Warn("update-required encode failed", "err", err)
	}
}

func (s *Server) writeErrorEnvelope(w http.ResponseWriter, status int, code, msg string) {
	env, err := NewEnvelope(TypeError, s.Sender(), s.caps, ErrorPayload{Code: code, Message: msg})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		s.logger.Warn("error encode failed", "err", err)
	}
}

// Feature routes share one gate with /ping: protocol_v -> min_peer_build ->
// capability -> token -> logic. Each route accepts its message types and
// replies with a pong echoing the request nonce, so senders match replies
// fail-closed with the same nonce check as ping. Payload contents are
// validated lightly (IDs, sizes, modes); semantic handling (stores, badge
// counts, clipboard writes) lives in the adapters, never here.
func (s *Server) handleNotif(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypeNotifPost:     CapabilityNotifications,
		TypeNotifDismiss:  CapabilityNotifications,
		TypeNotifAppsReq:  CapabilityNotifications,
		TypeNotifAppsResp: CapabilityNotifications,
	})
}

// handleClip serves clip-push under capability clipboard (manual only).
func (s *Server) handleClip(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypeClipPush: CapabilityClipboard,
	})
}

// handleSettings serves settings-sync under capability settings-sync.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypeSettingsSync: CapabilitySettingsSync,
	})
}

// handlePlayback serves playback-state/cmd under capability playback.
func (s *Server) handlePlayback(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypePlaybackState: CapabilityPlayback,
		TypePlaybackCmd:   CapabilityPlayback,
	})
}

// handleUnpair serves the goodbye message under capability ping
// (presence-level): the sender just unpaired and the adapter drops the peer
// on accept. Acked with a pong echo like every other feature route.
func (s *Server) handleUnpair(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypeUnpair: CapabilityPing,
	})
}

// handleFiles serves all file-manager messages under capability files.
// Each message is gated and acked with a pong echo; bulk transfer is via
// sequential file-chunk messages (1 MiB raw max each).
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypeFileList:     CapabilityFiles,
		TypeFileListResp: CapabilityFiles,
		TypeFileMkdir:    CapabilityFiles,
		TypeFileDelete:   CapabilityFiles,
		TypeFileRename:   CapabilityFiles,
		TypeFileChunk:    CapabilityFiles,
		TypeFilePullReq:  CapabilityFiles,
		TypeFileAck:      CapabilityFiles,
		TypeFileCancel:   CapabilityFiles,
		TypeFileStatReq:  CapabilityFiles,
		TypeFileStatResp: CapabilityFiles,
	})
}

// handlePhotos serves all photo-library messages under capability photos.
// Listing is cursor-paged; thumbs are fetched via separate thumb-req/resp;
// full-res is streamed via photo-chunk (isolated from file-chunk).
func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request) {
	s.handleFeature(w, r, map[string]string{
		TypePhotoList:       CapabilityPhotos,
		TypePhotoListResp:   CapabilityPhotos,
		TypePhotoThumbReq:   CapabilityPhotos,
		TypePhotoThumbResp:  CapabilityPhotos,
		TypePhotoPullReq:    CapabilityPhotos,
		TypePhotoChunk:      CapabilityPhotos,
		TypePhotoDelete:     CapabilityPhotos,
		TypePhotoDeleteResp: CapabilityPhotos,
	})
}

// handleFeature gates an envelope for one route's accepted types and acks
// with a pong echo. Every path writes exactly one reply; nothing is
// silently dropped.
func (s *Server) handleFeature(w http.ResponseWriter, r *http.Request, accepted map[string]string) {
	if r.Method != http.MethodPost {
		s.reject(w, http.StatusMethodNotAllowed, "wrong_method", "method not allowed")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		s.reject(w, http.StatusBadRequest, "bad_body", "unreadable request body")
		return
	}
	hdr, err := ParseAndGateHeader(body)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnsupportedProtocol):
			s.rejectUpdate(w, s.platform, CurrentBuild)
		case errors.Is(err, ErrLocalOutdated):
			s.rejectUpdate(w, s.platform, hdr.Sender.MinPeerBuild)
		case errors.Is(err, ErrPeerOutdated):
			s.rejectUpdate(w, hdr.Sender.Platform, CurrentMinPeerBuild)
		default:
			s.reject(w, http.StatusBadRequest, "bad_envelope", "malformed request")
		}
		return
	}
	wantCap, ok := accepted[hdr.Type]
	if !ok {
		s.reject(w, http.StatusBadRequest, "wrong_type", "unexpected message type")
		return
	}
	if !IsCapabilitySupported(hdr.Capabilities, wantCap) {
		s.rejectUpdate(w, hdr.Sender.Platform, CurrentBuild)
		return
	}
	if !VerifyToken(s.currentToken(), bearerToken(r)) {
		s.reject(w, http.StatusForbidden, "unauthorized", "invalid pair token")
		return
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		s.reject(w, http.StatusBadRequest, "bad_envelope", "malformed request")
		return
	}
	nonce, ok := featureNonce(hdr.Type, env.Payload)
	if !ok {
		s.reject(w, http.StatusBadRequest, "bad_payload", "malformed feature payload")
		return
	}
	if err := validateFeaturePayload(hdr.Type, env.Payload); err != nil {
		s.reject(w, http.StatusBadRequest, "bad_payload", "malformed feature payload")
		return
	}
	pong, err := NewEnvelope(TypePong, s.Sender(), s.caps, PongPayload{
		Nonce:      nonce,
		ReceivedAt: time.Now().Unix(),
	})
	if err != nil {
		s.reject(w, http.StatusInternalServerError, "internal", "could not build reply")
		return
	}
	s.logger.Debug("feature handled",
		"peer_platform", hdr.Sender.Platform, "type", hdr.Type, "result", "ack")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(pong); err != nil {
		s.logger.Warn("ack encode failed", "err", err)
	}
}

// featureNonce extracts the ack nonce from a feature payload. ok is false
// when the body is not JSON or the nonce is absent.
func featureNonce(msgType string, raw json.RawMessage) (string, bool) {
	var p struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", false
	}
	if p.Nonce == "" {
		return "", false
	}
	return p.Nonce, true
}

// validateFeaturePayload enforces size/shape caps per type: notification ID
// presence, clipboard length, settings mode. Truncatable display fields
// (app/title/text) are the receiver's fail-soft concern, not a rejection.
func validateFeaturePayload(msgType string, raw json.RawMessage) error {
	switch msgType {
	case TypeNotifPost:
		var p NotifPostPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, ok := SanitizeNotifID(p.ID); !ok {
			return errors.New("bad notification id")
		}
		return nil
	case TypeNotifDismiss:
		var p NotifDismissPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, ok := SanitizeNotifID(p.ID); !ok {
			return errors.New("bad notification id")
		}
		return nil
	case TypeNotifAppsReq:
		var p NotifAppsReqPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeNotifAppsReq(p) {
			return errors.New("bad notif-apps request")
		}
		return nil
	case TypeNotifAppsResp:
		var p NotifAppsRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeNotifAppsResp(p) {
			return errors.New("bad notif-apps response")
		}
		return nil
	case TypeClipPush:
		var p ClipPushPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeClipPush(p) {
			return errors.New("bad clipboard payload")
		}
		return nil
	case TypeSettingsSync:
		var p SettingsSyncPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, ok := SanitizeSettings(p); !ok {
			return errors.New("bad settings blob")
		}
		return nil
	case TypePlaybackState:
		var p PlaybackStatePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, ok := SanitizePlaybackState(p); !ok {
			return errors.New("bad playback state")
		}
		return nil
	case TypePlaybackCmd:
		var p PlaybackCmdPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if _, ok := SanitizePlaybackCmd(p); !ok {
			return errors.New("bad playback command")
		}
		return nil
	case TypeUnpair:
		var p UnpairPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.Nonce == "" {
			return errors.New("missing unpair nonce")
		}
		return nil
	case TypeFileList:
		var p FileListPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileList(p) {
			return errors.New("bad file-list payload")
		}
		return nil
	case TypeFileListResp:
		var p FileListRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileListResp(p) {
			return errors.New("bad file-list-resp payload")
		}
		return nil
	case TypeFileMkdir:
		var p FileMkdirPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileMkdir(p) {
			return errors.New("bad file-mkdir payload")
		}
		return nil
	case TypeFileDelete:
		var p FileDeletePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileDelete(p) {
			return errors.New("bad file-delete payload")
		}
		return nil
	case TypeFileRename:
		var p FileRenamePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileRename(p) {
			return errors.New("bad file-rename payload")
		}
		return nil
	case TypeFileChunk:
		var p FileChunkPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileChunk(p) {
			return errors.New("bad file-chunk payload")
		}
		return nil
	case TypeFilePullReq:
		var p FilePullReqPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFilePullReq(p) {
			return errors.New("bad file-pull-req payload")
		}
		return nil
	case TypeFileAck:
		var p FileAckPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileAck(p) {
			return errors.New("bad file-ack payload")
		}
		return nil
	case TypeFileCancel:
		var p FileCancelPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileCancel(p) {
			return errors.New("bad file-cancel payload")
		}
		return nil
	case TypeFileStatReq:
		var p FileStatReqPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileStatReq(p) {
			return errors.New("bad file-stat-req payload")
		}
		return nil
	case TypeFileStatResp:
		var p FileStatRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizeFileStatResp(p) {
			return errors.New("bad file-stat-resp payload")
		}
		return nil
	case TypePhotoList:
		var p PhotoListPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoList(p) {
			return errors.New("bad photo-list payload")
		}
		return nil
	case TypePhotoListResp:
		var p PhotoListRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoListResp(p) {
			return errors.New("bad photo-list-resp payload")
		}
		return nil
	case TypePhotoThumbReq:
		var p PhotoThumbReqPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoThumbReq(p) {
			return errors.New("bad photo-thumb-req payload")
		}
		return nil
	case TypePhotoThumbResp:
		var p PhotoThumbRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoThumbResp(p) {
			return errors.New("bad photo-thumb-resp payload")
		}
		return nil
	case TypePhotoPullReq:
		var p PhotoPullReqPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoPullReq(p) {
			return errors.New("bad photo-pull-req payload")
		}
		return nil
	case TypePhotoChunk:
		var p PhotoChunkPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoChunk(p) {
			return errors.New("bad photo-chunk payload")
		}
		return nil
	case TypePhotoDelete:
		var p PhotoDeletePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoDelete(p) {
			return errors.New("bad photo-delete payload")
		}
		return nil
	case TypePhotoDeleteResp:
		var p PhotoDeleteRespPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if !SanitizePhotoDeleteResp(p) {
			return errors.New("bad photo-delete-resp payload")
		}
		return nil
	default:
		return errors.New("unknown feature type")
	}
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	token, found := strings.CutPrefix(auth, "Bearer ")
	if !found {
		return ""
	}
	return strings.TrimSpace(token)
}
