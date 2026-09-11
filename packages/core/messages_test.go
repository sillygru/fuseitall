// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessagesSanitize(t *testing.T) {
	// Address
	if addr, ok := SanitizeSMSAddress("+1 (555) 123-4567"); !ok || addr != "+1 (555) 123-4567" {
		t.Fatalf("expected valid address, got %q, ok=%v", addr, ok)
	}
	if _, ok := SanitizeSMSAddress("rm -rf /"); ok {
		t.Fatal("expected injection attack address to be rejected")
	}

	// Body
	if body, ok := SanitizeSMSBody("Hello world!"); !ok || body != "Hello world!" {
		t.Fatalf("expected valid body, got %q, ok=%v", body, ok)
	}
	if _, ok := SanitizeSMSBody(""); ok {
		t.Fatal("expected empty body to be rejected")
	}
	if _, ok := SanitizeSMSBody(strings.Repeat("a", MaxSMSBodyLen+1)); ok {
		t.Fatal("expected oversize body to be rejected")
	}

	// Send req
	req := SMSSendReqPayload{
		Nonce:     "nonce-123",
		ReqID:     "req-456",
		Recipient: "+1234567890",
		Body:      "Test message",
		ClientID:  "client-abc",
	}
	if !SanitizeSMSSendReq(req) {
		t.Fatal("expected valid send req to pass")
	}
	req.Recipient = ""
	if SanitizeSMSSendReq(req) {
		t.Fatal("expected missing recipient to fail")
	}
}

func TestMessagesSerialization(t *testing.T) {
	thread := SMSThread{
		ThreadID:     42,
		Address:      "+1987654321",
		ContactName:  "Charlie",
		Snippet:      "Hey there!",
		Date:         1710000000000,
		MessageCount: 15,
		UnreadCount:  2,
		Read:         false,
	}

	resp := SMSThreadsRespPayload{
		Nonce:      "n-threads",
		ReqID:      "r-threads",
		Threads:    []SMSThread{thread},
		NextCursor: "",
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded SMSThreadsRespPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Nonce != "n-threads" || len(decoded.Threads) != 1 || decoded.Threads[0].ContactName != "Charlie" {
		t.Fatalf("unexpected decoded: %+v", decoded)
	}
}

func TestSMSMessageContactIdentityRoundTrip(t *testing.T) {
	m := SMSMessage{
		ID: 7, ThreadID: 42, Address: "+15551234567", Body: "hi",
		Date: 1710000000000, Type: SMSMsgTypeInbox, Read: false,
		ContactName: "Alice", ContactID: "9", PhotoVersion: "5",
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded SMSMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.ContactName != "Alice" || decoded.ContactID != "9" || decoded.PhotoVersion != "5" {
		t.Fatalf("per-message identity lost: %+v", decoded)
	}
	// Old peers omit the additive fields: fail-soft to empty.
	var legacy SMSMessage
	if err := json.Unmarshal([]byte(`{"id":1,"thread_id":2,"address":"555","body":"x","date":1,"type":1,"read":true}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal failed: %v", err)
	}
	if legacy.ContactName != "" || legacy.ContactID != "" || legacy.PhotoVersion != "" {
		t.Fatalf("legacy should be empty: %+v", legacy)
	}
}

func TestSanitizeSMSMessage(t *testing.T) {
	m := SMSMessage{ContactName: "  Alice  ", ContactID: "  9  ", PhotoVersion: " v1 "}
	SanitizeSMSMessage(&m)
	if m.ContactName != "Alice" || m.ContactID != "9" || m.PhotoVersion != "v1" {
		t.Fatalf("sanitize failed: %+v", m)
	}
	m = SMSMessage{ContactID: "bad\x00id", PhotoVersion: strings.Repeat("x", 200)}
	SanitizeSMSMessage(&m)
	if m.ContactID != "" {
		t.Fatalf("control-char id accepted: %+v", m)
	}
	if len([]rune(m.PhotoVersion)) > 128 {
		t.Fatalf("photo version not capped: %+v", m)
	}
}

func TestSMSMarkReadSanitize(t *testing.T) {
	req := SMSMarkReadReqPayload{
		Nonce:    "n-read-1",
		ReqID:    "req-r1",
		ThreadID: 42,
		Address:  "+15551234567",
	}
	if !SanitizeSMSMarkReadReq(req) {
		t.Fatal("expected valid SMSMarkReadReq to pass")
	}

	reqMissingNonce := req
	reqMissingNonce.Nonce = ""
	if SanitizeSMSMarkReadReq(reqMissingNonce) {
		t.Fatal("expected missing nonce to fail")
	}

	reqNegativeThread := req
	reqNegativeThread.ThreadID = -1
	if SanitizeSMSMarkReadReq(reqNegativeThread) {
		t.Fatal("expected negative thread ID to fail")
	}

	reqEmptyAll := SMSMarkReadReqPayload{
		Nonce: "n-read-2",
		ReqID: "req-r2",
	}
	if SanitizeSMSMarkReadReq(reqEmptyAll) {
		t.Fatal("expected empty thread, message, and address to fail")
	}

	resp := SMSMarkReadRespPayload{
		Nonce:    "n-read-1",
		ReqID:    "req-r1",
		ThreadID: 42,
		OK:       true,
	}
	if !SanitizeSMSMarkReadResp(resp) {
		t.Fatal("expected valid SMSMarkReadResp to pass")
	}

	respMissingNonce := resp
	respMissingNonce.Nonce = ""
	if SanitizeSMSMarkReadResp(respMissingNonce) {
		t.Fatal("expected missing nonce in resp to fail")
	}
}
