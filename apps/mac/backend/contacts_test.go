// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"testing"

	"fuseitall/core"
)

func TestContactsSearchAndCache(t *testing.T) {
	svc := NewService("", "", "", nil)

	svc.contactsMu.Lock()
	svc.contactsCache = []core.ContactEntry{
		{
			ContactID:   "c1",
			DisplayName: "Alice Smith",
			Phones: []core.ContactPhone{
				{Number: "+15551112222", Type: "mobile", IsPrimary: true},
			},
			Emails: []core.ContactEmail{
				{Address: "alice@example.com", Type: "home"},
			},
		},
		{
			ContactID:   "c2",
			DisplayName: "Bob Jones",
			Phones: []core.ContactPhone{
				{Number: "+15553334444", Type: "work"},
			},
		},
	}
	svc.contactsMu.Unlock()

	// Empty query returns all
	all := svc.SearchContacts("")
	if len(all) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(all))
	}

	// Match by name
	res := svc.SearchContacts("alice")
	if len(res) != 1 || res[0].ContactID != "c1" {
		t.Fatalf("expected alice, got %v", res)
	}

	// Match by phone
	res = svc.SearchContacts("3334444")
	if len(res) != 1 || res[0].ContactID != "c2" {
		t.Fatalf("expected bob by phone, got %v", res)
	}

	// Match by email
	res = svc.SearchContacts("example.com")
	if len(res) != 1 || res[0].ContactID != "c1" {
		t.Fatalf("expected alice by email, got %v", res)
	}

	// Non-matching
	res = svc.SearchContacts("charlie")
	if len(res) != 0 {
		t.Fatalf("expected 0, got %d", len(res))
	}
}

func TestContactsIngestListAndAvatar(t *testing.T) {
	svc := NewService("", "", "", nil)

	// Ingest contacts list response
	listPayload := core.ContactsListRespPayload{
		ReqID: "req-1",
		Entries: []core.ContactEntry{
			{
				ContactID:   "c100",
				DisplayName: "Dana Scully",
				AvatarB64:   "fake-scully-avatar",
			},
		},
		TotalCount: 1,
	}
	payloadBytes, _ := json.Marshal(listPayload)
	env := core.Envelope{
		ProtocolV:    1,
		Type:         core.TypeContactsListResp,
		Sender:       core.SenderInfo{Platform: "android", AppBuild: 13},
		Capabilities: []string{core.CapabilityContacts},
		Payload:      payloadBytes,
	}
	envBytes, _ := json.Marshal(env)

	svc.ingestContactsBody(envBytes)

	svc.contactsMu.Lock()
	if len(svc.contactsCache) != 1 || svc.contactsCache[0].DisplayName != "Dana Scully" {
		t.Fatalf("contactsCache mismatch: %v", svc.contactsCache)
	}
	if svc.avatarCache["c100"] != "fake-scully-avatar" {
		t.Fatalf("avatarCache mismatch: %v", svc.avatarCache["c100"])
	}
	svc.contactsMu.Unlock()

	// Ingest contacts changed event
	changedEnv := core.Envelope{
		ProtocolV: 1,
		Type:      core.TypeContactsChanged,
		Sender:    core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload:   []byte(`{}`),
	}
	changedBytes, _ := json.Marshal(changedEnv)
	svc.ingestContactsBody(changedBytes)

	svc.contactsMu.Lock()
	if svc.contactsCache != nil {
		t.Fatalf("expected nil cache after contacts-changed, got %v", svc.contactsCache)
	}
	svc.contactsMu.Unlock()
}
