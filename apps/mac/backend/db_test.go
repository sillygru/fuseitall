// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"fuseitall/core"
)

func TestDBCacheRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	testKey, err := core.DeriveDBKey("test-db-key")
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(dbPath, testKey)
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

func TestLoadMessagesPaginationAndOrder(t *testing.T) {
	tmpDir := t.TempDir()
	testKey, err := core.DeriveDBKey("test-db-key-pagination")
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenDB(filepath.Join(tmpDir, "test.db"), testKey)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Insert 15 messages for thread 200 with timestamps 1000..15000
	var msgs []core.SMSMessage
	for i := 1; i <= 15; i++ {
		msgs = append(msgs, core.SMSMessage{
			ID:       int64(i),
			ThreadID: 200,
			Address:  "+15559876543",
			Body:     fmt.Sprintf("Message %d", i),
			Date:     int64(i * 1000),
			Type:     1,
			Read:     true,
		})
	}
	if err := db.SaveMessages(msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	// LoadMessagesForThread with limit 5 should load the 5 most recent messages (11..15) chronologically
	recent, err := db.LoadMessagesForThread(200, 5)
	if err != nil {
		t.Fatalf("LoadMessagesForThread: %v", err)
	}
	if len(recent) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(recent))
	}
	if recent[0].ID != 11 || recent[4].ID != 15 {
		t.Fatalf("expected messages 11..15, got %d..%d", recent[0].ID, recent[4].ID)
	}

	// LoadMessagesBeforeCursor before message 11 (date 11000, ID 11) with limit 5 should load messages 6..10
	older, err := db.LoadMessagesBeforeCursor(200, 11000, 11, 5)
	if err != nil {
		t.Fatalf("LoadMessagesBeforeCursor: %v", err)
	}
	if len(older) != 5 {
		t.Fatalf("expected 5 older messages, got %d", len(older))
	}
	if older[0].ID != 6 || older[4].ID != 10 {
		t.Fatalf("expected older messages 6..10, got %d..%d", older[0].ID, older[4].ID)
	}

	// Next page before message 6 should load messages 1..5
	oldest, err := db.LoadMessagesBeforeCursor(200, 6000, 6, 5)
	if err != nil {
		t.Fatalf("LoadMessagesBeforeCursor oldest: %v", err)
	}
	if len(oldest) != 5 {
		t.Fatalf("expected 5 oldest messages, got %d", len(oldest))
	}
	if oldest[0].ID != 1 || oldest[4].ID != 5 {
		t.Fatalf("expected oldest messages 1..5, got %d..%d", oldest[0].ID, oldest[4].ID)
	}

	// Querying before message 1 should return 0 messages
	empty, err := db.LoadMessagesBeforeCursor(200, 1000, 1, 5)
	if err != nil {
		t.Fatalf("LoadMessagesBeforeCursor empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected 0 messages before oldest, got %d", len(empty))
	}
}

func TestDBEncryptionAtRest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "encrypted_test.db")

	testKey, err := core.DeriveDBKey("master-secret-12345")
	if err != nil {
		t.Fatal(err)
	}

	db, err := OpenDB(dbPath, testKey)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}

	secretBody := "Top secret SMS message containing sensitive information 42"
	secretAddress := "+19998887777"
	secretContact := "Secret Agent"

	if err := db.SaveMessages([]core.SMSMessage{
		{
			ID:          1,
			ThreadID:    1,
			Address:     secretAddress,
			Body:        secretBody,
			Date:        1234567,
			Type:        1,
			Read:        true,
			ContactName: secretContact,
		},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	if err := db.SaveThreads([]core.SMSThread{
		{
			ThreadID:    1,
			Address:     secretAddress,
			ContactName: secretContact,
			Snippet:     secretBody,
			Date:        1234567,
		},
	}); err != nil {
		t.Fatalf("SaveThreads: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Directly inspect SQLite disk file with standard sql.Open
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("raw Open: %v", err)
	}
	defer rawDB.Close()

	var rawBody, rawMsgAddr string
	if err := rawDB.QueryRow("SELECT body, address FROM messages WHERE id = 1").Scan(&rawBody, &rawMsgAddr); err != nil {
		t.Fatalf("raw query messages: %v", err)
	}

	// Assert ciphertext at rest: must start with enc:v1: and must NOT contain plaintext
	if !strings.HasPrefix(rawBody, core.EncryptedPrefix) {
		t.Fatalf("raw body is not encrypted: %s", rawBody)
	}
	if strings.Contains(rawBody, secretBody) {
		t.Fatalf("plaintext leaked into raw DB body: %s", rawBody)
	}

	if !strings.HasPrefix(rawMsgAddr, core.EncryptedPrefix) {
		t.Fatalf("raw address is not encrypted: %s", rawMsgAddr)
	}
	if strings.Contains(rawMsgAddr, secretAddress) {
		t.Fatalf("plaintext phone number leaked into raw DB address: %s", rawMsgAddr)
	}

	var rawSnippet, rawThreadAddr string
	if err := rawDB.QueryRow("SELECT snippet, address FROM threads WHERE thread_id = 1").Scan(&rawSnippet, &rawThreadAddr); err != nil {
		t.Fatalf("raw query threads: %v", err)
	}
	if !strings.HasPrefix(rawSnippet, core.EncryptedPrefix) || strings.Contains(rawSnippet, secretBody) {
		t.Fatalf("plaintext snippet leaked: %s", rawSnippet)
	}
	if !strings.HasPrefix(rawThreadAddr, core.EncryptedPrefix) || strings.Contains(rawThreadAddr, secretAddress) {
		t.Fatalf("plaintext thread address leaked: %s", rawThreadAddr)
	}

	// Reopening with the correct key decrypts successfully
	db2, err := OpenDB(dbPath, testKey)
	if err != nil {
		t.Fatalf("reopen OpenDB: %v", err)
	}
	defer db2.Close()

	msgs, err := db2.LoadMessagesForThread(1, 10)
	if err != nil {
		t.Fatalf("LoadMessagesForThread: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != secretBody || msgs[0].Address != secretAddress {
		t.Fatalf("decrypted mismatch: got %+v", msgs)
	}

	threads, err := db2.LoadAllThreads()
	if err != nil {
		t.Fatalf("LoadAllThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].Snippet != secretBody || threads[0].Address != secretAddress {
		t.Fatalf("decrypted thread mismatch: got %+v", threads)
	}
}

func TestDBKeyMismatchFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "mismatch_test.db")

	key1, _ := core.DeriveDBKey("key-1")
	key2, _ := core.DeriveDBKey("key-2")

	db1, err := OpenDB(dbPath, key1)
	if err != nil {
		t.Fatal(err)
	}

	_ = db1.SaveMessages([]core.SMSMessage{
		{ID: 1, ThreadID: 1, Address: "123", Body: "Secret", Date: 100},
	})
	_ = db1.Close()

	// Open with different key: decryption should fail closed and not return wrong/corrupted data
	db2, err := OpenDB(dbPath, key2)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	msgs, err := db2.LoadMessagesForThread(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Failed-closed: cannot decrypt, so messages are not emitted
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages with wrong key, got %d", len(msgs))
	}
}

