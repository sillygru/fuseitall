// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

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

	// Caps limits for contacts.
	MaxContactIDLen        = 64
	MaxContactNameLen      = 128
	MaxPhoneNumberLen      = 32
	MaxEmailAddressLen     = 128
	MaxContactLabelLen     = 32
	MaxContactsPerResp     = 100
	DefaultContactsLimit   = 50
	MaxContactAvatarB64Len = 65536 // 64 KB cap for thumbnail avatar
	MaxContactsQueryLen    = 128
)

// ContactPhone is one phone number associated with a contact.
type ContactPhone struct {
	Number    string `json:"number"`
	Type      string `json:"type,omitempty"`
	Label     string `json:"label,omitempty"`
	IsPrimary bool   `json:"is_primary,omitempty"`
}

// ContactEmail is one email address associated with a contact.
type ContactEmail struct {
	Address string `json:"address"`
	Type    string `json:"type,omitempty"`
	Label   string `json:"label,omitempty"`
}

// ContactEntry is one contact in the directory.
type ContactEntry struct {
	ContactID   string         `json:"contact_id"`
	DisplayName string         `json:"display_name"`
	Phones      []ContactPhone `json:"phones,omitempty"`
	Emails      []ContactEmail `json:"emails,omitempty"`
	AvatarB64   string         `json:"avatar_b64,omitempty"`
	Starred     bool           `json:"starred,omitempty"`
}

// ContactsListReqPayload is the payload of a TypeContactsListReq envelope.
type ContactsListReqPayload struct {
	Nonce  string `json:"nonce"`
	ReqID  string `json:"req_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Query  string `json:"query,omitempty"`
}

// ContactsListRespPayload is the payload of a TypeContactsListResp envelope.
type ContactsListRespPayload struct {
	Nonce      string         `json:"nonce"`
	ReqID      string         `json:"req_id"`
	Entries    []ContactEntry `json:"entries,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`
	TotalCount int            `json:"total_count,omitempty"`
	Error      string         `json:"error,omitempty"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Permission string         `json:"permission,omitempty"`
}

// ContactAvatarReqPayload is the payload of a TypeContactAvatarReq envelope.
type ContactAvatarReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	ContactID string `json:"contact_id"`
}

// ContactAvatarRespPayload is the payload of a TypeContactAvatarResp envelope.
type ContactAvatarRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	ContactID  string `json:"contact_id"`
	Mime       string `json:"mime,omitempty"`
	DataB64    string `json:"data_b64,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// ContactsChangedPayload is the payload of a TypeContactsChanged envelope.
type ContactsChangedPayload struct {
	Nonce     string `json:"nonce"`
	ChangedAt int64  `json:"changed_at,omitempty"`
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
