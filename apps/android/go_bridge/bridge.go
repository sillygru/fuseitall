// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Package main is the Android c-shared bridge over fuseitall/core. It runs
// the phone-side TLS ping server (POST /ping, gated by core) in-process so
// Dart FFI can start it without JNI/Java. Realtime: features ride the
// persistent TLS WebSocket (phone_websocket.dart), never an FFI poll queue.
// The exported C surface is: PhoneStart, PhoneStartWithCert, PhoneCertPEM,
// PhoneKeyPEM, PhoneStop, PhoneLastError, PhoneFree.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"crypto/tls"
	"fmt"
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
	actualPort     int
	currentCertPEM string
	currentKeyPEM  string
	lastStartError string
)

// phoneBindAddr is 0.0.0.0 (all interfaces), not 127.0.0.1: the phone-side
// server must be reachable from the LAN peer (the Mac connects to the
// advertised reply_port over Wi-Fi). Gating stays in core (token + version
// gate); the bind only controls reachability, never auth.
const phoneBindAddr = "0.0.0.0"

// Realtime note: no sniff/queue layer. The core handler gates (token +
// version) and answers directly. Presence + features ride the persistent TLS
// WebSocket in Dart (phone_websocket.dart); the HTTP server here is only the
// reachable LAN endpoint for Mac dials, never a poll queue.

// serveWithCert binds the TLS listener for an already-built core server and
// records its PEM identity for later persistence. Callers hold mu.
func serveWithCert(srv *core.Server, port int, certPEM, keyPEM string) (string, bool) {
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{srv.TLSCertificate()},
	}
	ln, err := tls.Listen("tcp", net.JoinHostPort(phoneBindAddr, strconv.Itoa(port)), cfg)
	if err != nil {
		lastStartError = fmt.Sprintf("listen %s:%d: %v", phoneBindAddr, port, err)
		return "", false
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		lastStartError = "listener is not TCP"
		return "", false
	}
	hs := &http.Server{
		Handler:           srv.Handler(),
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
// plus the 0.2.0 features (notifications, clipboard, settings-sync),
// 0.5.0 file manager, 0.7.0 photos (0.8.0 adds video), and 0.10.0 playback.
// Rebuild the .so (task build:android) to ship.
func phoneCaps() []string {
	return []string{
		core.CapabilityPing,
		core.CapabilityNotifications,
		core.CapabilityClipboard,
		core.CapabilitySettingsSync,
		core.CapabilityFiles,
		core.CapabilityPhotos,
		core.CapabilityPlayback,
	}
}

// goStart holds all of PhoneStart's logic in pure Go (testable without
// cgo): it returns "actualPort:fingerprint" and true only when serving.
// The minted cert PEMs are retained for goCertPEM/goKeyPEM so Dart can
// persist the phone identity and reuse it across restarts.
func goStart(token string, port int) (string, bool) {
	if token == "" {
		lastStartError = "pair token must not be empty"
		return "", false
	}
	if port < 0 || port > 65535 {
		lastStartError = fmt.Sprintf("invalid port %d", port)
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	if httpServer != nil {
		lastStartError = "phone server already started"
		return "", false
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := core.NewServer(token, "android", phoneCaps(), logger)
	if err != nil {
		lastStartError = fmt.Sprintf("create server: %v", err)
		return "", false
	}
	certPEM, keyPEM, err := core.EncodeTLSCertPEM(srv.TLSCertificate())
	if err != nil {
		lastStartError = fmt.Sprintf("encode cert: %v", err)
		return "", false
	}
	res, ok := serveWithCert(srv, port, string(certPEM), string(keyPEM))
	if !ok {
		if lastStartError == "" || lastStartError == "phone server already started" {
			// serveWithCert sets lastStartError on failure
			if lastStartError == "" {
				lastStartError = "failed to bind TLS listener"
			}
		}
		return "", false
	}
	lastStartError = ""
	return res, true
}

// goStartWithCert restarts the server with a persisted cert (stable phone
// identity across restarts). PEMs are validated via core.ParseTLSCertPEM;
// bad PEMs fail closed so the caller can mint fresh. Pure Go, no cgo.
func goStartWithCert(token string, port int, certPEM, keyPEM string) (string, bool) {
	if token == "" {
		lastStartError = "pair token must not be empty"
		return "", false
	}
	if port < 0 || port > 65535 {
		lastStartError = fmt.Sprintf("invalid port %d", port)
		return "", false
	}
	if certPEM == "" || keyPEM == "" {
		lastStartError = "cert or key PEM must not be empty"
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	if httpServer != nil {
		lastStartError = "phone server already started"
		return "", false
	}
	cert, fp, err := core.ParseTLSCertPEM([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		lastStartError = fmt.Sprintf("parse cert PEM: %v", err)
		return "", false
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv, err := core.NewServerWithCert(token, "android", phoneCaps(), logger, cert, fp)
	if err != nil {
		lastStartError = fmt.Sprintf("create server: %v", err)
		return "", false
	}
	res, ok := serveWithCert(srv, port, certPEM, keyPEM)
	if !ok {
		if lastStartError == "phone server already started" || lastStartError == "" {
			if lastStartError == "" {
				lastStartError = "failed to bind TLS listener"
			}
		}
		return "", false
	}
	lastStartError = ""
	return res, true
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

// goLastError returns the last start failure for diagnostics. Empty when
// the last start succeeded or no start was attempted.
func goLastError() string {
	mu.Lock()
	defer mu.Unlock()
	return lastStartError
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
	return 0
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

// PhoneStop shuts the server down. Returns 0 on stop, 1 when no server was
// running.
//
//export PhoneStop
func PhoneStop() C.int {
	return C.int(goStop())
}

// PhoneLastError returns a malloc'd string with the last start failure,
// or NULL when the last start succeeded. Free with PhoneFree. Lets Dart
// surface the real bind/cert error instead of a generic “failed to start”.
//
//export PhoneLastError
func PhoneLastError() *C.char {
	pem := goLastError()
	if pem == "" {
		return nil
	}
	return C.CString(pem)
}

// PhoneFree releases strings returned by PhoneStart/PhoneStartWithCert/
// PhoneCertPEM/PhoneKeyPEM/PhoneLastError.
//
//export PhoneFree
func PhoneFree(s *C.char) {
	C.free(unsafe.Pointer(s))
}

func main() {}
