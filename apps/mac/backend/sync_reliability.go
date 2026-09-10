// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"errors"
	"fmt"

	"fuseitall/core"
)

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
	}
	for reqID, ch := range s.pendingMessagesReqs {
		select {
		case ch <- SMSMessagesResult{Error: err.Error(), ErrorCode: core.CodeSyncTimeout}:
		default:
		}
		delete(s.pendingMessagesReqs, reqID)
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
