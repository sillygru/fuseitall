// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"path/filepath"
	"testing"

	"fuseitall/core"
)

func TestDBCacheRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// 1. Contacts
	contacts := []core.ContactEntry{
		{
			ContactID:   "c1",
			DisplayName: "Alice Smith",
			Phones:      []core.ContactPhone{{Number: "+15551234567", Type: "mobile"}},
			Emails:      []core.ContactEmail{{Address: "alice@example.com"}},
		},
		{
			ContactID:   "c2",
			DisplayName: "Bob Jones",
			Phones:      []core.ContactPhone{{Number: "5559876543", Type: "home"}},
		},
	}
	if err := db.SaveContacts(contacts); err != nil {
		t.Fatalf("SaveContacts: %v", err)
	}

	loadedContacts, err := db.LoadAllContacts()
	if err != nil {
		t.Fatalf("LoadAllContacts: %v", err)
	}
	if len(loadedContacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(loadedContacts))
	}
	if loadedContacts[0].DisplayName != "Alice Smith" || loadedContacts[1].DisplayName != "Bob Jones" {
		t.Errorf("contacts unexpected order or content: %+v", loadedContacts)
	}

	// 2. Avatars
	if err := db.SaveAvatar("c1", "aW1hZ2VkYXRh", "v1"); err != nil {
		t.Fatalf("SaveAvatar: %v", err)
	}
	avatars, versions, err := db.LoadAllAvatars()
	if err != nil {
		t.Fatalf("LoadAllAvatars: %v", err)
	}
	if avatars["c1"] != "aW1hZ2VkYXRh" || versions["c1"] != "v1" {
		t.Errorf("avatar unexpected: avatars=%v versions=%v", avatars, versions)
	}

	// 3. Threads & Messages
	threads := []core.SMSThread{
		{
			ThreadID:     101,
			Address:      "+15551234567",
			ContactName:  "Alice Smith",
			Snippet:      "Hello there",
			Date:         1700000000000,
			MessageCount: 1,
			UnreadCount:  1,
			Read:         false,
		},
	}
	if err := db.SaveThreads(threads); err != nil {
		t.Fatalf("SaveThreads: %v", err)
	}

	msgs := []core.SMSMessage{
		{
			ID:       1,
			ThreadID: 101,
			Address:  "+15551234567",
			Body:     "Hello there",
			Date:     1700000000000,
			Type:     1,
			Read:     false,
		},
	}
	if err := db.SaveMessages(msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	loadedThreads, err := db.LoadAllThreads()
	if err != nil {
		t.Fatalf("LoadAllThreads: %v", err)
	}
	if len(loadedThreads) != 1 || loadedThreads[0].UnreadCount != 1 || loadedThreads[0].Read {
		t.Errorf("unexpected loaded thread: %+v", loadedThreads)
	}

	loadedMsgs, err := db.LoadMessagesForThread(101, 10)
	if err != nil {
		t.Fatalf("LoadMessagesForThread: %v", err)
	}
	if len(loadedMsgs) != 1 || loadedMsgs[0].Body != "Hello there" || loadedMsgs[0].Read {
		t.Errorf("unexpected loaded msgs: %+v", loadedMsgs)
	}

	// 4. Mark Thread Read
	if err := db.MarkThreadReadInDB(101); err != nil {
		t.Fatalf("MarkThreadReadInDB: %v", err)
	}
	loadedThreads, _ = db.LoadAllThreads()
	if len(loadedThreads) != 1 || loadedThreads[0].UnreadCount != 0 || !loadedThreads[0].Read {
		t.Errorf("thread was not marked read: %+v", loadedThreads[0])
	}
	loadedMsgs, _ = db.LoadMessagesForThread(101, 10)
	if len(loadedMsgs) != 1 || !loadedMsgs[0].Read {
		t.Errorf("message was not marked read: %+v", loadedMsgs[0])
	}

	// 5. Clear All
	if err := db.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	loadedContacts, _ = db.LoadAllContacts()
	loadedThreads, _ = db.LoadAllThreads()
	if len(loadedContacts) != 0 || len(loadedThreads) != 0 {
		t.Errorf("ClearAll did not empty tables: contacts=%d threads=%d", len(loadedContacts), len(loadedThreads))
	}
}
