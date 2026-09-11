// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"strings"
	"time"
)

// PairStatus is the typed pairing-listener state for the frontend PairCard.
// It answers the silent-failure question "phone shows my Mac name but the
// Mac stays on the pairing screen with no logs": either no attempt reached
// us yet (wrong host/firewall/TLS pin), or attempts were rejected (stale
// token/version). Facts/PII never ride here — only coordinates and kinds.
type PairStatus struct {
	// Listening is true once ServePairServer bound :port.
	Listening bool
	// QRHost/QRPort/QRCandidates are the boot-time QR coordinates the phone
	// dials first (primary + Happy-Eyeballs fallbacks).
	QRHost       string
	QRPort       int
	QRCandidates []string
	// LastRejectKind is "", "auth", "update", or "bad_request".
	LastRejectKind string
	LastRejectUnix int64
	// LastAcceptUnix is the last accepted phone contact (HTTP 200 ping or
	// WS connect/envelope). Zero means no phone ever reached us.
	LastAcceptUnix int64
}

// GetPairStatus returns the live listener status. Typed binding: the PairCard
// renders "waiting at host:port" vs "last attempt rejected" from this, never
// from log scraping. Always returns a value; unknown fields are zero.
func (s *Service) GetPairStatus() PairStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.demoMode {
		return s.demoPairStatusLocked()
	}
	st := PairStatus{
		Listening:      s.listening,
		LastRejectKind: s.lastRejectKind,
		LastRejectUnix: s.lastRejectUnix,
		LastAcceptUnix: s.lastAcceptUnix,
	}
	if s.qrConfigured {
		st.QRHost = s.qrInputs.Host
		st.QRPort = s.qrInputs.Port
		st.QRCandidates = splitCandidates(s.qrInputs.Candidates)
	}
	return st
}

// markListening records that ServePairServer bound addr. Called once at
// startup; idempotent.
func (s *Service) markListening(addr string) {
	s.mu.Lock()
	s.listening = true
	s.listenAddr = addr
	s.mu.Unlock()
}

// recordAccept stamps the last accepted phone contact. Called on HTTP 200
// ping capture, WS connect, and WS ping envelopes. Silent: callers log/emit.
func (s *Service) recordAccept() {
	s.mu.Lock()
	s.lastAcceptUnix = time.Now().Unix()
	s.mu.Unlock()
}

// recordReject stamps the last rejected phone attempt by kind. Kinds are
// low-cardinality ("auth", "update", "bad_request", "wrong_method") —
// never tokens, fingerprints, or device names.
func (s *Service) recordReject(kind string) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	s.mu.Lock()
	s.lastRejectKind = kind
	s.lastRejectUnix = time.Now().Unix()
	s.mu.Unlock()
}

// splitCandidates parses the QR comma-separated candidate list. Pure.
func splitCandidates(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		t := strings.TrimSpace(part)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}
