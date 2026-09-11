// SPDX-License-Identifier: AGPL-3.0-only

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

	// Register the live waiter first: ingest only mutates the directory for
	// a correlated in-flight request (late responses after timeout or a
	// change-wipe must not resurrect stale pages).
	listCh := make(chan ContactListResult, 1)
	svc.contactsMu.Lock()
	if svc.pendingContactsLists == nil {
		svc.pendingContactsLists = make(map[string]chan ContactListResult)
	}
	svc.pendingContactsLists["req-1"] = listCh
	svc.contactsMu.Unlock()

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

func TestContactsFilteredPageDoesNotPolluteCache(t *testing.T) {
	svc := NewService("", "", "", nil)
	svc.contactsMu.Lock()
	svc.contactsCache = []core.ContactEntry{{ContactID: "c1", DisplayName: "Alice"}}
	svc.pendingContactsLists = map[string]chan ContactListResult{"req-f": make(chan ContactListResult, 1)}
	svc.pendingContactsMeta = map[string]contactsReqMeta{"req-f": {query: "bob", cursor: "", gen: svc.contactsGen}}
	svc.contactsMu.Unlock()

	payloadBytes, _ := json.Marshal(core.ContactsListRespPayload{
		Nonce: "n", ReqID: "req-f",
		Entries: []core.ContactEntry{{ContactID: "c2", DisplayName: "Bob"}},
	})
	envBytes, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeContactsListResp,
		Sender:    core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload:   payloadBytes,
	})
	svc.ingestContactsBody(envBytes)

	svc.contactsMu.Lock()
	defer svc.contactsMu.Unlock()
	if len(svc.contactsCache) != 1 || svc.contactsCache[0].ContactID != "c1" {
		t.Fatalf("filtered page polluted cache: %v", svc.contactsCache)
	}
}

func TestContactsFirstPageReplacesCache(t *testing.T) {
	svc := NewService("", "", "", nil)
	svc.contactsMu.Lock()
	svc.contactsCache = []core.ContactEntry{{ContactID: "stale", DisplayName: "Stale"}}
	svc.pendingContactsLists = map[string]chan ContactListResult{"req-1": make(chan ContactListResult, 1)}
	svc.pendingContactsMeta = map[string]contactsReqMeta{"req-1": {query: "", cursor: "", gen: svc.contactsGen}}
	svc.contactsMu.Unlock()

	payloadBytes, _ := json.Marshal(core.ContactsListRespPayload{
		Nonce: "n", ReqID: "req-1", NextCursor: "v2.abc.9",
		Entries: []core.ContactEntry{{ContactID: "fresh", DisplayName: "Fresh"}},
	})
	envBytes, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeContactsListResp,
		Sender:    core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload:   payloadBytes,
	})
	svc.ingestContactsBody(envBytes)

	svc.contactsMu.Lock()
	defer svc.contactsMu.Unlock()
	if len(svc.contactsCache) != 1 || svc.contactsCache[0].ContactID != "fresh" {
		t.Fatalf("first page should replace, got %v", svc.contactsCache)
	}
}

func TestAvatarCacheVersionedAndBounded(t *testing.T) {
	svc := NewService("", "", "", nil)
	svc.contactsMu.Lock()
	// Version mismatch must miss.
	svc.avatarCachePut("c1", "aaa", "v1")
	if _, _, ok := svc.avatarCacheGet("c1", "v2"); ok {
		t.Fatal("stale version should miss")
	}
	if b64, _, ok := svc.avatarCacheGet("c1", "v1"); !ok || b64 != "aaa" {
		t.Fatal("matching version should hit")
	}
	// Bound: push beyond capacity, oldest evicted.
	for i := 0; i < maxAvatarCacheEntries+10; i++ {
		svc.avatarCachePut("k"+string(rune('a'+i%26))+itoaTest(i), "x", "v")
	}
	if len(svc.avatarCache) > maxAvatarCacheEntries {
		t.Fatalf("avatar cache unbounded: %d", len(svc.avatarCache))
	}
	svc.contactsMu.Unlock()
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestContactsDeleteIngest(t *testing.T) {
	svc := NewService("", "", "", nil)

	ch := make(chan ContactDeleteResult, 1)
	svc.contactsMu.Lock()
	if svc.pendingDeleteReqs == nil {
		svc.pendingDeleteReqs = make(map[string]chan ContactDeleteResult)
	}
	svc.pendingDeleteReqs["del-1"] = ch
	svc.contactsMu.Unlock()

	payloadBytes, _ := json.Marshal(core.ContactDeleteRespPayload{
		ReqID:     "del-1",
		ContactID: "c1",
		OK:        true,
	})
	envBytes, _ := json.Marshal(core.Envelope{
		ProtocolV: 1, Type: core.TypeContactDeleteResp,
		Sender:  core.SenderInfo{Platform: "android", AppBuild: 13},
		Payload: payloadBytes,
	})
	svc.ingestContactsBody(envBytes)

	select {
	case res := <-ch:
		if !res.OK || res.ContactID != "c1" {
			t.Fatalf("unexpected delete result: %+v", res)
		}
	default:
		t.Fatal("expected delete response on channel")
	}
}

