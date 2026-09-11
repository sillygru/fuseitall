// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContactsSanitize(t *testing.T) {
	// ID
	if _, ok := SanitizeContactID(""); ok {
		t.Fatal("expected empty ID to be rejected")
	}
	if id, ok := SanitizeContactID("  12345  "); !ok || id != "12345" {
		t.Fatalf("expected 12345, got %q, ok=%v", id, ok)
	}
	if _, ok := SanitizeContactID(strings.Repeat("a", 70)); ok {
		t.Fatal("expected oversize ID to be rejected")
	}

	// Name
	if n := SanitizeContactName("  Alice Smith  "); n != "Alice Smith" {
		t.Fatalf("expected Alice Smith, got %q", n)
	}
	longName := strings.Repeat("x", 200)
	if n := SanitizeContactName(longName); len([]rune(n)) > MaxContactNameLen {
		t.Fatalf("expected capped at %d, got %d", MaxContactNameLen, len([]rune(n)))
	}

	// Phone
	if p, ok := SanitizePhoneNumber("+1 (555) 019-2834"); !ok || p != "+1 (555) 019-2834" {
		t.Fatalf("expected valid phone, got %q, ok=%v", p, ok)
	}
	if _, ok := SanitizePhoneNumber("bad;drop table;"); ok {
		t.Fatal("expected invalid characters to be rejected")
	}

	// Email
	if em, ok := SanitizeEmailAddress("alice@example.com"); !ok || em != "alice@example.com" {
		t.Fatalf("expected valid email, got %q, ok=%v", em, ok)
	}
	if _, ok := SanitizeEmailAddress("invalid-email"); ok {
		t.Fatal("expected email without @ to be rejected")
	}
}

func TestContactsExtendedFieldsRoundTrip(t *testing.T) {
	entry := ContactEntry{
		ContactID:     "c-9",
		DisplayName:   "Extended Person",
		PhotoVersion:  "12:34:5678",
		PhotoURI:      "content://contacts/photos/9",
		BirthdayMs:    631152000000,
		Nickname:      "Ext",
		Note:          "met at conf",
		Website:       "https://example.com",
		Organization:  &ContactOrganization{Company: "Acme", Title: "Eng"},
		Postal:        &ContactPostal{Formatted: "1 Main St", Type: "home"},
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded ContactEntry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.PhotoVersion != "12:34:5678" || decoded.BirthdayMs != 631152000000 {
		t.Fatalf("extended fields lost: %+v", decoded)
	}
	if decoded.Organization == nil || decoded.Organization.Company != "Acme" {
		t.Fatalf("org lost: %+v", decoded)
	}
	if got := SanitizeContactNickname("  Bob  "); got != "Bob" {
		t.Fatalf("nickname sanitize: %q", got)
	}
	if got := SanitizeContactPhotoURI("content://x"); got == "" {
		t.Fatal("photo uri rejected")
	}
	if SanitizeContactPhotoURI("bad\x00uri") != "" {
		t.Fatal("control char uri accepted")
	}
}

func TestContactsSerialization(t *testing.T) {	entry := ContactEntry{
		ContactID:   "c-1",
		DisplayName: "Bob Jones",
		Phones: []ContactPhone{
			{Number: "+1234567890", Type: "mobile", IsPrimary: true},
		},
		Emails: []ContactEmail{
			{Address: "bob@example.com", Type: "home"},
		},
		Starred: true,
	}

	resp := ContactsListRespPayload{
		Nonce:      "n1",
		ReqID:      "r1",
		Entries:    []ContactEntry{entry},
		NextCursor: "next",
		TotalCount: 1,
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ContactsListRespPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Nonce != "n1" || len(decoded.Entries) != 1 || decoded.Entries[0].DisplayName != "Bob Jones" {
		t.Fatalf("unexpected decoded: %+v", decoded)
	}
	if !SanitizeContactsListResp(decoded) {
		t.Fatal("expected SanitizeContactsListResp to pass")
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+1 (555) 123-4567": "+15551234567",
		"(415) 555-0132":    "4155550132",
		"555.123.4567":      "5551234567",
		"  +44 20 7946 0958 ": "+442079460958",
		"":                  "",
		"abc":               "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in); got != want {
			t.Fatalf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhonesEqual(t *testing.T) {
	equal := [][2]string{
		{"+1 (555) 123-4567", "5551234567"},
		{"+15551234567", "+1 555-123-4567"},
		{"(415) 555-0132", "4155550132"},
		{"5551234567", "5551234567"},
	}
	for _, p := range equal {
		if !PhonesEqual(p[0], p[1]) {
			t.Fatalf("expected equal: %q vs %q", p[0], p[1])
		}
	}
	unequal := [][2]string{
		{"5551234567", "5551234568"},
		{"123", "1234"},
		{"", "5551234567"},
		{"123456", "123457"},
	}
	for _, p := range unequal {
		if PhonesEqual(p[0], p[1]) {
			t.Fatalf("expected unequal: %q vs %q", p[0], p[1])
		}
	}
}

func TestContactDeleteReqRespSanitize(t *testing.T) {
	req := ContactDeleteReqPayload{
		Nonce:     "n-del-1",
		ReqID:     "req-1",
		ContactID: "c-123",
		LookupKey: "0r1-12345",
	}
	if !SanitizeContactDeleteReq(req) {
		t.Fatal("expected valid ContactDeleteReq to pass")
	}

	reqMissingID := req
	reqMissingID.ContactID = ""
	if SanitizeContactDeleteReq(reqMissingID) {
		t.Fatal("expected missing contact ID to fail")
	}

	reqBadLookup := req
	reqBadLookup.LookupKey = "bad\x00key"
	if SanitizeContactDeleteReq(reqBadLookup) {
		t.Fatal("expected control chars in lookup key to fail")
	}

	resp := ContactDeleteRespPayload{
		Nonce:     "n-del-1",
		ReqID:     "req-1",
		ContactID: "c-123",
		OK:        true,
	}
	if !SanitizeContactDeleteResp(resp) {
		t.Fatal("expected valid ContactDeleteResp to pass")
	}

	respMissingNonce := resp
	respMissingNonce.Nonce = ""
	if SanitizeContactDeleteResp(respMissingNonce) {
		t.Fatal("expected missing nonce in resp to fail")
	}
}
