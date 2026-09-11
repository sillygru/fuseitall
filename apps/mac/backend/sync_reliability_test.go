// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/json"
	"errors"
	"testing"

	"fuseitall/core"
)

func TestLateThreadsRespDoesNotPoisonCache(t *testing.T) {
	svc := NewService("", "", "", nil)
	payload := core.SMSThreadsRespPayload{
		ReqID:   "ghost-req",
		Threads: []core.SMSThread{{ThreadID: 9, Address: "+1000", Date: 1}},
	}
	raw, _ := json.Marshal(payload)
	env, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeSMSThreadsResp,
		Sender: core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload: raw,
	})
	svc.ingestMessagesBody(env)
	svc.messagesMu.Lock()
	defer svc.messagesMu.Unlock()
	if len(svc.threadsCache) != 0 {
		t.Fatalf("late resp must not populate cache, got %v", svc.threadsCache)
	}
}

func TestDuplicatePushDeduped(t *testing.T) {
	svc := NewService("", "", "", nil)
	push := core.SMSPushPayload{
		Message: core.SMSMessage{ID: 77, ThreadID: 5, Address: "+1000", Body: "hi", Date: 100},
	}
	raw, _ := json.Marshal(push)
	env, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeSMSPush,
		Sender: core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload: raw,
	})
	svc.ingestMessagesBody(env)
	svc.ingestMessagesBody(env) // redelivery
	svc.messagesMu.Lock()
	defer svc.messagesMu.Unlock()
	if len(svc.threadsCache) != 1 {
		t.Fatalf("duplicate push must not duplicate thread, got %v", svc.threadsCache)
	}
	if svc.threadsCache[0].UnreadCount != 1 {
		t.Fatalf("duplicate push must not bump unread twice, got %d", svc.threadsCache[0].UnreadCount)
	}
}

func TestFailPendingSyncUnblocks(t *testing.T) {
	svc := NewService("", "", "", nil)
	threadsCh := make(chan SMSThreadsResult, 1)
	contactsCh := make(chan ContactListResult, 1)
	sendCh := make(chan SMSSendResult, 1)
	svc.messagesMu.Lock()
	if svc.pendingThreadsReqs == nil {
		svc.pendingThreadsReqs = make(map[string]chan SMSThreadsResult)
	}
	if svc.pendingSendReqs == nil {
		svc.pendingSendReqs = make(map[string]chan SMSSendResult)
	}
	svc.pendingThreadsReqs["t1"] = threadsCh
	svc.pendingSendReqs["s1"] = sendCh
	svc.messagesMu.Unlock()
	svc.contactsMu.Lock()
	if svc.pendingContactsLists == nil {
		svc.pendingContactsLists = make(map[string]chan ContactListResult)
	}
	svc.pendingContactsLists["c1"] = contactsCh
	svc.contactsMu.Unlock()

	svc.failPendingSyncRequests(errors.New("phone is offline — reconnect first"))

	select {
	case res := <-threadsCh:
		if res.Error == "" || res.ErrorCode != core.CodeSyncTimeout {
			t.Fatalf("threads waiter should fail with timeout code, got %+v", res)
		}
	default:
		t.Fatal("threads waiter not unblocked")
	}
	select {
	case res := <-contactsCh:
		if res.Error == "" {
			t.Fatalf("contacts waiter should fail, got %+v", res)
		}
	default:
		t.Fatal("contacts waiter not unblocked")
	}
	select {
	case res := <-sendCh:
		if res.Ok || res.Error == "" {
			t.Fatalf("send waiter should fail closed, got %+v", res)
		}
	default:
		t.Fatal("send waiter not unblocked")
	}
}

func TestContactsChangedBumpsGen(t *testing.T) {
	svc := NewService("", "", "", nil)
	env, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeContactsChanged,
		Sender:  core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload: []byte(`{"changed_at":1}`),
	})
	svc.ingestContactsBody(env)
	svc.contactsMu.Lock()
	defer svc.contactsMu.Unlock()
	if svc.contactsGen != 1 {
		t.Fatalf("contacts-changed should bump gen, got %d", svc.contactsGen)
	}
	if svc.contactsCache != nil {
		t.Fatalf("contacts-changed should clear directory, got %v", svc.contactsCache)
	}
}

func TestCorruptV2CursorFailsClosed(t *testing.T) {
	svc := NewService("", "", "", nil) // offline: validation runs before transport

	if _, err := svc.ListSMSThreads("v2.!!!.5", 50, true); !errors.Is(err, core.ErrCursorInvalid) {
		t.Fatalf("corrupt threads cursor should fail with ErrCursorInvalid, got %v", err)
	}
	if _, err := svc.ListSMSMessages(1, "v2.foo", 50, true); !errors.Is(err, core.ErrCursorInvalid) {
		t.Fatalf("corrupt messages cursor should fail with ErrCursorInvalid, got %v", err)
	}
	res, err := svc.ListContactsWithQuery("v2.eA.eHh4", 50, true, "")
	if !errors.Is(err, core.ErrCursorInvalid) || res.ErrorCode != core.CodeCursorInvalid {
		t.Fatalf("corrupt contacts cursor should fail with cursor_invalid, got %+v / %v", res, err)
	}
	// Legacy cursors still pass validation (fail-open, phone degrades).
	if err := core.ValidateKeysetCursor("1700000010000:42"); err != nil {
		t.Fatalf("legacy SMS cursor should validate, got %v", err)
	}
}

func TestOfflineUnifiesOnCoreSentinel(t *testing.T) {
	if !errors.Is(ErrPhoneOffline, core.ErrPhoneOffline) {
		t.Fatal("backend.ErrPhoneOffline must unwrap to core.ErrPhoneOffline")
	}
	wrapped := offlineSyncError("list threads")
	if !errors.Is(wrapped, core.ErrPhoneOffline) {
		t.Fatalf("offlineSyncError must wrap core.ErrPhoneOffline, got %v", wrapped)
	}
}
