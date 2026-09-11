// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestKeysetCursorV2RoundTrip(t *testing.T) {
	vectors := []struct {
		key string
		id  int64
	}{
		{"1700000010000", 42},   // SMS date key
		{"1700000010000", 0},    // first-page SMS cursor
		{"Zoë Müller-Smith", 7}, // contacts unicode name
		{"Alex", 1},
		{"Alex", 2},
		{"a:b.c d/e+f", 99}, // hostile key: separators, spaces, b64 chars
		{"", 0},             // empty key
		{"v2.tricky", 3},    // key that looks like a prefix
		{"123:45", 6},       // key shaped like a legacy SMS cursor
	}
	for _, v := range vectors {
		cur := EncodeKeysetCursor(v.key, v.id)
		if !strings.HasPrefix(cur, KeysetCursorPrefix+".") {
			t.Fatalf("cursor %q missing v2 prefix", cur)
		}
		key, id, ok := DecodeKeysetCursor(cur)
		if !ok || key != v.key || id != v.id {
			t.Fatalf("round-trip failed for (%q,%d): got (%q,%d,%v)", v.key, v.id, key, id, ok)
		}
	}
	if EncodeKeysetCursor("Alex", 1) == EncodeKeysetCursor("Alex", 2) {
		t.Fatal("same key with different ids must produce different cursors")
	}
	// Negative ids clamp to 0.
	if _, id, ok := DecodeKeysetCursor(EncodeKeysetCursor("k", -5)); !ok || id != 0 {
		t.Fatalf("negative id should clamp to 0, got (%d,%v)", id, ok)
	}
}

func TestKeysetCursorLegacyCompat(t *testing.T) {
	// Legacy SMS "date:id" / bare "date".
	key, id, ok := DecodeKeysetCursor("1700000010000:42")
	if !ok || key != "1700000010000" || id != 42 {
		t.Fatalf("legacy SMS pair failed: got (%q,%d,%v)", key, id, ok)
	}
	key, id, ok = DecodeKeysetCursor("1700000010000")
	if !ok || key != "1700000010000" || id != 0 {
		t.Fatalf("legacy bare date failed: got (%q,%d,%v)", key, id, ok)
	}
	// Legacy contacts "b64name.id" (pre-v2 encoding, no prefix).
	legacy := EncodeContactsCursorForTest("Zoë Müller-Smith", 7)
	key, id, ok = DecodeKeysetCursor(legacy)
	if !ok || key != "Zoë Müller-Smith" || id != 7 {
		t.Fatalf("legacy contacts pair failed: got (%q,%d,%v)", key, id, ok)
	}
	// Legacy bare display name (any string survives as key, id 0).
	key, id, ok = DecodeKeysetCursor("Alice Smith")
	if !ok || key != "Alice Smith" || id != 0 {
		t.Fatalf("legacy bare name failed: got (%q,%d,%v)", key, id, ok)
	}
	// Empty means first page.
	if key, id, ok := DecodeKeysetCursor(""); !ok || key != "" || id != 0 {
		t.Fatalf("empty cursor should decode to (\"\",0,true), got (%q,%d,%v)", key, id, ok)
	}
}

// EncodeContactsCursorForTest reproduces the pre-v2 contacts wire form so
// the compat test does not depend on the removed per-feature codec.
func EncodeContactsCursorForTest(displayName string, rowID int64) string {
	enc := base64.RawURLEncoding.EncodeToString([]byte(displayName))
	return enc + "." + strconv.FormatInt(rowID, 10)
}

func TestKeysetCursorRejects(t *testing.T) {
	// Oversize always fails.
	if _, _, ok := DecodeKeysetCursor(strings.Repeat("x", MaxSyncCursorLen+1)); ok {
		t.Fatal("oversize cursor should fail")
	}
	// Corrupt v2 fails closed (bad b64, bad id, dangling prefix).
	for _, bad := range []string{
		"v2.",
		"v2.nodot",
		"v2.!!!.5",
		"v2.eA.eHh4",
		"v2.eA.-1",
	} {
		if _, _, ok := DecodeKeysetCursor(bad); ok {
			t.Fatalf("corrupt v2 cursor %q should fail", bad)
		}
	}
	// A legacy string that merely starts with "v2." fails closed instead of
	// paging wrong (e.g. a contact literally named "v2.foo").
	if _, _, ok := DecodeKeysetCursor("v2.foo"); ok {
		t.Fatal("legacy 'v2.foo' should fail closed, not page wrong")
	}
	// Bare "v2" (no dot) is a legacy bare string, tolerated like any name.
	if key, _, ok := DecodeKeysetCursor("v2"); !ok || key != "v2" {
		t.Fatalf("bare 'v2' should decode leniently, got (%q,%v)", key, ok)
	}
	// Padded input trims to a valid cursor.
	if key, id, ok := DecodeKeysetCursor("  "+EncodeKeysetCursor("k", 3)+"  "); !ok || key != "k" || id != 3 {
		t.Fatalf("padded v2 cursor should decode, got (%q,%d,%v)", key, id, ok)
	}
}

func TestValidateKeysetCursor(t *testing.T) {
	for _, okCur := range []string{
		"",
		"1700000010000:42", // legacy tolerated
		"Alice Smith",      // legacy tolerated
		EncodeKeysetCursor("1700000010000", 42),
		EncodeKeysetCursor("Zoë", 1),
	} {
		if err := ValidateKeysetCursor(okCur); err != nil {
			t.Fatalf("cursor %q should validate, got %v", okCur, err)
		}
	}
	for _, badCur := range []string{
		strings.Repeat("x", MaxSyncCursorLen+1),
		"v2.!!!.5",
		"v2.foo",
	} {
		err := ValidateKeysetCursor(badCur)
		if !errors.Is(err, ErrCursorInvalid) {
			t.Fatalf("cursor %q should fail with ErrCursorInvalid, got %v", badCur, err)
		}
	}
}

func TestDedupCache(t *testing.T) {
	d := NewDedupCache(3)
	if d.Seen("a") {
		t.Fatal("fresh cache should miss")
	}
	if !d.Mark("a") {
		t.Fatal("first mark should be new")
	}
	if d.Mark("a") {
		t.Fatal("second mark should be duplicate")
	}
	if !d.Seen("a") {
		t.Fatal("marked key should hit")
	}
	d.Mark("b")
	d.Mark("c")
	d.Mark("d") // evicts "a"
	if d.Seen("a") {
		t.Fatal("evicted key should miss")
	}
	if !d.Seen("d") {
		t.Fatal("newest key should hit")
	}
	if d.Mark("") {
		t.Fatal("empty key must never mark")
	}
	if d.Seen("") {
		t.Fatal("empty key must never hit")
	}
}

func TestSendReqRejectsBadSubID(t *testing.T) {
	req := SMSSendReqPayload{
		Nonce:     "n",
		ReqID:     "r",
		Recipient: "+15551234567",
		Body:      "hi",
		ClientID:  "c",
		SubID:     "abc",
	}
	if SanitizeSMSSendReq(req) {
		t.Fatal("non-numeric sub_id should fail validation")
	}
	req.SubID = "1"
	if !SanitizeSMSSendReq(req) {
		t.Fatal("numeric sub_id should pass validation")
	}
}
