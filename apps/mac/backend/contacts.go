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
	s.pendingContactsLists[reqID] = ch
	s.contactsMu.Unlock()

	defer func() {
		s.contactsMu.Lock()
		delete(s.pendingContactsLists, reqID)
		s.contactsMu.Unlock()
	}()

	payload := core.ContactsListReqPayload{
		Nonce:  nonce,
		ReqID:  reqID,
		Cursor: cursor,
		Limit:  limit,
		Query:  strings.TrimSpace(query),
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
	sanitizedID, ok := core.SanitizeContactID(contactID)
	if !ok {
		return ContactAvatarResult{}, errors.New("invalid contact id")
	}
	contactID = sanitizedID

	s.contactsMu.Lock()
	if s.avatarCache != nil {
		if b64, ok := s.avatarCache[contactID]; ok {
			ver := ""
			if s.avatarVersions != nil {
				ver = s.avatarVersions[contactID]
			}
			s.contactsMu.Unlock()
			return ContactAvatarResult{ContactID: contactID, AvatarB64: b64, PhotoVersion: ver}, nil
		}
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
	s.pendingAvatarReqs[reqID] = ch
	s.contactsMu.Unlock()

	defer func() {
		s.contactsMu.Lock()
		delete(s.pendingAvatarReqs, reqID)
		s.contactsMu.Unlock()
	}()

	payload := core.ContactAvatarReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		ContactID: contactID,
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

// pruneAvatarCache drops avatar entries for contacts no longer present,
// keeping offline avatars for survivors. Caller must hold contactsMu.
func (s *Service) pruneAvatarCache() {
	if len(s.avatarCache) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(s.contactsCache))
	for _, c := range s.contactsCache {
		keep[c.ContactID] = struct{}{}
	}
	for id := range s.avatarCache {
		if _, ok := keep[id]; !ok {
			delete(s.avatarCache, id)
			delete(s.avatarVersions, id)
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
		if ok && resp.Error == "" {
			if s.avatarCache == nil {
				s.avatarCache = make(map[string]string)
			}
			if s.avatarVersions == nil {
				s.avatarVersions = make(map[string]string)
			}
			// Populate avatar cache from inline avatars
			for _, c := range resp.Entries {
				if c.AvatarB64 != "" {
					s.avatarCache[c.ContactID] = c.AvatarB64
				}
			}
			// Only mutate the directory for a live waiter; late
			// responses after timeout/disconnect/change-wipe must not
			// resurrect stale pages.
			if resp.NextCursor == "" {
				s.contactsCache = resp.Entries
			} else {
				s.contactsCache = append(s.contactsCache, resp.Entries...)
			}
			s.pruneAvatarCache()
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
		if ok && resp.Error == "" && resp.DataB64 != "" {
			if s.avatarCache == nil {
				s.avatarCache = make(map[string]string)
			}
			if s.avatarVersions == nil {
				s.avatarVersions = make(map[string]string)
			}
			s.avatarCache[resp.ContactID] = resp.DataB64
			if resp.PhotoVersion != "" {
				s.avatarVersions[resp.ContactID] = resp.PhotoVersion
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
