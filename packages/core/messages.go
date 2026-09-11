// SPDX-License-Identifier: AGPL-3.0-only

package core

import (
	"strings"
	"unicode"
)

const (
	// CapabilityMessages advertises SMS synchronization and sending.
	CapabilityMessages = "messages"

	// TypeSMSThreadsReq requests conversation threads (Mac -> phone).
	TypeSMSThreadsReq = "sms-threads-req"
	// TypeSMSThreadsResp returns conversation threads (phone -> Mac).
	TypeSMSThreadsResp = "sms-threads-resp"
	// TypeSMSMessagesReq requests messages for a thread (Mac -> phone).
	TypeSMSMessagesReq = "sms-messages-req"
	// TypeSMSMessagesResp returns messages for a thread (phone -> Mac).
	TypeSMSMessagesResp = "sms-messages-resp"
	// TypeSMSSendReq sends an SMS message (Mac -> phone).
	TypeSMSSendReq = "sms-send-req"
	// TypeSMSSendResp returns the send result (phone -> Mac).
	TypeSMSSendResp = "sms-send-resp"
	// TypeSMSPush pushes a newly received SMS message to the Mac (phone -> Mac).
	TypeSMSPush = "sms-push"
	// TypeSMSChanged notifies that the SMS database changed (phone -> Mac).
	TypeSMSChanged = "sms-changed"
	// TypeSMSMarkReadReq requests marking a message or thread as read (Mac -> phone).
	TypeSMSMarkReadReq = "sms-mark-read-req"
	// TypeSMSMarkReadResp returns the result of marking as read (phone -> Mac).
	TypeSMSMarkReadResp = "sms-mark-read-resp"

	// SMS message types (matching Android Telephony.Sms.MESSAGE_TYPE_*).
	SMSMsgTypeInbox   = 1
	SMSMsgTypeSent    = 2
	SMSMsgTypeDraft   = 3
	SMSMsgTypeOutbox  = 4
	SMSMsgTypeFailed  = 5
	SMSMsgTypeQueued  = 6

	// Caps limits for messages.
	MaxSMSBodyLen         = 5000
	MaxSMSAddressLen      = 64
	MaxSMSThreadsPerResp  = 100
	DefaultSMSThreadsLimit = 50
	MaxSMSMessagesPerResp = 100
	DefaultSMSMessagesLimit = 50
	MaxSMSClientIDLen     = 64
)

// SMSThread represents a conversation thread.
type SMSThread struct {
	ThreadID     int64  `json:"thread_id"`
	Address      string `json:"address"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactID    string `json:"contact_id,omitempty"`
	PhotoVersion string `json:"photo_version,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
	Date         int64  `json:"date"`
	MessageCount int    `json:"message_count"`
	UnreadCount  int    `json:"unread_count,omitempty"`
	Read         bool   `json:"read"`
}

// SMSMessage represents an individual SMS message.
type SMSMessage struct {
	ID       int64  `json:"id"`
	ThreadID int64  `json:"thread_id"`
	Address  string `json:"address"`
	Body     string `json:"body"`
	Date     int64  `json:"date"`
	Type     int    `json:"type"`
	Read     bool   `json:"read"`
	Status   int    `json:"status,omitempty"`
	// ContactName/ContactID/PhotoVersion are additive per-message identity
	// (mirrors SMSThread + SMSPush top-level). Empty = unknown/no permission.
	// Old peers omit them; decoders fail-soft to thread-level/Mac join.
	ContactName  string `json:"contact_name,omitempty"`
	ContactID    string `json:"contact_id,omitempty"`
	PhotoVersion string `json:"photo_version,omitempty"`
}

// SMSThreadsReqPayload is the payload of a TypeSMSThreadsReq envelope.
type SMSThreadsReqPayload struct {
	Nonce  string `json:"nonce"`
	ReqID  string `json:"req_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	// CursorGen echoes the caller's known store generation (0 = unknown).
	CursorGen int64 `json:"cursor_gen,omitempty"`
}

// SMSThreadsRespPayload is the payload of a TypeSMSThreadsResp envelope.
type SMSThreadsRespPayload struct {
	Nonce      string      `json:"nonce"`
	ReqID      string      `json:"req_id"`
	Threads    []SMSThread `json:"threads,omitempty"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Error      string      `json:"error,omitempty"`
	ErrorCode  string      `json:"error_code,omitempty"`
	Permission string      `json:"permission,omitempty"`
}

// SMSMessagesReqPayload is the payload of a TypeSMSMessagesReq envelope.
type SMSMessagesReqPayload struct {
	Nonce    string `json:"nonce"`
	ReqID    string `json:"req_id"`
	ThreadID int64  `json:"thread_id"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	// CursorGen echoes the caller's known store generation (0 = unknown).
	CursorGen int64 `json:"cursor_gen,omitempty"`
}

// SMSMessagesRespPayload is the payload of a TypeSMSMessagesResp envelope.
type SMSMessagesRespPayload struct {
	Nonce      string       `json:"nonce"`
	ReqID      string       `json:"req_id"`
	ThreadID   int64        `json:"thread_id"`
	Messages   []SMSMessage `json:"messages,omitempty"`
	NextCursor string       `json:"next_cursor,omitempty"`
	Error      string       `json:"error,omitempty"`
	ErrorCode  string       `json:"error_code,omitempty"`
	Permission string       `json:"permission,omitempty"`
}

// SMSSendReqPayload is the payload of a TypeSMSSendReq envelope.
type SMSSendReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	Recipient string `json:"recipient"`
	Body      string `json:"body"`
	ClientID  string `json:"client_id"`
	// SubID selects the Android subscription for dual-SIM phones ("" =
	// default). Echoed in the response; decoders ignore unknown fields.
	SubID string `json:"sub_id,omitempty"`
}

// SMSSendRespPayload is the payload of a TypeSMSSendResp envelope.
type SMSSendRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	ClientID   string `json:"client_id"`
	OK         bool   `json:"ok"`
	MessageID  int64  `json:"message_id,omitempty"`
	ThreadID   int64  `json:"thread_id,omitempty"`
	SubID      string `json:"sub_id,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// SMSPushPayload is the payload of a TypeSMSPush envelope.
type SMSPushPayload struct {
	Nonce       string     `json:"nonce"`
	Message     SMSMessage `json:"message"`
	ContactName string     `json:"contact_name,omitempty"`
	ContactID   string     `json:"contact_id,omitempty"`
	PhotoVersion string    `json:"photo_version,omitempty"`
	// ClientID is the origin-side dedup key for at-least-once pushes.
	// Seq is the per-stream sequence for gap detection (0 = unset/legacy).
	ClientID string `json:"client_id,omitempty"`
	Seq      int64  `json:"seq,omitempty"`
}

// SMSChangedPayload is the payload of a TypeSMSChanged envelope.
type SMSChangedPayload struct {
	Nonce     string `json:"nonce"`
	ChangedAt int64  `json:"changed_at,omitempty"`
	// CursorGen bumps when the phone's SMS store generation resets
	// (restore/wiped DB). Receivers must drop caches and full-resync.
	CursorGen int64 `json:"cursor_gen,omitempty"`
}

// SMSMarkReadReqPayload is the payload of a TypeSMSMarkReadReq envelope.
type SMSMarkReadReqPayload struct {
	Nonce     string `json:"nonce"`
	ReqID     string `json:"req_id"`
	ThreadID  int64  `json:"thread_id"`
	MessageID int64  `json:"message_id,omitempty"`
	Address   string `json:"address,omitempty"`
}

// SMSMarkReadRespPayload is the payload of a TypeSMSMarkReadResp envelope.
type SMSMarkReadRespPayload struct {
	Nonce      string `json:"nonce"`
	ReqID      string `json:"req_id"`
	ThreadID   int64  `json:"thread_id,omitempty"`
	MessageID  int64  `json:"message_id,omitempty"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// SanitizeSubID trims and validates an Android subscription id for
// dual-SIM send routing. Empty means "default subscription" and is valid.
// Pure.
func SanitizeSubID(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", true
	}
	if len(trimmed) > MaxSubIDLen {
		return "", false
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeSMSAddress trims and validates an SMS recipient or sender address.
func SanitizeSMSAddress(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxSMSAddressLen {
		return "", false
	}
	for _, r := range trimmed {
		if !unicode.IsDigit(r) && !unicode.IsLetter(r) && r != '+' && r != '-' && r != '(' && r != ')' && r != '.' && r != ' ' && r != '@' {
			return "", false
		}
	}
	return trimmed, true
}

// SanitizeSMSBody trims and validates message body (1..MaxSMSBodyLen runes).
func SanitizeSMSBody(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || len([]rune(trimmed)) > MaxSMSBodyLen {
		return "", false
	}
	return trimmed, true
}

// SanitizeSMSSendReq validates send request payload.
func SanitizeSMSSendReq(p SMSSendReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if _, ok := SanitizeSMSAddress(p.Recipient); !ok {
		return false
	}
	if _, ok := SanitizeSMSBody(p.Body); !ok {
		return false
	}
	clientID := strings.TrimSpace(p.ClientID)
	if clientID == "" || len(clientID) > MaxSMSClientIDLen {
		return false
	}
	if _, ok := SanitizeSubID(p.SubID); !ok {
		return false
	}
	return true
}

// SanitizeSMSThreadsReq validates threads request payload.
func SanitizeSMSThreadsReq(p SMSThreadsReqPayload) bool {
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
	if p.Limit < 0 || p.Limit > MaxSMSThreadsPerResp {
		return false
	}
	return true
}

// SanitizeSMSMessagesReq validates messages request payload.
func SanitizeSMSMessagesReq(p SMSMessagesReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if p.ThreadID < 0 {
		return false
	}
	if len(p.Cursor) > 256 {
		return false
	}
	if p.Limit < 0 || p.Limit > MaxSMSMessagesPerResp {
		return false
	}
	return true
}

// SanitizeSMSMarkReadReq validates mark-read request payload.
func SanitizeSMSMarkReadReq(p SMSMarkReadReqPayload) bool {
	if p.Nonce == "" {
		return false
	}
	reqID := strings.TrimSpace(p.ReqID)
	if reqID == "" || len(reqID) > 64 {
		return false
	}
	if p.ThreadID < 0 || p.MessageID < 0 {
		return false
	}
	if p.ThreadID == 0 && p.MessageID == 0 && strings.TrimSpace(p.Address) == "" {
		return false
	}
	if p.Address != "" {
		if _, ok := SanitizeSMSAddress(p.Address); !ok {
			return false
		}
	}
	return true
}

// SanitizeSMSMarkReadResp validates mark-read response payload.
func SanitizeSMSMarkReadResp(p SMSMarkReadRespPayload) bool {
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

// SanitizeSMSMessage sanitizes additive per-message contact identity in
// place (fail-soft: oversize/unknown becomes ""). Pure.
func SanitizeSMSMessage(m *SMSMessage) {
	if m == nil {
		return
	}
	m.ContactName = SanitizeContactName(m.ContactName)
	if id, ok := SanitizeContactID(m.ContactID); ok {
		m.ContactID = id
	} else if strings.TrimSpace(m.ContactID) != "" {
		// SanitizeContactID rejects empty; keep empty as unknown.
		m.ContactID = ""
	} else {
		m.ContactID = ""
	}
	m.PhotoVersion = SanitizePhotoVersion(m.PhotoVersion)
}
