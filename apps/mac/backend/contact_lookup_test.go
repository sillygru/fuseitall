// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"testing"

	"fuseitall/core"
)

func TestLookupContactForAddress(t *testing.T) {
	contacts := []core.ContactEntry{
		{
			ContactID: "c1", DisplayName: "Alice", PhotoVersion: "5",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 123-4567", Normalized: "+15551234567"},
			},
		},
		{
			ContactID: "c2", DisplayName: "Bob",
			Emails: []core.ContactEmail{{Address: "Bob@Example.com"}},
		},
	}
	if name, id, _, ok := lookupContactForAddress(contacts, "5551234567"); !ok || id != "c1" || name != "Alice" {
		t.Fatalf("E.164 vs national failed: %q %q %v", name, id, ok)
	}
	if _, id, _, ok := lookupContactForAddress(contacts, "+15551234567"); !ok || id != "c1" {
		t.Fatalf("exact E.164 failed: %q %v", id, ok)
	}
	if _, id, _, ok := lookupContactForAddress(contacts, "bob@example.com"); !ok || id != "c2" {
		t.Fatalf("email match failed: %q %v", id, ok)
	}
	if _, _, _, ok := lookupContactForAddress(contacts, "9990001234"); ok {
		t.Fatal("unknown number matched")
	}
	if _, _, _, ok := lookupContactForAddress(nil, "5551234567"); ok {
		t.Fatal("empty directory matched")
	}
}

func TestEnrichThreadAndMessage(t *testing.T) {
	contacts := []core.ContactEntry{
		{ContactID: "c1", DisplayName: "Alice", PhotoVersion: "5",
			Phones: []core.ContactPhone{{Number: "(555) 123-4567"}}},
	}
	th := core.SMSThread{ThreadID: 1, Address: "+15551234567"}
	enrichThreadWithContacts(&th, contacts)
	if th.ContactID != "c1" || th.ContactName != "Alice" {
		t.Fatalf("thread not enriched: %+v", th)
	}
	// Phone-provided values win over directory.
	th2 := core.SMSThread{ThreadID: 2, Address: "+15551234567", ContactName: "Phone", ContactID: "p9"}
	enrichThreadWithContacts(&th2, contacts)
	if th2.ContactID != "p9" || th2.ContactName != "Phone" {
		t.Fatalf("phone value overwritten: %+v", th2)
	}
	m := core.SMSMessage{ID: 1, ThreadID: 1, Address: "5551234567"}
	enrichMessageWithContacts(&m, contacts)
	if m.ContactID != "c1" || m.ContactName != "Alice" {
		t.Fatalf("message not enriched: %+v", m)
	}
}

func TestFindSMSThreadForAddress(t *testing.T) {
	svc := NewService("", "", "", nil)
	svc.threadsCache = []core.SMSThread{
		{ThreadID: 11, Address: "+15551234567"},
		{ThreadID: 12, Address: "user@example.com"},
	}
	if id := svc.FindSMSThreadForAddress("(555) 123-4567"); id != 11 {
		t.Fatalf("normalized thread match failed: %d", id)
	}
	if id := svc.FindSMSThreadForAddress("USER@EXAMPLE.COM"); id != 12 {
		t.Fatalf("email thread match failed: %d", id)
	}
	if id := svc.FindSMSThreadForAddress("9990001234"); id != 0 {
		t.Fatalf("unknown matched: %d", id)
	}
}
