// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"strings"

	"fuseitall/core"
)

// contactLookupSnapshot copies the cached directory under lock so matching
// runs lock-free (avoids contactsMu->messagesMu ordering hazards). Pure
// snapshot; no network.
func (s *Service) contactLookupSnapshot() []core.ContactEntry {
	s.contactsMu.Lock()
	defer s.contactsMu.Unlock()
	return append([]core.ContactEntry{}, s.contactsCache...)
}

// lookupContactForAddress finds the best directory match for a raw SMS
// address. Phone numbers match via core.PhonesEqual (E.164 vs national,
// separators, +1 prefix) including provider NORMALIZED_NUMBER; email senders
// match case-insensitively on exact address. Returns name/id/version; ok is
// false when the directory has no match. Fail-open: callers fall back to the
// raw address. Pure matching over a snapshot.
func lookupContactForAddress(contacts []core.ContactEntry, address string) (name, contactID, photoVersion string, ok bool) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" || len(contacts) == 0 {
		return "", "", "", false
	}
	if strings.Contains(trimmed, "@") {
		lower := strings.ToLower(trimmed)
		for _, c := range contacts {
			for _, e := range c.Emails {
				if strings.ToLower(strings.TrimSpace(e.Address)) == lower {
					return c.DisplayName, c.ContactID, c.PhotoVersion, true
				}
			}
		}
		return "", "", "", false
	}
	// Prefer normalized provider form, then raw numbers, both via PhonesEqual.
	for _, c := range contacts {
		for _, p := range c.Phones {
			if p.Normalized != "" && core.PhonesEqual(p.Normalized, trimmed) {
				return c.DisplayName, c.ContactID, c.PhotoVersion, true
			}
		}
	}
	for _, c := range contacts {
		for _, p := range c.Phones {
			if p.Number != "" && core.PhonesEqual(p.Number, trimmed) {
				return c.DisplayName, c.ContactID, c.PhotoVersion, true
			}
		}
	}
	return "", "", "", false
}

// LookupContactForAddress is the Wails-bound fallback join: Mac-side sender
// resolution against the cached directory when the phone's PhoneLookup missed
// (denied permission, unsaved sender, format mismatch). Returns empty strings
// when unknown; the UI renders contact_name||address.
func (s *Service) LookupContactForAddress(address string) (map[string]string, error) {
	snapshot := s.contactLookupSnapshot()
	name, id, ver, ok := lookupContactForAddress(snapshot, address)
	if !ok {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	if name != "" {
		out["contact_name"] = name
	}
	if id != "" {
		out["contact_id"] = id
	}
	if ver != "" {
		out["photo_version"] = ver
	}
	return out, nil
}

// FindSMSThreadForAddress returns the cached thread_id whose address matches
// (PhonesEqual) so Contacts->Message handoffs select the existing thread
// instead of opening a duplicate compose. 0 = no match.
func (s *Service) FindSMSThreadForAddress(address string) int64 {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return 0
	}
	s.messagesMu.Lock()
	defer s.messagesMu.Unlock()
	if strings.Contains(trimmed, "@") {
		lower := strings.ToLower(trimmed)
		for _, t := range s.threadsCache {
			if strings.ToLower(strings.TrimSpace(t.Address)) == lower {
				return t.ThreadID
			}
		}
		return 0
	}
	for _, t := range s.threadsCache {
		if core.PhonesEqual(t.Address, trimmed) {
			return t.ThreadID
		}
	}
	return 0
}

// enrichThreadWithContacts fills empty thread identity from the directory
// snapshot. Non-empty phone-provided values always win.
func enrichThreadWithContacts(t *core.SMSThread, contacts []core.ContactEntry) {
	if t == nil || (t.ContactID != "" && t.ContactName != "") {
		return
	}
	name, id, ver, ok := lookupContactForAddress(contacts, t.Address)
	if !ok {
		return
	}
	if t.ContactName == "" {
		t.ContactName = name
	}
	if t.ContactID == "" {
		t.ContactID = id
	}
	if t.PhotoVersion == "" {
		t.PhotoVersion = ver
	}
}

// enrichMessageWithContacts fills empty per-message identity from the
// directory snapshot, then sanitizes (fail-soft to ""). Non-empty
// phone-provided values always win.
func enrichMessageWithContacts(m *core.SMSMessage, contacts []core.ContactEntry) {
	if m == nil || (m.ContactID != "" && m.ContactName != "") {
		if m != nil {
			core.SanitizeSMSMessage(m)
		}
		return
	}
	name, id, ver, ok := lookupContactForAddress(contacts, m.Address)
	if ok {
		if m.ContactName == "" {
			m.ContactName = name
		}
		if m.ContactID == "" {
			m.ContactID = id
		}
		if m.PhotoVersion == "" {
			m.PhotoVersion = ver
		}
	}
	core.SanitizeSMSMessage(m)
}
