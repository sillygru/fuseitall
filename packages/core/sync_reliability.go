// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Feature-agnostic sync reliability primitives: typed sync errors, the
// unified keyset cursor codec, and the receiver dedup cache. Every feature
// (SMS, contacts, and whatever comes next: photos, files, ...) builds on
// these instead of minting its own. Feature-specific validators live with
// their feature (SanitizeSubID in messages.go, contact sanitizers in
// contacts.go); only the shared shapes live here.
//
// Adoption checklist for a new synced listing:
//  1. Stamp Nonce on every *-req (crypto/rand via FreshNonce); correlate
//     responses on ReqID, echo the nonce.
//  2. Page with Encode/DecodeKeysetCursor: sortKey is whatever orders the
//     listing (dateMs, display name, ...), rowID is the tiebreak.
//  3. Validate inbound cursors with ValidateKeysetCursor (fail closed).
//  4. Fail all pending waiters on disconnect (never hang to timeout).
//  5. Dedupe at-least-once pushes with DedupCache (exactly-once illusion).
//  6. Bump a generation counter on invalidate; drop stale responses.
//  7. Resync-on-connect: invalidate caches and emit so panes refetch.
//
// Additive only: wire decoders ignore unknown fields, so older peers keep
// working while newer peers gain ordering and gap detection.
package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var (
	// ErrCursorInvalid means a paging cursor was rejected (stale generation,
	// undecodable v2 cursor, or semantic mismatch). Callers must drop the
	// cache and run a full resync from cursor "", never silent-drop. Maps to
	// error_code "cursor_invalid" on the wire.
	ErrCursorInvalid = errors.New("cursor invalid")
	// ErrSyncTimeout means a sync request got no correlated response before
	// its deadline. Retryable with the same req_id/client_id.
	ErrSyncTimeout = errors.New("sync timed out")
	// ErrPhoneOffline means no transport was available for a sync request.
	// Retryable on reconnect with the same req_id/client_id. The single
	// identity for this condition: features wrap it with context
	// (fmt.Errorf("...: %w", ErrPhoneOffline)) instead of declaring their
	// own sentinel, so errors.Is unifies across features.
	ErrPhoneOffline = errors.New("phone is offline")
	// ErrPermissionRevoked means the phone denied a sync op mid-session.
	// Not retryable without a re-grant; receivers must freeze (never wipe)
	// their cache.
	ErrPermissionRevoked = errors.New("sync permission revoked")
)

const (
	// CodeCursorInvalid is the machine error_code for cursor rejection.
	CodeCursorInvalid = "cursor_invalid"
	// CodeSyncTimeout is the machine error_code for correlated-response timeouts.
	CodeSyncTimeout = "timeout"
	// CodePermissionDenied is the machine error_code for revoked permissions.
	CodePermissionDenied = "permission_denied"

	// SyncDedupCap bounds receiver dedup caches (req_id/client_id/seq).
	SyncDedupCap = 500
	// MaxSyncCursorLen caps opaque cursor strings on the wire.
	MaxSyncCursorLen = 256
	// MaxSubIDLen caps the Android subscription id string.
	MaxSubIDLen = 16
	// KeysetCursorPrefix tags canonical v2 cursors. Anything without the
	// prefix is a legacy form, decoded leniently (see DecodeKeysetCursor).
	KeysetCursorPrefix = "v2"
)

// EncodeKeysetCursor builds an opaque paging cursor from a sort key and row
// id. The row id tiebreak survives duplicate sort keys (equal-millisecond
// timestamps, duplicate display names); base64 keeps arbitrary keys (spaces,
// unicode, colons, dots) opaque. SMS passes the dateMs as a decimal string;
// contacts passes the display name. Pure.
func EncodeKeysetCursor(sortKey string, rowID int64) string {
	if rowID < 0 {
		rowID = 0
	}
	enc := base64.RawURLEncoding.EncodeToString([]byte(sortKey))
	return KeysetCursorPrefix + "." + enc + "." + strconv.FormatInt(rowID, 10)
}

// DecodeKeysetCursor parses any cursor the fleet emits, old or new. Empty
// means "first page" and decodes to ("",0,true). v2 cursors decode strictly
// (fail closed); legacy forms decode leniently so old peers degrade instead
// of failing:
//
//	v2.<b64sortkey>.<id>  canonical (strict, round-trip verified)
//	<date>:<id> / <date>   legacy SMS keyset (numeric-left rule)
//	<b64name>.<id> / name  legacy contacts keyset (or any bare string)
//
// The numeric-left rule means a legacy contacts cursor for a contact
// literally named like "123:45" decodes as an SMS-shaped key in a numeric
// context; callers disambiguate by validating semantics (SMS requires a
// numeric sortKey). Vanishingly rare, self-heals on full resync. Pure.
func DecodeKeysetCursor(cursor string) (sortKey string, rowID int64, ok bool) {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return "", 0, true
	}
	if len(trimmed) > MaxSyncCursorLen {
		return "", 0, false
	}
	if strings.HasPrefix(trimmed, KeysetCursorPrefix+".") {
		return decodeKeysetV2(trimmed)
	}
	if key, id, isPair := splitCursorPair(trimmed, ":"); isPair {
		if isDigits(key) {
			return key, id, true
		}
		return trimmed, 0, true
	}
	if isDigits(trimmed) {
		return trimmed, 0, true
	}
	if key, id, isPair := splitCursorPair(trimmed, "."); isPair {
		if raw, err := base64.RawURLEncoding.DecodeString(key); err == nil {
			return string(raw), id, true
		}
		return trimmed, 0, true
	}
	return trimmed, 0, true
}

// decodeKeysetV2 strictly decodes a canonical cursor, verifying the
// decode→re-encode round trip so a legacy string that merely starts with
// "v2." (e.g. a contact literally named "v2.foo") fails closed instead of
// paging wrong. Pure.
func decodeKeysetV2(cursor string) (string, int64, bool) {
	rest := strings.TrimPrefix(cursor, KeysetCursorPrefix+".")
	key, id, isPair := splitCursorPair(rest, ".")
	if !isPair {
		return "", 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return "", 0, false
	}
	if EncodeKeysetCursor(string(raw), id) != strings.TrimSpace(cursor) {
		return "", 0, false
	}
	return string(raw), id, true
}

// splitCursorPair splits s on the last sep into (left, numericID). It
// reports false unless the tail parses as a non-negative integer. Pure.
func splitCursorPair(s, sep string) (left string, id int64, isPair bool) {
	idx := strings.LastIndex(s, sep)
	if idx < 0 {
		return "", 0, false
	}
	id, err := strconv.ParseInt(strings.TrimSpace(s[idx+1:]), 10, 64)
	if err != nil || id < 0 {
		return "", 0, false
	}
	return s[:idx], id, true
}

// isDigits reports whether s is a non-empty all-ASCII-digit string. Pure.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidateKeysetCursor fail-closes on corrupt v2 cursors and oversize
// input, and tolerates legacy forms (feature semantics validate those on
// decode: SMS requires a numeric sortKey). Call it at the top of every
// List* entry point, before cache/transport checks, so malformed input
// fails fast without a network round trip. Pure.
func ValidateKeysetCursor(cursor string) error {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > MaxSyncCursorLen {
		return fmt.Errorf("cursor too long: %w", ErrCursorInvalid)
	}
	if !strings.HasPrefix(trimmed, KeysetCursorPrefix+".") {
		return nil
	}
	if _, _, ok := DecodeKeysetCursor(trimmed); !ok {
		return fmt.Errorf("reject cursor: %w", ErrCursorInvalid)
	}
	return nil
}

// DedupCache is a bounded receiver-side seen-set for req_id/client_id/msg
// keys. At-least-once delivery plus this cache gives the exactly-once
// illusion: duplicates resend the cached ack without re-applying.
type DedupCache struct {
	mu   sync.Mutex
	cap  int
	seen map[string]struct{}
	ord  []string
}

// NewDedupCache returns a cache holding up to cap keys (SyncDedupCap when
// cap <= 0).
func NewDedupCache(cap int) *DedupCache {
	if cap <= 0 {
		cap = SyncDedupCap
	}
	return &DedupCache{cap: cap, seen: make(map[string]struct{})}
}

// Seen reports whether key was already recorded. Pure under lock.
func (d *DedupCache) Seen(key string) bool {
	if key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[key]
	return ok
}

// Mark records key, evicting the oldest when over capacity. Empty keys are
// never recorded: it returns false for "" so callers must decide explicitly
// (fail-open apply vs fail-closed reject) instead of the cache deciding.
func (d *DedupCache) Mark(key string) bool {
	if key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[key]; ok {
		return false
	}
	d.seen[key] = struct{}{}
	d.ord = append(d.ord, key)
	if len(d.ord) > d.cap {
		overflow := len(d.ord) - d.cap
		for _, old := range d.ord[:overflow] {
			delete(d.seen, old)
		}
		d.ord = append([]string{}, d.ord[overflow:]...)
	}
	return true
}
