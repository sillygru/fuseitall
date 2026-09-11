// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"strings"
	"unicode"
)

const (
	// CapabilityContacts advertises contacts directory synchronization.
	CapabilityContacts = "contacts"

	// TypeContactsListReq requests a page of contacts (Mac -> phone).
	TypeContactsListReq = "contacts-list-req"
	// TypeContactsListResp returns a page of contacts (phone -> Mac).
	TypeContactsListResp = "contacts-list-resp"
	// TypeContactAvatarReq requests a contact's avatar photo (Mac -> phone).
	TypeContactAvatarReq = "contact-avatar-req"
	// TypeContactAvatarResp returns a contact's avatar photo (phone -> Mac).
	TypeContactAvatarResp = "contact-avatar-resp"
	// TypeContactsChanged pushes a notification that phone contacts changed (phone -> Mac).
	TypeContactsChanged = "contacts-changed"
	// TypeContactDeleteReq requests deleting a contact (Mac -> phone).
	TypeContactDeleteReq = "contact-delete-req"
	// TypeContactDeleteResp returns the result of contact deletion (phone -> Mac).
	TypeContactDeleteResp = "contact-delete-resp"

	// Caps limits for contacts.
	MaxContactIDLen        = 64
	MaxContactLookupKeyLen = 256
	MaxContactNameLen      = 128
	MaxPhoneNumberLen      = 32
	MaxEmailAddressLen     = 128
	MaxContactLabelLen     = 32
	MaxContactsPerResp     = 100
	DefaultContactsLimit   = 50
	MaxContactAvatarB64Len = 65536 // 64 KB cap for thumbnail avatar
	MaxContactsQueryLen    = 128
	MaxContactNicknameLen  = 128
	MaxContactNoteLen      = 2048
	MaxContactWebsiteLen   = 512
	MaxContactOrgLen       = 128
	MaxContactPostalLen    = 512
	MaxContactPhotoURILen  = 512
)

// ContactPhone is one phone number associated with a contact.
type ContactPhone struct {
	Number    string `json:"number"`
	Type      string `json:"type,omitempty"`
	Label     string `json:"label,omitempty"`
	IsPrimary bool   `json:"is_primary,omitempty"`
	// Normalized is the E.164-ish canonical form (DATA4/NORMALIZED_NUMBER)
	// when the provider supplies it. Additive; "" = unknown. Used for
	// cross-format matching without extra lookups.
	Normalized string `json:"normalized_number,omitempty"`
}

// ContactEmail is one email address associated with a contact.
type ContactEmail struct {
	Address string `json:"address"`
	Type    string `json:"type,omitempty"`
	Label   string `json:"label,omitempty"`
}

// ContactOrganization is the employer/role block for a contact.
type ContactOrganization struct {
	Company    string `json:"company,omitempty"`
	Title      string `json:"title,omitempty"`
	Department string `json:"department,omitempty"`
}

// ContactPostal is a formatted postal address for a contact.
type ContactPostal struct {
	Formatted string `json:"formatted,omitempty"`
	Type      string `json:"type,omitempty"`
}

// ContactEntry is one contact in the directory.
type ContactEntry struct {
	ContactID   string         `json:"contact_id"`
	DisplayName string         `json:"display_name"`
	Phones      []ContactPhone `json:"phones,omitempty"`
	Emails      []ContactEmail `json:"emails,omitempty"`
	AvatarB64   string         `json:"avatar_b64,omitempty"`
	Starred     bool           `json:"starred,omitempty"`
	// LookupKey is the stable Android LOOKUP_KEY surviving aggregation
	// split/merge ("" = legacy peer). LastUpdatedMs is the aggregate
	// CONTACT_LAST_UPDATED_TIMESTAMP watermark for delta sync.
	LookupKey     string `json:"lookup_key,omitempty"`
	LastUpdatedMs int64  `json:"last_updated_ms,omitempty"`
	// PhotoVersion versions inline avatars (photo_id:file_id:updated).
	// PhotoURI is the optional thumbnail/display photo URI string.
	PhotoVersion string `json:"photo_version,omitempty"`
	PhotoURI     string `json:"photo_uri,omitempty"`
	// Extended detail fields (all Android-provided, 0/"" = unknown).
	// Additive: older peers simply omit them.
	BirthdayMs    int64                `json:"birthday_ms,omitempty"`
	AnniversaryMs int64                `json:"anniversary_ms,omitempty"`
	Nickname      string               `json:"nickname,omitempty"`
	Note          string               `json:"note,omitempty"`
	Website       string               `json:"website,omitempty"`
	Organization  *ContactOrganization `json:"organization,omitempty"`
	Postal        *ContactPostal       `json:"postal,omitempty"`
}

// ContactsListReqPayload is the payload of a TypeContactsListReq envelope.
type ContactsListReqPayload struct {
	Nonce  string `json:"nonce"`
	ReqID  string `json:"req_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Query  string `json:"query,omitempty"`
	// CursorGen echoes the caller's known directory generation.
	CursorGen int64 `json:"cursor_gen,omitempty"`
}

// ContactsListRespPayload is the payload of a TypeContactsListResp envelope.
type ContactsListRespPayload struct {
	Nonce      string         `json:"nonce"`
	ReqID      string         `json:"req_id"`
	Entries    []ContactEntry `json:"entries,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	TotalCount int            `json:"total_count,omitempty"`
	CursorGen  int64          `json:"cursor_gen,omitempty"`
	Error      string         `json:"error,omitempty"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Permission string         `json:"permission,omitempty"`
}

// ContactAvatarReqPayload is the payload of a TypeContactAvatarReq envelope.
type ContactAvatarReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	ContactID string `json:"contact_id"`
	// HighRes requests the full display photo (detail header) instead of
	// the thumbnail (lists/messages). Unknown-field-safe for old phones.
	HighRes bool `json:"high_res,omitempty"`
}

// ContactAvatarRespPayload is the payload of a TypeContactAvatarResp envelope.
type ContactAvatarRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	ContactID  string `json:"contact_id"`
	Mime       string `json:"mime,omitempty"`
	DataB64    string `json:"data_b64,omitempty"`
	// PhotoVersion versions the avatar bytes (photo id / file id / update
	// timestamp joined by the phone). Receivers key LRU eviction on it.
	PhotoVersion string `json:"photo_version,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	Permission   string `json:"permission,omitempty"`
}

// ContactsChangedPayload is the payload of a TypeContactsChanged envelope.
type ContactsChangedPayload struct {
	Nonce     string `json:"nonce"`
	ChangedAt int64  `json:"changed_at,omitempty"`
	// CursorGen bumps when the directory generation resets. Receivers must
	// drop caches and full-resync on mismatch.
	CursorGen int64 `json:"cursor_gen,omitempty"`
}

// ContactDeleteReqPayload is the payload of a TypeContactDeleteReq envelope.
type ContactDeleteReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	ContactID string `json:"contact_id"`
	LookupKey string `json:"lookup_key,omitempty"`
}

// ContactDeleteRespPayload is the payload of a TypeContactDeleteResp envelope.
type ContactDeleteRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	ContactID  string `json:"contact_id"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// SanitizeContactID trims and verifies contact_id (1..MaxContactIDLen runes, no control chars).
func SanitizeContactID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxContactIDLen {
		return "", false
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeContactName trims and caps display name to MaxContactNameLen runes.
func SanitizeContactName(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) > MaxContactNameLen {
		return strings.TrimSpace(string(runes[:MaxContactNameLen]))
	}
	return trimmed
}

// SanitizePhoneNumber trims, caps, and verifies phone number.
func SanitizePhoneNumber(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxPhoneNumberLen {
		return "", false
	}
	// Permitted: digits, +, -, (, ), ., space
	for _, r := range trimmed {
		if !unicode.IsDigit(r) && r != '+' && r != '-' && r != '(' && r != ')' && r != '.' && r != ' ' {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeEmailAddress trims and caps email address.
func SanitizeEmailAddress(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxEmailAddressLen {
		return "", false
	}
	if !strings.Contains(trimmed, "@") {
		return "", false
	}
	return trimmed, true
}

// SanitizeContactAvatarB64 checks length cap and validates base64 shape.
func SanitizeContactAvatarB64(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxContactAvatarB64Len {
		return ""
	}
	for _, r := range trimmed {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			continue
		}
		return ""
	}
	return trimmed
}

// SanitizeContactsListReq validates list request payload.
func SanitizeContactsListReq(p ContactsListReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if len(p.Cursor) > 256 {
		return false
	}
	if p.Limit < 0 || p.Limit > MaxContactsPerResp {
		return false
	}
	if len(p.Query) > MaxContactsQueryLen {
		return false
	}
	return true
}

// SanitizeContactsListResp validates list response payload.
func SanitizeContactsListResp(p ContactsListRespPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if len(p.Entries) > MaxContactsPerResp {
		return false
	}
	if len(p.NextCursor) > 256 {
		return false
	}
	if len(p.Error) > 512 {
		return false
	}
	return true
}

// SanitizeContactNickname trims and caps a nickname.
func SanitizeContactNickname(s string) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) > MaxContactNicknameLen {
		return strings.TrimSpace(string(runes[:MaxContactNicknameLen]))
	}
	return trimmed
}

// SanitizeContactNote trims and caps a contact note.
func SanitizeContactNote(s string) string {
	trimmed := strings.TrimSpace(s)
	runes := []rune(trimmed)
	if len(runes) > MaxContactNoteLen {
		return strings.TrimSpace(string(runes[:MaxContactNoteLen]))
	}
	return trimmed
}

// SanitizeContactWebsite trims and caps a website URL.
func SanitizeContactWebsite(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) > MaxContactWebsiteLen {
		return ""
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return trimmed
}

// SanitizeContactPhotoURI trims and caps a photo URI string.
func SanitizeContactPhotoURI(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len(trimmed) > MaxContactPhotoURILen {
		return ""
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return trimmed
}

// SanitizeContactAvatarReq validates avatar request payload.
func SanitizeContactAvatarReq(p ContactAvatarReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	_, ok := SanitizeContactID(p.ContactID)
	return ok
}

// SanitizeContactLookupKey trims and verifies lookup_key (0..MaxContactLookupKeyLen runes, no control chars).
func SanitizeContactLookupKey(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if len([]rune(trimmed)) > MaxContactLookupKeyLen {
		return "", false
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeContactDeleteReq validates delete request payload.
func SanitizeContactDeleteReq(p ContactDeleteReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if _, ok := SanitizeContactID(p.ContactID); !ok {
		return false
	}
	if _, ok := SanitizeContactLookupKey(p.LookupKey); !ok {
		return false
	}
	return true
}

// SanitizeContactDeleteResp validates delete response payload.
func SanitizeContactDeleteResp(p ContactDeleteRespPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if len(p.Error) > 512 {
		return false
	}
	return true
}

// NormalizePhone returns the canonical comparison form of a phone number:
// trimmed, leading "+" preserved once, all other non-digits stripped.
// Examples: "+1 (555) 123-4567" -> "+15551234567", "(415) 555-0132" ->
// "4155550132". Empty or no-digit input returns "". Pure.
func NormalizePhone(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	hasPlus := strings.HasPrefix(trimmed, "+")
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	if hasPlus {
		return "+" + digits
	}
	return digits
}

// normalizeDigits strips a single leading "+" for suffix comparison. Pure.
func normalizeDigits(s string) string {
	return strings.TrimPrefix(NormalizePhone(s), "+")
}

// PhonesEqual reports whether two raw phone strings likely denote the same
// endpoint. Exact normalized match wins; otherwise a suffix match of >=7
// digits covers national vs E.164 (+1 prefix, spaces/dashes/parens) without
// false-positiving on short codes. Pure.
func PhonesEqual(a, b string) bool {
	na := NormalizePhone(a)
	nb := NormalizePhone(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	da := normalizeDigits(na)
	db := normalizeDigits(nb)
	if da == db {
		return true
	}
	// Country-code tolerance: strip a single leading "1" for NANP-style
	// 11-digit vs 10-digit pairs before suffix comparison.
	stripOne := func(d string) string {
		if len(d) == 11 && strings.HasPrefix(d, "1") {
			return d[1:]
		}
		return d
	}
	da, db = stripOne(da), stripOne(db)
	if da == db {
		return true
	}
	short, long := da, db
	if len(da) > len(db) {
		short, long = db, da
	}
	if len(short) < 7 {
		return false
	}
	return strings.HasSuffix(long, short)
}

// SanitizePhotoVersion trims and caps a photo_version ETag.
func SanitizePhotoVersion(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) > 128 {
		return strings.TrimSpace(string(runes[:128]))
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return trimmed
}
