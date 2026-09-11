// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"fuseitall/core"
)

// SMSThreadsResult is the Wails-bound result for SMS conversations.
type SMSThreadsResult struct {
	Threads    []core.SMSThread `json:"threads"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Error      string           `json:"error,omitempty"`
	ErrorCode  string           `json:"error_code,omitempty"`
	Permission string           `json:"permission,omitempty"`
}

// SMSMessagesResult is the Wails-bound result for messages inside a thread.
type SMSMessagesResult struct {
	ThreadID   int64             `json:"thread_id"`
	Messages   []core.SMSMessage `json:"messages"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Error      string            `json:"error,omitempty"`
	ErrorCode  string            `json:"error_code,omitempty"`
	Permission string            `json:"permission,omitempty"`
}

// SMSSendResult is the Wails-bound result for an SMS send operation.
type SMSSendResult struct {
	Ok         bool   `json:"ok"`
	ClientID   string `json:"client_id"`
	MessageID  int64  `json:"message_id,omitempty"`
	ThreadID   int64  `json:"thread_id,omitempty"`
	SubID      string `json:"sub_id,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
}

// smsPushDedupKey returns the stable dedup key for an inbound sms-push:
// client_id when present, else provider id, else address|date|body-length
// fallback for legacy pushes. Pure.
func smsPushDedupKey(msg core.SMSMessage, clientID string) string {
	if clientID != "" {
		return "c:" + clientID
	}
	if msg.ID != 0 {
		return "id:" + strconv.FormatInt(msg.ID, 10)
	}
	return "fb:" + msg.Address + "|" + strconv.FormatInt(msg.Date, 10) + "|" + strconv.Itoa(len(msg.Body))
}

// mergeSMSThreads appends src rows that are not already present (by
// ThreadID), keeping first-seen order. Overlapping pages (concurrent loads,
// push-while-paging, cursor-equal dates) must never duplicate cache rows:
// Svelte keyed each blocks throw on duplicate keys.
func mergeSMSThreads(dst []core.SMSThread, src []core.SMSThread) []core.SMSThread {
	if len(dst) == 0 {
		return append([]core.SMSThread{}, src...)
	}
	seen := make(map[int64]struct{}, len(dst)+len(src))
	for _, t := range dst {
		seen[t.ThreadID] = struct{}{}
	}
	for _, t := range src {
		if _, ok := seen[t.ThreadID]; ok {
			continue
		}
		seen[t.ThreadID] = struct{}{}
		dst = append(dst, t)
	}
	return dst
}

// mergeSMSMessages appends src rows with unseen IDs, keeping chronological order. Caller must hold messagesMu.
func mergeSMSMessages(dst []core.SMSMessage, src []core.SMSMessage) []core.SMSMessage {
	if len(dst) == 0 {
		res := append([]core.SMSMessage{}, src...)
		sort.Slice(res, func(i, j int) bool {
			if res[i].Date != res[j].Date {
				return res[i].Date < res[j].Date
			}
			return res[i].ID < res[j].ID
		})
		return res
	}
	seen := make(map[int64]struct{}, len(dst)+len(src))
	for _, m := range dst {
		seen[m.ID] = struct{}{}
	}
	for _, m := range src {
		if m.ID != 0 {
			if _, ok := seen[m.ID]; ok {
				continue
			}
			seen[m.ID] = struct{}{}
		}
		dst = append(dst, m)
	}
	sort.Slice(dst, func(i, j int) bool {
		if dst[i].Date != dst[j].Date {
			return dst[i].Date < dst[j].Date
		}
		return dst[i].ID < dst[j].ID
	})
	return dst
}

// ListSMSThreads retrieves conversation threads. If cached and not forceRefresh,
// returns immediately from cache.
func (s *Service) ListSMSThreads(cursor string, limit int, forceRefresh bool) (SMSThreadsResult, error) {
	if len(cursor) > 256 {
		return SMSThreadsResult{}, errors.New("invalid cursor")
	}
	if limit == 0 {
		limit = core.DefaultSMSThreadsLimit
	}
	if limit < 1 || limit > core.MaxSMSThreadsPerResp {
		return SMSThreadsResult{}, errors.New("invalid limit")
	}
	if err := core.ValidateKeysetCursor(cursor); err != nil {
		return SMSThreadsResult{Error: "sms cursor rejected — resync from start", ErrorCode: core.CodeCursorInvalid}, err
	}

	s.messagesMu.Lock()
	if !forceRefresh && cursor == "" {
		if len(s.threadsCache) == 0 && s.db != nil {
			if dbThreads, err := s.db.LoadAllThreads(); err == nil && len(dbThreads) > 0 {
				s.threadsCache = dbThreads
			}
		}
		if len(s.threadsCache) > 0 {
			cached := SMSThreadsResult{
				Threads: append([]core.SMSThread{}, s.threadsCache...),
			}
			s.messagesMu.Unlock()
			// Mac-side fallback join: fill number-only rows from the cached
			// directory (phone PhoneLookup may have missed). Snapshot is
			// lock-free matching; phone values always win.
			contacts := s.contactLookupSnapshot()
			for i := range cached.Threads {
				enrichThreadWithContacts(&cached.Threads[i], contacts)
			}
			return cached, nil
		}
	}
	s.messagesMu.Unlock()

	if !s.IsPaired() {
		return SMSThreadsResult{Error: "phone is offline — reconnect first", ErrorCode: core.CodeSyncTimeout}, offlineSyncError("list threads")
	}
	if err := s.checkPeerCapability(core.CapabilityMessages, 13); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return SMSThreadsResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
		}
		return SMSThreadsResult{}, err
	}

	reqID, err := freshTransferID()
	if err != nil {
		return SMSThreadsResult{}, fmt.Errorf("fresh req_id: %w", err)
	}
	if len(reqID) > 16 {
		reqID = reqID[:16]
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return SMSThreadsResult{}, fmt.Errorf("fresh nonce: %w", err)
	}

	ch := make(chan SMSThreadsResult, 1)
	s.messagesMu.Lock()
	if s.pendingThreadsReqs == nil {
		s.pendingThreadsReqs = make(map[string]chan SMSThreadsResult)
	}
	if s.pendingThreadsMeta == nil {
		s.pendingThreadsMeta = make(map[string]smsPageMeta)
	}
	s.pendingThreadsReqs[reqID] = ch
	s.pendingThreadsMeta[reqID] = smsPageMeta{cursor: cursor, gen: s.smsGen}
	gen := s.smsGen
	s.messagesMu.Unlock()

	defer func() {
		s.messagesMu.Lock()
		delete(s.pendingThreadsReqs, reqID)
		delete(s.pendingThreadsMeta, reqID)
		s.messagesMu.Unlock()
	}()

	payload := core.SMSThreadsReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		Cursor:    cursor,
		Limit:     limit,
		CursorGen: gen,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSThreadsReq, &payload); err != nil {
		return SMSThreadsResult{}, fmt.Errorf("send sms-threads-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		contacts := s.contactLookupSnapshot()
		for i := range res.Threads {
			enrichThreadWithContacts(&res.Threads[i], contacts)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		if err := s.checkPeerCapability(core.CapabilityMessages, 13); err != nil {
			var upd *core.UpdateRequiredError
			if errors.As(err, &upd) {
				return SMSThreadsResult{Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
			}
			return SMSThreadsResult{}, err
		}
		return SMSThreadsResult{Error: "sms threads timed out — phone did not respond", ErrorCode: core.CodeSyncTimeout}, timeoutSyncError("sms threads")
	}
}

// ListSMSMessages retrieves messages for a specific conversation thread.
func (s *Service) ListSMSMessages(threadID int64, cursor string, limit int, forceRefresh bool) (SMSMessagesResult, error) {
	if threadID <= 0 {
		return SMSMessagesResult{}, errors.New("invalid thread id")
	}
	if len(cursor) > 256 {
		return SMSMessagesResult{}, errors.New("invalid cursor")
	}
	if limit == 0 {
		limit = core.DefaultSMSMessagesLimit
	}
	if limit < 1 || limit > core.MaxSMSMessagesPerResp {
		return SMSMessagesResult{}, errors.New("invalid limit")
	}
	if err := core.ValidateKeysetCursor(cursor); err != nil {
		return SMSMessagesResult{ThreadID: threadID, Error: "sms cursor rejected — resync from start", ErrorCode: core.CodeCursorInvalid}, err
	}

	s.messagesMu.Lock()
	if !forceRefresh && cursor == "" {
		if (s.messagesCache == nil || len(s.messagesCache[threadID]) == 0) && s.db != nil {
			if dbMsgs, err := s.db.LoadMessagesForThread(threadID, limit); err == nil && len(dbMsgs) > 0 {
				if s.messagesCache == nil {
					s.messagesCache = make(map[int64][]core.SMSMessage)
				}
				s.messagesCache[threadID] = dbMsgs
			}
		}
		if s.messagesCache != nil {
			if msgs, ok := s.messagesCache[threadID]; ok && len(msgs) > 0 {
				cached := SMSMessagesResult{
					ThreadID: threadID,
					Messages: append([]core.SMSMessage{}, msgs...),
				}
				// Populate NextCursor from the oldest message so the UI can page older messages
				oldest := msgs[0]
				for _, m := range msgs {
					if m.Date < oldest.Date || (m.Date == oldest.Date && m.ID < oldest.ID) {
						oldest = m
					}
				}
				if oldest.Date > 0 {
					cached.NextCursor = core.EncodeKeysetCursor(strconv.FormatInt(oldest.Date, 10), oldest.ID)
				}
				s.messagesMu.Unlock()
				contacts := s.contactLookupSnapshot()
				for i := range cached.Messages {
					enrichMessageWithContacts(&cached.Messages[i], contacts)
				}
				return cached, nil
			}
		}
	} else if !forceRefresh && cursor != "" && s.db != nil {
		if sortKey, rowID, ok := core.DecodeKeysetCursor(cursor); ok {
			if cursorDate, err := strconv.ParseInt(sortKey, 10, 64); err == nil && cursorDate > 0 {
				if dbMsgs, err := s.db.LoadMessagesBeforeCursor(threadID, cursorDate, rowID, limit); err == nil && len(dbMsgs) > 0 {
					cached := SMSMessagesResult{
						ThreadID: threadID,
						Messages: dbMsgs,
					}
					if len(dbMsgs) == limit {
						oldest := dbMsgs[0]
						cached.NextCursor = core.EncodeKeysetCursor(strconv.FormatInt(oldest.Date, 10), oldest.ID)
					}
					s.messagesMu.Unlock()
					contacts := s.contactLookupSnapshot()
					for i := range cached.Messages {
						enrichMessageWithContacts(&cached.Messages[i], contacts)
					}
					return cached, nil
				}
			}
		}
	}
	s.messagesMu.Unlock()

	if !s.IsPaired() {
		return SMSMessagesResult{ThreadID: threadID, Error: "phone is offline — reconnect first", ErrorCode: core.CodeSyncTimeout}, offlineSyncError("list messages")
	}
	if err := s.checkPeerCapability(core.CapabilityMessages, 13); err != nil {
		return SMSMessagesResult{ThreadID: threadID}, err
	}

	reqID, err := freshTransferID()
	if err != nil {
		return SMSMessagesResult{ThreadID: threadID}, fmt.Errorf("fresh req_id: %w", err)
	}
	if len(reqID) > 16 {
		reqID = reqID[:16]
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return SMSMessagesResult{ThreadID: threadID}, fmt.Errorf("fresh nonce: %w", err)
	}

	ch := make(chan SMSMessagesResult, 1)
	s.messagesMu.Lock()
	if s.pendingMessagesReqs == nil {
		s.pendingMessagesReqs = make(map[string]chan SMSMessagesResult)
	}
	if s.pendingMessagesMeta == nil {
		s.pendingMessagesMeta = make(map[string]smsPageMeta)
	}
	s.pendingMessagesReqs[reqID] = ch
	s.pendingMessagesMeta[reqID] = smsPageMeta{cursor: cursor, threadID: threadID, gen: s.smsGen}
	gen := s.smsGen
	s.messagesMu.Unlock()

	defer func() {
		s.messagesMu.Lock()
		delete(s.pendingMessagesReqs, reqID)
		delete(s.pendingMessagesMeta, reqID)
		s.messagesMu.Unlock()
	}()

	payload := core.SMSMessagesReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		ThreadID:  threadID,
		Cursor:    cursor,
		Limit:     limit,
		CursorGen: gen,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSMessagesReq, &payload); err != nil {
		return SMSMessagesResult{ThreadID: threadID}, fmt.Errorf("send sms-messages-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		contacts := s.contactLookupSnapshot()
		for i := range res.Messages {
			enrichMessageWithContacts(&res.Messages[i], contacts)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		return SMSMessagesResult{ThreadID: threadID, Error: "sms messages timed out — phone did not respond", ErrorCode: core.CodeSyncTimeout}, timeoutSyncError("sms messages")
	}
}

// SendSMS sends an SMS to the recipient address through the connected phone.
func (s *Service) SendSMS(recipient, body string) (SMSSendResult, error) {
	return s.SendSMSWithSubID(recipient, body, "")
}

// SendSMSWithSubID sends an SMS via an explicit Android subscription
// (dual-SIM). Empty subID means the phone default. The req_id/client_id pair
// is minted once per call: callers retrying after a timeout must reuse the
// returned ClientID path via SendSMSWithIDs instead of minting a new send.
func (s *Service) SendSMSWithSubID(recipient, body, subID string) (SMSSendResult, error) {
	cleanRecipient, ok := core.SanitizeSMSAddress(recipient)
	if !ok {
		return SMSSendResult{Ok: false, Error: "invalid recipient address"}, errors.New("invalid recipient address")
	}
	cleanBody, ok := core.SanitizeSMSBody(body)
	if !ok {
		return SMSSendResult{Ok: false, Error: "invalid message body"}, errors.New("invalid message body")
	}
	recipient = cleanRecipient
	body = cleanBody
	cleanSub, ok := core.SanitizeSubID(subID)
	if !ok {
		return SMSSendResult{Ok: false, Error: "invalid subscription id"}, errors.New("invalid subscription id")
	}

	if !s.IsPaired() {
		return SMSSendResult{Ok: false, Error: "phone is offline — reconnect first", ErrorCode: core.CodeSyncTimeout}, offlineSyncError("send sms")
	}
	if err := s.checkPeerCapability(core.CapabilityMessages, 13); err != nil {
		var upd *core.UpdateRequiredError
		if errors.As(err, &upd) {
			return SMSSendResult{Ok: false, Error: upd.Message, ErrorCode: core.CodeUpdateRequired}, err
		}
		return SMSSendResult{Ok: false, Error: err.Error()}, err
	}

	reqID, err := freshTransferID()
	if err != nil {
		return SMSSendResult{Ok: false}, fmt.Errorf("fresh req_id: %w", err)
	}
	if len(reqID) > 16 {
		reqID = reqID[:16]
	}
	clientID, _ := freshTransferID()
	if len(clientID) > 16 {
		clientID = clientID[:16]
	}
	nonce, err := core.FreshNonce()
	if err != nil {
		return SMSSendResult{Ok: false}, fmt.Errorf("fresh nonce: %w", err)
	}

	ch := make(chan SMSSendResult, 1)
	s.messagesMu.Lock()
	if s.pendingSendReqs == nil {
		s.pendingSendReqs = make(map[string]chan SMSSendResult)
	}
	s.pendingSendReqs[reqID] = ch
	s.messagesMu.Unlock()

	defer func() {
		s.messagesMu.Lock()
		delete(s.pendingSendReqs, reqID)
		s.messagesMu.Unlock()
	}()

	payload := core.SMSSendReqPayload{
		Nonce:     nonce,
		ReqID:     reqID,
		ClientID:  clientID,
		Recipient: recipient,
		Body:      body,
		SubID:     cleanSub,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSSendReq, &payload); err != nil {
		return SMSSendResult{Ok: false, ClientID: clientID, SubID: cleanSub, Error: err.Error()}, fmt.Errorf("send sms-send-req: %w", err)
	}

	select {
	case res := <-ch:
		if !res.Ok {
			return res, errors.New(res.Error)
		}
		// Optimistically append sent message to cache if thread exists
		if res.ThreadID > 0 {
			nowMs := time.Now().UnixMilli()
			sentMsg := core.SMSMessage{
				ID:       res.MessageID,
				ThreadID: res.ThreadID,
				Address:  recipient,
				Body:     body,
				Date:     nowMs,
				Type:     core.SMSMsgTypeSent,
				Read:     true,
			}
			s.messagesMu.Lock()
			if s.messagesCache != nil {
				s.messagesCache[res.ThreadID] = append(s.messagesCache[res.ThreadID], sentMsg)
			}
			// Update snippet in thread cache
			for i := range s.threadsCache {
				if s.threadsCache[i].ThreadID == res.ThreadID {
					s.threadsCache[i].Snippet = body
					s.threadsCache[i].Date = nowMs
					break
				}
			}
			s.messagesMu.Unlock()
			if s.db != nil {
				go func(m core.SMSMessage, tid int64) {
					_ = s.db.SaveMessages([]core.SMSMessage{m})
					s.messagesMu.Lock()
					tCopy := append([]core.SMSThread{}, s.threadsCache...)
					s.messagesMu.Unlock()
					_ = s.db.SaveThreads(tCopy)
				}(sentMsg, res.ThreadID)
			}
			s.emitMessagesChanged()
		}
		return res, nil
	case <-time.After(12 * time.Second):
		return SMSSendResult{Ok: false, ClientID: clientID, SubID: cleanSub, Error: "sms send timed out — phone did not reply", ErrorCode: core.CodeSyncTimeout}, timeoutSyncError("sms send")
	}
}

// MarkThreadRead marks a conversation thread as read on the Mac, updating
// the in-memory cache, the persistent SQLite database, emitting messages:changed,
// and forwarding the mark-as-read request to the paired phone.
func (s *Service) MarkThreadRead(threadID int64) error {
	if threadID <= 0 {
		return errors.New("invalid thread id")
	}
	s.messagesMu.Lock()
	found := false
	var address string
	for i := range s.threadsCache {
		if s.threadsCache[i].ThreadID == threadID {
			s.threadsCache[i].UnreadCount = 0
			s.threadsCache[i].Read = true
			address = s.threadsCache[i].Address
			found = true
			break
		}
	}
	if s.messagesCache != nil {
		if msgs, ok := s.messagesCache[threadID]; ok {
			for i := range msgs {
				msgs[i].Read = true
				if address == "" && msgs[i].Address != "" {
					address = msgs[i].Address
				}
			}
		}
	}
	s.messagesMu.Unlock()

	if s.db != nil {
		_ = s.db.MarkThreadReadInDB(threadID)
	}
	if found {
		s.emitMessagesChanged()
	}

	if s.IsPaired() {
		go func() {
			if err := s.checkPeerCapability(core.CapabilityMessages, 13); err != nil {
				return
			}
			reqID, err := freshTransferID()
			if err != nil {
				return
			}
			if len(reqID) > 16 {
				reqID = reqID[:16]
			}
			nonce, err := core.FreshNonce()
			if err != nil {
				return
			}

			ch := make(chan core.SMSMarkReadRespPayload, 1)
			s.messagesMu.Lock()
			if s.pendingMarkReadReqs == nil {
				s.pendingMarkReadReqs = make(map[string]chan core.SMSMarkReadRespPayload)
			}
			s.pendingMarkReadReqs[reqID] = ch
			s.messagesMu.Unlock()

			defer func() {
				s.messagesMu.Lock()
				delete(s.pendingMarkReadReqs, reqID)
				s.messagesMu.Unlock()
			}()

			payload := core.SMSMarkReadReqPayload{
				Nonce:    nonce,
				ReqID:    reqID,
				ThreadID: threadID,
				Address:  address,
			}
			if err := s.sendFeatureToPhone(core.TypeSMSMarkReadReq, &payload); err != nil {
				return
			}

			select {
			case <-ch:
			case <-time.After(5 * time.Second):
			}
		}()
	}

	return nil
}

// ingestMessagesBody parses inbound SMS envelopes from phone.
func (s *Service) ingestMessagesBody(body []byte) {
	var env core.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}

	switch env.Type {
	case core.TypeSMSThreadsResp:
		var resp core.SMSThreadsRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		// Snapshot directory before taking messagesMu (lock order:
		// contactsMu -> messagesMu never inverted) for fallback join.
		contacts := s.contactLookupSnapshot()
		for i := range resp.Threads {
			enrichThreadWithContacts(&resp.Threads[i], contacts)
		}
		s.messagesMu.Lock()
		ch, ok := s.pendingThreadsReqs[resp.ReqID]
		meta, hasMeta := s.pendingThreadsMeta[resp.ReqID]
		// Only mutate the cache for a live waiter. Late responses after
		// timeout/disconnect/change-wipe must not resurrect stale pages.
		// Permission errors freeze (never wipe) so the UI keeps stale rows
		// with an error banner instead of a blank list.
		if ok && resp.Error == "" {
			if hasMeta && meta.gen != s.smsGen {
				ok = false
			} else if hasMeta && meta.cursor == "" {
				s.threadsCache = append([]core.SMSThread{}, resp.Threads...)
			} else if resp.NextCursor == "" && (!hasMeta || meta.cursor == "") {
				s.threadsCache = resp.Threads
			} else {
				s.threadsCache = mergeSMSThreads(s.threadsCache, resp.Threads)
			}
			if s.db != nil && len(s.threadsCache) > 0 {
				go func(t []core.SMSThread) {
					_ = s.db.SaveThreads(t)
				}(append([]core.SMSThread{}, s.threadsCache...))
			}
		}
		s.messagesMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- SMSThreadsResult{
				Threads:    resp.Threads,
				NextCursor: resp.NextCursor,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}:
			default:
			}
		}

	case core.TypeSMSMessagesResp:
		var resp core.SMSMessagesRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		contacts := s.contactLookupSnapshot()
		for i := range resp.Messages {
			enrichMessageWithContacts(&resp.Messages[i], contacts)
		}
		s.messagesMu.Lock()
		ch, ok := s.pendingMessagesReqs[resp.ReqID]
		meta, hasMeta := s.pendingMessagesMeta[resp.ReqID]
		if ok && resp.Error == "" {
			if hasMeta && (meta.gen != s.smsGen || meta.threadID != resp.ThreadID) {
				ok = false
			} else {
				if s.messagesCache == nil {
					s.messagesCache = make(map[int64][]core.SMSMessage)
				}
				if hasMeta && meta.cursor == "" {
					s.messagesCache[resp.ThreadID] = append([]core.SMSMessage{}, resp.Messages...)
				} else if resp.NextCursor == "" && (!hasMeta || meta.cursor == "") {
					s.messagesCache[resp.ThreadID] = resp.Messages
				} else {
					s.messagesCache[resp.ThreadID] = mergeSMSMessages(s.messagesCache[resp.ThreadID], resp.Messages)
				}
				if s.db != nil && len(resp.Messages) > 0 {
					go func(m []core.SMSMessage) {
						_ = s.db.SaveMessages(m)
					}(append([]core.SMSMessage{}, resp.Messages...))
				}
			}
		}
		s.messagesMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- SMSMessagesResult{
				ThreadID:   resp.ThreadID,
				Messages:   resp.Messages,
				NextCursor: resp.NextCursor,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}:
			default:
			}
		}

	case core.TypeSMSSendResp:
		var resp core.SMSSendRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		s.messagesMu.Lock()
		ch, ok := s.pendingSendReqs[resp.ReqID]
		s.messagesMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- SMSSendResult{
				Ok:         resp.OK,
				ClientID:   resp.ClientID,
				MessageID:  resp.MessageID,
				ThreadID:   resp.ThreadID,
				SubID:      resp.SubID,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}:
			default:
			}
		}

	case core.TypeSMSMarkReadResp:
		var resp core.SMSMarkReadRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}
		if resp.ReqID == "" {
			return
		}

		s.messagesMu.Lock()
		ch, ok := s.pendingMarkReadReqs[resp.ReqID]
		s.messagesMu.Unlock()

		if ok && ch != nil {
			select {
			case ch <- resp:
			default:
			}
		}

	case core.TypeSMSPush:
		var push core.SMSPushPayload
		if err := json.Unmarshal(env.Payload, &push); err != nil {
			return
		}

		// Fallback join for pushes whose PhoneLookup missed: directory may
		// already hold the number in another format. Phone values win.
		contacts := s.contactLookupSnapshot()
		if push.ContactID == "" && push.ContactName == "" {
			if name, id, ver, ok := lookupContactForAddress(contacts, push.Message.Address); ok {
				push.ContactName, push.ContactID, push.PhotoVersion = name, id, ver
			}
		}
		enrichMessageWithContacts(&push.Message, contacts)
		// Keep top-level and message-level identity consistent for old/new Mac.
		if push.ContactName == "" {
			push.ContactName = push.Message.ContactName
		}
		if push.ContactID == "" {
			push.ContactID = push.Message.ContactID
		}
		if push.PhotoVersion == "" {
			push.PhotoVersion = push.Message.PhotoVersion
		}
		if push.Message.ContactName == "" {
			push.Message.ContactName = push.ContactName
		}
		if push.Message.ContactID == "" {
			push.Message.ContactID = push.ContactID
		}
		if push.Message.PhotoVersion == "" {
			push.Message.PhotoVersion = push.PhotoVersion
		}
		s.messagesMu.Lock()
		// At-least-once redelivery guard: duplicates resend no cache mutate.
		if !s.markSMSPushSeen(smsPushDedupKey(push.Message, push.ClientID)) {
			s.messagesMu.Unlock()
			return
		}
		threadID := push.Message.ThreadID
		// Legacy thread_id=0 pushes (old phones) carry no cache position:
		// skip mutation and let the follow-up sms-changed invalidate.
		if s.messagesCache != nil && threadID > 0 {
			if msgs, exists := s.messagesCache[threadID]; exists {
				// ID-level dedup inside the thread transcript.
				dup := false
				if push.Message.ID != 0 {
					for _, m := range msgs {
						if m.ID == push.Message.ID {
							dup = true
							break
						}
					}
				}
				if !dup {
					s.messagesCache[threadID] = append(msgs, push.Message)
				}
			}
		}
		// Update thread cache
		found := false
		for i := range s.threadsCache {
			if s.threadsCache[i].ThreadID == threadID {
				s.threadsCache[i].Snippet = push.Message.Body
				s.threadsCache[i].Date = push.Message.Date
				s.threadsCache[i].UnreadCount++
				s.threadsCache[i].Read = false
				if push.ContactName != "" {
					s.threadsCache[i].ContactName = push.ContactName
				}
				if push.ContactID != "" {
					s.threadsCache[i].ContactID = push.ContactID
				}
				if push.PhotoVersion != "" {
					s.threadsCache[i].PhotoVersion = push.PhotoVersion
				}
				found = true
				break
			}
		}
		if !found && threadID > 0 {
			// Thread not in cache, prepend new thread entry
			newThread := core.SMSThread{
				ThreadID:     threadID,
				Address:      push.Message.Address,
				ContactName:  push.ContactName,
				ContactID:    push.ContactID,
				PhotoVersion: push.PhotoVersion,
				Snippet:      push.Message.Body,
				Date:         push.Message.Date,
				UnreadCount:  1,
				Read:         false,
			}
			s.threadsCache = append([]core.SMSThread{newThread}, s.threadsCache...)
		}
		if s.db != nil {
			go func(m core.SMSMessage, threads []core.SMSThread) {
				_ = s.db.SaveMessages([]core.SMSMessage{m})
				_ = s.db.SaveThreads(threads)
			}(push.Message, append([]core.SMSThread{}, s.threadsCache...))
		}
		s.messagesMu.Unlock()

	case core.TypeSMSChanged:
		s.messagesMu.Lock()
		s.smsGen++
		s.threadsCache = nil
		s.messagesCache = nil
		s.messagesMu.Unlock()
	}
}
