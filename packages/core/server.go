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
	"time"
)

// MaxBodyBytes caps inbound request bodies (fixed 1 MiB cap, no knob until
// ping payloads outgrow it). Exported so the Mac/bridge adapters reuse the
// exact same cap instead of triplicating the literal.
const MaxBodyBytes = 1 << 20

// Server is the TLS self-signed (TOFU) HTTP server for the BASE milestone:
// POST /ping (gated) and GET /health (liveness only).
type Server struct {
	platform    string
	token       string
	caps        []string
	cert        tls.Certificate
	fingerprint string
	logger      *slog.Logger
	mux         *http.ServeMux
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
	return s, nil
}

// Handler exposes the routes for embedding or tests.
func (s *Server) Handler() http.Handler { return s.mux }

// CertFingerprint is the hex SHA-256 of the server cert (TOFU pin).
func (s *Server) CertFingerprint() string { return s.fingerprint }

// TLSCertificate returns the server cert for listeners.
func (s *Server) TLSCertificate() tls.Certificate { return s.cert }

// Sender returns this server's advertised sender info.
func (s *Server) Sender() SenderInfo {
	return SenderInfo{Platform: s.platform, AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
}

// Serve listens with HTTPS using the self-signed cert.
func (s *Server) Serve(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
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
	if !VerifyToken(s.token, bearerToken(r)) {
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

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	token, found := strings.CutPrefix(auth, "Bearer ")
	if !found {
		return ""
	}
	return strings.TrimSpace(token)
}
