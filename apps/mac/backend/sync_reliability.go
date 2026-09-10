// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
	"fmt"

	"fuseitall/core"
)

// envelopeTypeOf extracts the wire type without full validation so HTTP
// fallback paths can decide whether a push/invalidate event (emit) or a
// waiter-resolved response (no emit) arrived. Fail-open true on parse
// failure: a missed emit heals on the next push, a missed response never
// resolves.
func envelopeTypeOf(body []byte) string {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	return env.Type
}

// isContactsInvalidateBody reports whether an inbound contacts body warrants
// a contacts:changed fan-out (push/invalidate only, never pure resps).
func isContactsInvalidateBody(body []byte) bool {
	t := envelopeTypeOf(body)
	if t == "" {
		return true
	}
	return t == core.TypeContactsChanged
}

// isMessagesInvalidateBody reports whether an inbound SMS body warrants a
// messages:changed fan-out (push/changed only; resps + send-ack resolve
// their waiter via the return path).
func isMessagesInvalidateBody(body []byte) bool {
	t := envelopeTypeOf(body)
	if t == "" {
		return true
	}
	return t == core.TypeSMSPush || t == core.TypeSMSChanged
}

// contactsReqMeta tracks the request shape behind a pending contacts list
// so ingest can decide cache mutation: filtered (query != "") pages never
// pollute the unfiltered directory, and first pages replace instead of
// appending. Gen drops list-while-changed races.
type contactsReqMeta struct {
	query  string
	cursor string
	gen    int64
}

// smsPageMeta tracks the cursor + generation behind a pending SMS page so
// first pages replace the cache instead of appending to stale entries.
type smsPageMeta struct {
	cursor   string
	threadID int64
	gen      int64
}

// pendingAvatarMeta tracks whether an avatar request wants the high-res
// display photo, so ingest stores it under the "#full" namespace.
type pendingAvatarMeta struct {
	highRes bool
}

// failPendingContactsLists unblocks all in-flight contacts listings when the
// peer disconnects so callers fail fast instead of hanging to timeout.
func (s *Service) failPendingContactsLists(err error) {
	s.contactsMu.Lock()
	defer s.contactsMu.Unlock()
	for reqID, ch := range s.pendingContactsLists {
		select {
		case ch <- ContactListResult{Error: err.Error(), ErrorCode: core.CodeSyncTimeout}:
		default:
		}
		delete(s.pendingContactsLists, reqID)
		delete(s.pendingContactsMeta, reqID)
	}
}

// failPendingAvatarReqs unblocks all in-flight avatar fetches on disconnect.
func (s *Service) failPendingAvatarReqs(err error) {
	s.contactsMu.Lock()
	defer s.contactsMu.Unlock()
	for reqID, ch := range s.pendingAvatarReqs {
		select {
		case ch <- ContactAvatarResult{Error: err.Error()}:
		default:
		}
		delete(s.pendingAvatarReqs, reqID)
		delete(s.pendingAvatarMeta, reqID)
	}
}

// failPendingSMSRequests unblocks all in-flight SMS requests on disconnect:
// thread listings, message pages, and outbound sends.
func (s *Service) failPendingSMSRequests(err error) {
	s.messagesMu.Lock()
	defer s.messagesMu.Unlock()
	for reqID, ch := range s.pendingThreadsReqs {
		select {
		case ch <- SMSThreadsResult{Error: err.Error(), ErrorCode: core.CodeSyncTimeout}:
		default:
		}
		delete(s.pendingThreadsReqs, reqID)
		delete(s.pendingThreadsMeta, reqID)
	}
	for reqID, ch := range s.pendingMessagesReqs {
		select {
		case ch <- SMSMessagesResult{Error: err.Error(), ErrorCode: core.CodeSyncTimeout}:
		default:
		}
		delete(s.pendingMessagesReqs, reqID)
		delete(s.pendingMessagesMeta, reqID)
	}
	for reqID, ch := range s.pendingSendReqs {
		select {
		case ch <- SMSSendResult{Ok: false, Error: err.Error(), ErrorCode: core.CodeSyncTimeout}:
		default:
		}
		delete(s.pendingSendReqs, reqID)
	}
}

// failPendingSyncRequests fails every sync pending waiter (contacts + SMS)
// with the same offline error. Called from OnWSDisconnect so no sync call
// hangs to its 5-12s timeout after the transport already knows the peer is
// gone. Follows the failPendingPhotoRequests pattern.
func (s *Service) failPendingSyncRequests(err error) {
	s.failPendingContactsLists(err)
	s.failPendingAvatarReqs(err)
	s.failPendingSMSRequests(err)
}

// markSMSPushSeen records a push key in the shared dedup cache. Returns
// false when the push is a duplicate. Empty keys fail open (treated as new)
// so legacy pushes without any id still apply; the per-thread id check in
// ingestMessagesBody is the backstop there. Caller must hold messagesMu for
// cache-mutation ordering (DedupCache itself is goroutine-safe).
func (s *Service) markSMSPushSeen(key string) bool {
	if key == "" {
		return true
	}
	if s.smsPushDedup == nil {
		s.smsPushDedup = core.NewDedupCache(0)
	}
	return s.smsPushDedup.Mark(key)
}

// offlineSyncError builds the fail-closed offline error for sync calls.
// Log-OR-return: callers log at the seam, we only wrap here.
func offlineSyncError(op string) error {
	return fmt.Errorf("%s: %w", op, core.ErrPhoneOffline)
}

// timeoutSyncResult marks a timeout result with the machine code so UIs
// branch on code, never on message strings.
func timeoutSyncError(op string) error {
	return fmt.Errorf("%s: %w", op, core.ErrSyncTimeout)
}

// isPermissionSyncError reports whether err (or its message) signals a
// revoked phone permission, so receivers freeze caches instead of wiping.
func isPermissionSyncError(err error, code, permission string) bool {
	if errors.Is(err, core.ErrPermissionRevoked) {
		return true
	}
	return code == core.CodePermissionDenied && permission != ""
}
