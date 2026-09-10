// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"fuseitall/core"
)

func TestMergeHelpersDropDuplicates(t *testing.T) {
	threads := mergeSMSThreads(
		[]core.SMSThread{{ThreadID: 53}, {ThreadID: 54}},
		[]core.SMSThread{{ThreadID: 53}, {ThreadID: 55}},
	)
	if len(threads) != 3 || threads[0].ThreadID != 53 || threads[2].ThreadID != 55 {
		t.Fatalf("threads merge kept duplicates: %+v", threads)
	}
	msgs := mergeSMSMessages(
		[]core.SMSMessage{{ID: 1}, {ID: 2}},
		[]core.SMSMessage{{ID: 2}, {ID: 3}},
	)
	if len(msgs) != 3 {
		t.Fatalf("messages merge kept duplicates: %+v", msgs)
	}
	contacts := mergeContactEntries(
		[]core.ContactEntry{{ContactID: "a"}},
		[]core.ContactEntry{{ContactID: "a"}, {ContactID: "b"}},
	)
	if len(contacts) != 2 {
		t.Fatalf("contacts merge kept duplicates: %+v", contacts)
	}
}

func TestMessagesOfflineValidation(t *testing.T) {
	svc := NewService("", "", "", nil)

	// Blank recipient/body rejected
	if _, err := svc.SendSMS("", "hello"); err == nil {
		t.Fatal("expected error for empty recipient")
	}
	if _, err := svc.SendSMS("+15551234567", ""); err == nil {
		t.Fatal("expected error for empty body")
	}

	// Unpaired/offline send returns error
	if _, err := svc.SendSMS("+15551234567", "hello"); err == nil {
		t.Fatal("expected error when phone is offline")
	}
}

func TestMessagesIngestAndPush(t *testing.T) {
	svc := NewService("", "", "", nil)

	// Register the live waiter first: ingest only mutates the cache for a
	// correlated in-flight request (late responses after timeout/disconnect
	// must not resurrect stale pages).
	threadsCh := make(chan SMSThreadsResult, 1)
	svc.messagesMu.Lock()
	if svc.pendingThreadsReqs == nil {
		svc.pendingThreadsReqs = make(map[string]chan SMSThreadsResult)
	}
	svc.pendingThreadsReqs["req-threads"] = threadsCh
	svc.messagesMu.Unlock()

	// Ingest threads response
	threadsPayload := core.SMSThreadsRespPayload{
		ReqID: "req-threads",
		Threads: []core.SMSThread{
			{
				ThreadID: 1,
				Address:  "+15551234567",
				Snippet:  "Hey there",
				Date:     1700000000000,
			},
		},
	}
	payloadBytes, _ := json.Marshal(threadsPayload)
	env := core.Envelope{
		ProtocolV:    1,
		Type:         core.TypeSMSThreadsResp,
		Sender:       core.SenderInfo{Platform: "android", AppBuild: 13},
		Capabilities: []string{core.CapabilityMessages},
		Payload:      payloadBytes,
	}
	envBytes, _ := json.Marshal(env)
	svc.ingestMessagesBody(envBytes)

	svc.messagesMu.Lock()
	if len(svc.threadsCache) != 1 || svc.threadsCache[0].Snippet != "Hey there" {
		t.Fatalf("unexpected threads cache: %v", svc.threadsCache)
	}
	svc.messagesMu.Unlock()

	// Ingest push for incoming message in the same thread
	pushPayload := core.SMSPushPayload{
		Message: core.SMSMessage{
			ID:       10,
			ThreadID: 1,
			Address:  "+15551234567",
			Body:     "New incoming reply",
			Date:     1700000010000,
			Type:     core.SMSMsgTypeInbox,
		},
	}
	pushBytes, _ := json.Marshal(pushPayload)
	pushEnv := core.Envelope{
		ProtocolV: 1,
		Type:      core.TypeSMSPush,
		Sender:    core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload:   pushBytes,
	}
	pushEnvBytes, _ := json.Marshal(pushEnv)
	svc.ingestMessagesBody(pushEnvBytes)

	svc.messagesMu.Lock()
	if len(svc.threadsCache) != 1 || svc.threadsCache[0].Snippet != "New incoming reply" {
		t.Fatalf("thread snippet not updated: %v", svc.threadsCache)
	}
	if svc.threadsCache[0].UnreadCount != 1 {
		t.Fatalf("expected unreadCount 1, got %d", svc.threadsCache[0].UnreadCount)
	}
	svc.messagesMu.Unlock()

	// Verify MarkThreadRead clears unread count
	if err := svc.MarkThreadRead(1); err != nil {
		t.Fatalf("MarkThreadRead: %v", err)
	}
	svc.messagesMu.Lock()
	if svc.threadsCache[0].UnreadCount != 0 || !svc.threadsCache[0].Read {
		t.Fatalf("thread not marked read: %+v", svc.threadsCache[0])
	}
	svc.messagesMu.Unlock()
}

func TestListSMSMessagesCacheCursorAndOlderPaging(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := OpenDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	svc := NewService("", "", "", nil)
	svc.db = db

	// Save 10 messages for thread 10 (dates 1000..10000)
	var msgs []core.SMSMessage
	for i := 1; i <= 10; i++ {
		msgs = append(msgs, core.SMSMessage{
			ID:       int64(i),
			ThreadID: 10,
			Address:  "+15551234567",
			Body:     fmt.Sprintf("Msg %d", i),
			Date:     int64(i * 1000),
			Type:     1,
			Read:     true,
		})
	}
	if err := db.SaveMessages(msgs); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	// 1. Initial list with limit 5 (should return most recent 5 messages 6..10, with NextCursor set to oldest msg 6)
	res, err := svc.ListSMSMessages(10, "", 5, false)
	if err != nil {
		t.Fatalf("ListSMSMessages: %v", err)
	}
	if len(res.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(res.Messages))
	}
	if res.Messages[0].ID != 6 || res.Messages[4].ID != 10 {
		t.Fatalf("expected messages 6..10, got %d..%d", res.Messages[0].ID, res.Messages[4].ID)
	}
	if res.NextCursor == "" {
		t.Fatalf("expected NextCursor to be set for cached messages, got empty string")
	}

	// 2. Fetch older messages using the returned NextCursor
	olderRes, err := svc.ListSMSMessages(10, res.NextCursor, 5, false)
	if err != nil {
		t.Fatalf("ListSMSMessages with cursor: %v", err)
	}
	if len(olderRes.Messages) != 5 {
		t.Fatalf("expected 5 older messages, got %d", len(olderRes.Messages))
	}
	if olderRes.Messages[0].ID != 1 || olderRes.Messages[4].ID != 5 {
		t.Fatalf("expected older messages 1..5, got %d..%d", olderRes.Messages[0].ID, olderRes.Messages[4].ID)
	}
}
