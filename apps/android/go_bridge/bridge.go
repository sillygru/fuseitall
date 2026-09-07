// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package main is the Android c-shared bridge over fuseitall/core. It runs
// the phone-side TLS ping server (POST /ping, gated by core) in-process so
// Dart FFI can start it without JNI/Java. The exported C surface is:
// PhoneStart, PhoneStartWithCert, PhoneCertPEM, PhoneKeyPEM, PhonePoll,
// PhoneStop, PhoneFree.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"fuseitall/core"
)

var (
	mu             sync.Mutex
	httpServer     *http.Server
	listener       net.Listener
	events         chan string
	featEvents     chan string
	actualPort     int
	currentCertPEM string
	currentKeyPEM  string
)

// phoneBindAddr is 0.0.0.0 (all interfaces), not 127.0.0.1: the phone-side
// server must be reachable from the LAN peer (the Mac connects to the
// advertised reply_port over Wi-Fi). Gating stays in core (token + version
// gate); the bind only controls reachability, never auth.
const phoneBindAddr = "0.0.0.0"

// statusRecorder captures the status code so the sniffer only reports pings
// the core server actually accepted (200), never rejected ones.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader implements http.ResponseWriter.
func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}

// extractPingNonce returns the payload nonce of a ping envelope, or "" when
// the body is not a ping. Unknown fields are ignored by encoding/json.
func extractPingNonce(body []byte) string {
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			Nonce string `json:"nonce"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	if env.Type != core.TypePing || env.Payload.Nonce == "" {
		return ""
	}
	return env.Payload.Nonce
}

// sniffAcceptedPings wraps the core handler: it pre-reads POST /ping bodies
// to learn the nonce, replays the body untouched to core, and queues the
// nonce only when core answers 200. Accepted feature posts (/notif, /clip,
// /settings) are queued whole for Dart to apply (clipboard writes, settings
// adoption, dismissal cancels). Gating stays entirely in core: only 200s
// are ever queued, rejected bodies never reach Dart.
func sniffAcceptedPings(inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var nonce string
		var feature string
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/ping":
				if body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxBodyBytes)); err == nil {
					nonce = extractPingNonce(body)
					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = int64(len(body))
				}
			case "/notif", "/clip", "/settings":
				if body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxBodyBytes)); err == nil {
					feature = string(body)
					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = int64(len(body))
				}
			}
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		inner.ServeHTTP(rec, r)
		if rec.status != http.StatusOK {
			return
		}
		if nonce != "" {
			select {
			case events <- nonce:
			default:
			}
		}
		if feature != "" {
			select {
			case featEvents <- feature:
			default:
			}
		}
	})
}

// serveWithCert binds the TLS listener for an already-built core server and
// records its PEM identity for later persistence. Callers hold mu.
func serveWithCert(srv *core.Server, port int, certPEM, keyPEM string) (string, bool) {
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{srv.TLSCertificate()},
	}
	ln, err := tls.Listen("tcp", net.JoinHostPort(phoneBindAddr, strconv.Itoa(port)), cfg)
	if err != nil {
		return "", false
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return "", false
	}
	events = make(chan string, 64)
	featEvents = make(chan string, 64)
	hs := &http.Server{
		Handler:           sniffAcceptedPings(srv.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpServer = hs
	listener = ln
	actualPort = addr.Port
	currentCertPEM = certPEM
	currentKeyPEM = keyPEM
	// Capture locals: the goroutine must not re-read the globals after mu
	// is released (goStop nils them on shutdown — a data race under -race).
	go func(s *http.Server, l net.Listener) {
		_ = s.Serve(l)
	}(hs, ln)
	return fmt.Sprintf("%d:%s", actualPort, srv.CertFingerprint()), true
}

// phoneCaps is the capability set the phone server advertises: presence
// plus the 0.2.0 features (notifications, clipboard, settings-sync).
// Rebuild the .so (task build:android) to ship this to devices.
func phoneCaps() []string {
	return []string{
		core.CapabilityPing,
		core.CapabilityNotifications,
		core.CapabilityClipboard,
		core.CapabilitySettingsSync,
	}
}

// goStart holds all of PhoneStart's logic in pure Go (testable without
// cgo): it returns "actualPort:fingerprint" and true only when serving.
// The minted cert PEMs are retained for goCertPEM/goKeyPEM so Dart can
// persist the phone identity and reuse it across restarts.
func goStart(token string, port int) (string, bool) {
	if token == "" || port < 0 || port > 65535 {
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	if httpServer != nil {
		return "", false
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := core.NewServer(token, "android", phoneCaps(), logger)
	if err != nil {
		return "", false
	}
	certPEM, keyPEM, err := core.EncodeTLSCertPEM(srv.TLSCertificate())
	if err != nil {
		return "", false
	}
	return serveWithCert(srv, port, string(certPEM), string(keyPEM))
}

// goStartWithCert restarts the server with a persisted cert (stable phone
// identity across restarts). PEMs are validated via core.ParseTLSCertPEM;
// bad PEMs fail closed so the caller can mint fresh. Pure Go, no cgo.
func goStartWithCert(token string, port int, certPEM, keyPEM string) (string, bool) {
	if token == "" || port < 0 || port > 65535 {
		return "", false
	}
	if certPEM == "" || keyPEM == "" {
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	if httpServer != nil {
		return "", false
	}
	cert, fp, err := core.ParseTLSCertPEM([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return "", false
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := core.NewServerWithCert(token, "android", phoneCaps(), logger, cert, fp)
	if err != nil {
		return "", false
	}
	return serveWithCert(srv, port, certPEM, keyPEM)
}

// goCertPEM returns the active server cert PEM, or "" when not serving.
func goCertPEM() string {
	mu.Lock()
	defer mu.Unlock()
	if httpServer == nil {
		return ""
	}
	return currentCertPEM
}

// goKeyPEM returns the active server key PEM, or "" when not serving.
func goKeyPEM() string {
	mu.Lock()
	defer mu.Unlock()
	if httpServer == nil {
		return ""
	}
	return currentKeyPEM
}

// goPoll returns the next accepted-ping nonce, false when the queue is empty.
func goPoll() (string, bool) {
	select {
	case nonce := <-events:
		return nonce, true
	default:
		return "", false
	}
}

// goPollEvent returns the next accepted feature envelope (raw JSON for
// /notif, /clip, /settings posts), false when the queue is empty. Dart
// parses the type and applies it (clipboard writes, settings adoption).
func goPollEvent() (string, bool) {
	select {
	case raw := <-featEvents:
		return raw, true
	default:
		return "", false
	}
}

// goStop shuts the server down: 0 on stop, 1 when nothing was running.
// The persisted PEMs are kept in memory so goCertPEM/goKeyPEM stay readable
// until the next start overwrites them; serving state is fully cleared.
func goStop() int {
	mu.Lock()
	defer mu.Unlock()
	if httpServer == nil {
		return 1
	}
	_ = httpServer.Close()
	_ = listener.Close()
	httpServer = nil
	listener = nil
	actualPort = 0
	for {
		select {
		case <-events:
		default:
			goto drainFeatures
		}
	}
drainFeatures:
	for {
		select {
		case <-featEvents:
		default:
			return 0
		}
	}
}

// PhoneStart launches the phone-side TLS ping server. token is the pairing
// token (must be non-empty); port is the bind port, 0 for ephemeral.
// Returns a malloc'd "actualPort:fingerprint" string (free with PhoneFree),
// or NULL when validation fails, the server is already running, or startup
// fails. Fail closed on every path: no server runs unless fully started.
//
//export PhoneStart
func PhoneStart(tokenC *C.char, port C.int) *C.char {
	if tokenC == nil {
		return nil
	}
	raw, ok := goStart(C.GoString(tokenC), int(port))
	if !ok {
		return nil
	}
	return C.CString(raw)
}

// PhoneStartWithCert launches the server with a persisted cert (stable phone
// identity). certC/keyC are PEM strings; bad PEMs fail closed with NULL so
// Dart can fall back to PhoneStart (mint fresh) and persist the new PEMs.
//
//export PhoneStartWithCert
func PhoneStartWithCert(tokenC *C.char, port C.int, certC *C.char, keyC *C.char) *C.char {
	if tokenC == nil || certC == nil || keyC == nil {
		return nil
	}
	raw, ok := goStartWithCert(C.GoString(tokenC), int(port), C.GoString(certC), C.GoString(keyC))
	if !ok {
		return nil
	}
	return C.CString(raw)
}

// PhoneCertPEM returns a malloc'd PEM of the active server cert, or NULL
// when not serving. Free with PhoneFree. Lets Dart persist the identity
// minted by PhoneStart so the next launch reuses it via PhoneStartWithCert.
//
//export PhoneCertPEM
func PhoneCertPEM() *C.char {
	pem := goCertPEM()
	if pem == "" {
		return nil
	}
	return C.CString(pem)
}

// PhoneKeyPEM returns a malloc'd PEM of the active server key, or NULL when
// not serving. Free with PhoneFree.
//
//export PhoneKeyPEM
func PhoneKeyPEM() *C.char {
	pem := goKeyPEM()
	if pem == "" {
		return nil
	}
	return C.CString(pem)
}

// PhonePoll returns a malloc'd nonce string for the next accepted ping, or
// NULL when none is queued. Non-blocking; free results with PhoneFree.
//
//export PhonePoll
func PhonePoll() *C.char {
	nonce, ok := goPoll()
	if !ok {
		return nil
	}
	return C.CString(nonce)
}

// PhonePollEvent returns a malloc'd raw JSON envelope for the next accepted
// feature post (/notif, /clip, /settings), or NULL when none is queued.
// Non-blocking; free results with PhoneFree. Older Dart builds without this
// symbol simply never see Mac-initiated pushes (presence unaffected).
//
//export PhonePollEvent
func PhonePollEvent() *C.char {
	raw, ok := goPollEvent()
	if !ok {
		return nil
	}
	return C.CString(raw)
}

// PhoneStop shuts the server down. Returns 0 on stop, 1 when no server was
// running.
//
//export PhoneStop
func PhoneStop() C.int {
	return C.int(goStop())
}

// PhoneFree releases strings returned by PhoneStart/PhoneStartWithCert/
// PhoneCertPEM/PhoneKeyPEM/PhonePoll.
//
//export PhoneFree
func PhoneFree(s *C.char) {
	C.free(unsafe.Pointer(s))
}

func main() {}
