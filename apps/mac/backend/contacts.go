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
	"strings"
	"time"

	"fuseitall/core"
)

// ContactListResult is the Wails-bound result for contacts list.
type ContactListResult struct {
	Contacts   []core.ContactEntry `json:"contacts"`
	NextCursor string              `json:"next_cursor,omitempty"`
	TotalCount int                 `json:"total_count"`
	Error      string              `json:"error,omitempty"`
	ErrorCode  string              `json:"error_code,omitempty"`
	Permission string              `json:"permission,omitempty"`
}

// ContactAvatarResult is the Wails-bound result for a contact's avatar.
type ContactAvatarResult struct {
	ContactID    string `json:"contact_id"`
	AvatarB64    string `json:"avatar_b64,omitempty"`
	PhotoVersion string `json:"photo_version,omitempty"`
	Error        string `json:"error,omitempty"`
}

// maxAvatarCacheEntries bounds the in-memory avatar store (each entry
// <=64KB; 300 caps RAM at ~19MB worst case). Eviction is oldest-touch.
const maxAvatarCacheEntries = 300

// avatarCacheGet returns the cached avatar when the stored version matches
// (or the caller has no version to compare). Caller must hold contactsMu.
func (s *Service) avatarCacheGet(contactID, expectedVersion string) (string, string, bool) {
	if s.avatarCache == nil {
		return "", "", false
	}
	b64, ok := s.avatarCache[contactID]
	if !ok || b64 == "" {
		return "", "", false
	}
	ver := ""
	if s.avatarVersions != nil {
		ver = s.avatarVersions[contactID]
	}
	if expectedVersion != "" && ver != "" && ver != expectedVersion {
		return "", ver, false
	}
	if s.avatarAt == nil {
		s.avatarAt = make(map[string]int64)
	}
	s.avatarSeq++
	s.avatarAt[contactID] = s.avatarSeq
	return b64, ver, true
}

// avatarCachePut stores an avatar and evicts oldest-touch beyond the bound.
// Caller must hold contactsMu.
func (s *Service) avatarCachePut(contactID, b64, version string) {
	if contactID == "" || b64 == "" {
		return
	}
	if s.avatarCache == nil {
		s.avatarCache = make(map[string]string)
	}
	if s.avatarVersions == nil {
		s.avatarVersions = make(map[string]string)
	}
	if s.avatarAt == nil {
		s.avatarAt = make(map[string]int64)
	}
	s.avatarCache[contactID] = b64
	if version != "" {
		s.avatarVersions[contactID] = version
	}
	s.avatarSeq++
	s.avatarAt[contactID] = s.avatarSeq
	for len(s.avatarCache) > maxAvatarCacheEntries {
		oldestID := ""
		var oldest int64
		first := true
		for id, at := range s.avatarAt {
			if first || at < oldest {
				oldest, oldestID, first = at, id, false
			}
		}
		if oldestID == "" {
			break
		}
		delete(s.avatarCache, oldestID)
		delete(s.avatarVersions, oldestID)
		delete(s.avatarAt, oldestID)
	}
}

// ListContacts returns the list of contacts. If forceRefresh is false and contacts
// are already cached, the cached list is returned immediately with zero network.
func (s *Service) ListContacts(cursor string, limit int, forceRefresh bool) (ContactListResult, error) {
	return s.ListContactsWithQuery(cursor, limit, forceRefresh, "")
}

// ListContactsWithQuery returns one page filtered by query ("" = all).
// Query is pinned into the request so phone-side filtering and Mac paging
// stay consistent; callers follow NextCursor until empty for full sync.
func (s *Service) ListContactsWithQuery(cursor string, limit int, forceRefresh bool, query string) (ContactListResult, error) {
	if len(cursor) > 256 {
		return ContactListResult{}, errors.New("invalid cursor")
	}
	if limit == 0 {
		limit = core.DefaultContactsLimit
	}
	if limit < 1 || limit > core.MaxContactsPerResp {
		return ContactListResult{}, errors.New("invalid limit")
	}
	query = strings.TrimSpace(query)
	if len(query) > core.MaxContactsQueryLen {
		return ContactListResult{}, errors.New("invalid query")
	}
	if err := core.ValidateKeysetCursor(cursor); err != nil {
		return ContactListResult{Error: "contacts cursor rejected — resync from start", ErrorCode: core.CodeCursorInvalid}, err
	}

	s.contactsMu.Lock()
	if !forceRefresh && cursor == "" && query == "" && len(s.contactsCache) > 0 {
		cached := ContactListResult{
			Contacts:   append([]core.ContactEntry{}, s.contactsCache...),
			TotalCount: len(s.contactsCache),
		}
		s.contactsMu.Unlock()
		return cached, nil
	}
	s.contactsMu.Unlock()

	if !s.IsPaired() {
		return ContactListResult{}, offlineSyncError("list contacts")
	}
	if err := s.checkPeerCapability(core.CapabilityContacts, 13); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return ContactListResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
		}
		return ContactListResult{}, err
	}

	reqID, err := freshTransferID()
	if err != nil {
		return ContactListResult{}, fmt.Errorf("fresh req_id: %w", err)
	}
	if len(reqID) > 16 {
		reqID = reqID[:16]
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return ContactListResult{}, fmt.Errorf("fresh nonce: %w", err)
	}

	ch := make(chan ContactListResult, 1)
	s.contactsMu.Lock()
	if s.pendingContactsLists == nil {
		s.pendingContactsLists = make(map[string]chan ContactListResult)
	}
	if s.pendingContactsMeta == nil {
		s.pendingContactsMeta = make(map[string]contactsReqMeta)
	}
	s.pendingContactsLists[reqID] = ch
	s.pendingContactsMeta[reqID] = contactsReqMeta{query: query, cursor: cursor, gen: s.contactsGen}
	gen := s.contactsGen
	s.contactsMu.Unlock()

	defer func() {
		s.contactsMu.Lock()
		delete(s.pendingContactsLists, reqID)
		delete(s.pendingContactsMeta, reqID)
		s.contactsMu.Unlock()
	}()

	payload := core.ContactsListReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		Cursor:    cursor,
		Limit:     limit,
		Query:     query,
		CursorGen: gen,
	}
	if err := s.sendFeatureToPhone(core.TypeContactsListReq, &payload); err != nil {
		return ContactListResult{}, fmt.Errorf("send contacts-list-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		if err := s.checkPeerCapability(core.CapabilityContacts, 13); err != nil {
			var upd *core.UpdateRequiredError
			if errors.As(err, &upd) {
				return ContactListResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
			}
			return ContactListResult{}, err
		}
		return ContactListResult{Error: "contacts listing timed out — phone did not respond", ErrorCode: core.CodeSyncTimeout}, timeoutSyncError("list contacts")
	}
}

// GetContactAvatar requests or retrieves the cached photo/avatar for a contact.
func (s *Service) GetContactAvatar(contactID string) (ContactAvatarResult, error) {
	return s.getContactAvatar(contactID, false, "")
}

// GetContactAvatarFull requests the high-res display photo for the detail
// header (thumbnail path serves lists/messages). Version-aware: pass the
// known photo_version to skip the network on a match.
func (s *Service) GetContactAvatarFull(contactID string) (ContactAvatarResult, error) {
	return s.getContactAvatar(contactID, true, "")
}

// GetContactAvatarVersioned is the version-aware fetch used by the messages
// pane: contact_id + photo_version from the thread row avoid refetching
// unchanged photos across thread list refreshes.
func (s *Service) GetContactAvatarVersioned(contactID, expectedVersion string) (ContactAvatarResult, error) {
	return s.getContactAvatar(contactID, false, expectedVersion)
}

func (s *Service) getContactAvatar(contactID string, highRes bool, expectedVersion string) (ContactAvatarResult, error) {
	sanitizedID, ok := core.SanitizeContactID(contactID)
	if !ok {
		return ContactAvatarResult{}, errors.New("invalid contact id")
	}
	contactID = sanitizedID

	s.contactsMu.Lock()
	cacheID := contactID
	if highRes {
		cacheID = contactID + "#full"
	}
	if b64, ver, ok := s.avatarCacheGet(cacheID, expectedVersion); ok {
		s.contactsMu.Unlock()
		return ContactAvatarResult{ContactID: contactID, AvatarB64: b64, PhotoVersion: ver}, nil
	}
	s.contactsMu.Unlock()

	if !s.IsPaired() {
		return ContactAvatarResult{ContactID: contactID}, offlineSyncError("get avatar")
	}
	if err := s.checkPeerCapability(core.CapabilityContacts, 13); err != nil {
		return ContactAvatarResult{ContactID: contactID}, err
	}

	reqID, err := freshTransferID()
	if err != nil {
		return ContactAvatarResult{ContactID: contactID}, fmt.Errorf("fresh req_id: %w", err)
	}
	if len(reqID) > 16 {
		reqID = reqID[:16]
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return ContactAvatarResult{ContactID: contactID}, fmt.Errorf("fresh nonce: %w", err)
	}

	ch := make(chan ContactAvatarResult, 1)
	s.contactsMu.Lock()
	if s.pendingAvatarReqs == nil {
		s.pendingAvatarReqs = make(map[string]chan ContactAvatarResult)
	}
	if s.pendingAvatarMeta == nil {
		s.pendingAvatarMeta = make(map[string]pendingAvatarMeta)
	}
	s.pendingAvatarReqs[reqID] = ch
	s.pendingAvatarMeta[reqID] = pendingAvatarMeta{highRes: highRes}
	s.contactsMu.Unlock()

	defer func() {
		s.contactsMu.Lock()
		delete(s.pendingAvatarReqs, reqID)
		delete(s.pendingAvatarMeta, reqID)
		s.contactsMu.Unlock()
	}()

	payload := core.ContactAvatarReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		ContactID: contactID,
		HighRes:   highRes,
	}
	if err := s.sendFeatureToPhone(core.TypeContactAvatarReq, &payload); err != nil {
		return ContactAvatarResult{ContactID: contactID}, fmt.Errorf("send contact-avatar-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(5 * time.Second):
		return ContactAvatarResult{ContactID: contactID, Error: "avatar request timed out", PhotoVersion: ""}, timeoutSyncError("get avatar")
	}
}

// mergeContactEntries appends src rows with unseen ContactIDs, keeping
// first-seen order. Overlapping pages must never duplicate directory rows:
// Svelte keyed each blocks throw on duplicate keys.
func mergeContactEntries(dst []core.ContactEntry, src []core.ContactEntry) []core.ContactEntry {
	if len(dst) == 0 {
		return append([]core.ContactEntry{}, src...)
	}
	seen := make(map[string]struct{}, len(dst)+len(src))
	for _, c := range dst {
		seen[c.ContactID] = struct{}{}
	}
	for _, c := range src {
		if _, ok := seen[c.ContactID]; ok {
			continue
		}
		seen[c.ContactID] = struct{}{}
		dst = append(dst, c)
	}
	return dst
}

// pruneAvatarCache drops avatar entries for contacts no longer present,
// keeping offline avatars for survivors. Caller must hold contactsMu.
func (s *Service) pruneAvatarCache() {
	if len(s.avatarCache) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(s.contactsCache))
	for _, c := range s.contactsCache {
		keep[c.ContactID] = struct{}{}
		keep[c.ContactID+"#full"] = struct{}{}
	}
	for id := range s.avatarCache {
		if _, ok := keep[id]; !ok {
			delete(s.avatarCache, id)
			delete(s.avatarVersions, id)
			delete(s.avatarAt, id)
		}
	}
}

// SearchContacts filters the cached contacts in-memory by query string.
func (s *Service) SearchContacts(query string) []core.ContactEntry {
	s.contactsMu.Lock()
	defer s.contactsMu.Unlock()

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]core.ContactEntry{}, s.contactsCache...)
	}

	var results []core.ContactEntry
	for _, c := range s.contactsCache {
		if strings.Contains(strings.ToLower(c.DisplayName), q) {
			results = append(results, c)
			continue
		}
		if c.Nickname != "" && strings.Contains(strings.ToLower(c.Nickname), q) {
			results = append(results, c)
			continue
		}
		if c.Organization != nil {
			if strings.Contains(strings.ToLower(c.Organization.Company), q) ||
				strings.Contains(strings.ToLower(c.Organization.Title), q) {
				results = append(results, c)
				continue
			}
		}
		if c.Note != "" && strings.Contains(strings.ToLower(c.Note), q) {
			results = append(results, c)
			continue
		}
		if c.Website != "" && strings.Contains(strings.ToLower(c.Website), q) {
			results = append(results, c)
			continue
		}
		matched := false
		for _, p := range c.Phones {
			if strings.Contains(strings.ToLower(p.Number), q) {
				results = append(results, c)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, e := range c.Emails {
			if strings.Contains(strings.ToLower(e.Address), q) {
				results = append(results, c)
				break
			}
		}
	}
	return results
}

// ingestContactsBody parses inbound contacts envelopes from phone.
func (s *Service) ingestContactsBody(body []byte) {
	var env core.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}

	switch env.Type {
	case core.TypeContactsListResp:
		var resp core.ContactsListRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		s.contactsMu.Lock()
		ch, ok := s.pendingContactsLists[resp.ReqID]
		meta, hasMeta := s.pendingContactsMeta[resp.ReqID]
		if ok && resp.Error == "" {
			// Gen guard: a contacts-changed wipe or reconnect that landed
			// after the request went out invalidates this page.
			if hasMeta && meta.gen != s.contactsGen {
				ok = false
			} else if hasMeta && meta.query != "" {
				// Filtered pages never pollute the unfiltered directory;
				// the waiter still gets its rows below.
			} else {
				// Populate avatar cache from inline avatars (versioned).
				for _, c := range resp.Entries {
					if c.AvatarB64 != "" {
						s.avatarCachePut(c.ContactID, c.AvatarB64, c.PhotoVersion)
					} else if c.PhotoVersion != "" {
						if s.avatarVersions == nil {
							s.avatarVersions = make(map[string]string)
						}
						// Remember the version even without bytes so the
						// UI can skip fetching known-absent photos.
						if _, exists := s.avatarCache[c.ContactID]; !exists {
							s.avatarVersions[c.ContactID] = c.PhotoVersion
						}
					}
				}
				// Only mutate the directory for a live waiter; late
				// responses after timeout/disconnect/change-wipe must not
				// resurrect stale pages. First pages replace (fixes
				// forceRefresh duplication); later pages append.
				if hasMeta && meta.cursor == "" {
					s.contactsCache = append([]core.ContactEntry{}, resp.Entries...)
				} else if resp.NextCursor == "" && (!hasMeta || meta.cursor == "") {
					s.contactsCache = resp.Entries
				} else if hasMeta {
					s.contactsCache = mergeContactEntries(s.contactsCache, resp.Entries)
				} else if resp.NextCursor == "" {
					s.contactsCache = resp.Entries
				} else {
					s.contactsCache = mergeContactEntries(s.contactsCache, resp.Entries)
				}
				s.pruneAvatarCache()
			}
		}
		s.contactsMu.Unlock()

		if ok && ch != nil {
			total := resp.TotalCount
			if total == 0 {
				total = len(resp.Entries)
			}
			select {
			case ch <- ContactListResult{
				Contacts:   resp.Entries,
				NextCursor: resp.NextCursor,
				TotalCount: total,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}:
			default:
			}
		}

	case core.TypeContactAvatarResp:
		var resp core.ContactAvatarRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		s.contactsMu.Lock()
		ch, ok := s.pendingAvatarReqs[resp.ReqID]
		avatarMeta := s.pendingAvatarMeta[resp.ReqID]
		if ok && resp.Error == "" && resp.DataB64 != "" {
			cacheID := resp.ContactID
			if avatarMeta.highRes {
				cacheID = resp.ContactID + "#full"
			}
			s.avatarCachePut(cacheID, resp.DataB64, resp.PhotoVersion)
			// Patch the directory entry too so panes rendering
			// contact.avatar_b64 update without a full refetch
			// (previously the map updated but the UI never saw it).
			if !avatarMeta.highRes {
				for i := range s.contactsCache {
					if s.contactsCache[i].ContactID == resp.ContactID {
						s.contactsCache[i].AvatarB64 = resp.DataB64
						s.contactsCache[i].PhotoVersion = resp.PhotoVersion
						break
					}
				}
			}
		}
		s.contactsMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- ContactAvatarResult{
				ContactID:    resp.ContactID,
				AvatarB64:    resp.DataB64,
				PhotoVersion: resp.PhotoVersion,
				Error:        resp.Error,
			}:
			default:
			}
		}

	case core.TypeContactsChanged:
		// Phone informs that contacts changed; invalidate directory but
		// keep avatars offline until the next list prunes survivors.
		s.contactsMu.Lock()
		s.contactsGen++
		s.contactsCache = nil
		s.contactsMu.Unlock()
	}
}
