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
