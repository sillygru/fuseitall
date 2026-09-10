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
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Permission string `json:"permission,omitempty"`
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

	s.messagesMu.Lock()
	if !forceRefresh && cursor == "" && len(s.threadsCache) > 0 {
		cached := SMSThreadsResult{
			Threads: append([]core.SMSThread{}, s.threadsCache...),
		}
		s.messagesMu.Unlock()
		return cached, nil
	}
	s.messagesMu.Unlock()

	if !s.IsPaired() {
		return SMSThreadsResult{}, errors.New("phone is offline — reconnect first")
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

	ch := make(chan SMSThreadsResult, 1)
	s.messagesMu.Lock()
	if s.pendingThreadsReqs == nil {
		s.pendingThreadsReqs = make(map[string]chan SMSThreadsResult)
	}
	s.pendingThreadsReqs[reqID] = ch
	s.messagesMu.Unlock()

	defer func() {
		s.messagesMu.Lock()
		delete(s.pendingThreadsReqs, reqID)
		s.messagesMu.Unlock()
	}()

	payload := core.SMSThreadsReqPayload{
		ReqID:  reqID,
		Cursor: cursor,
		Limit:  limit,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSThreadsReq, &payload); err != nil {
		return SMSThreadsResult{}, fmt.Errorf("send sms-threads-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
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
		return SMSThreadsResult{}, errors.New("sms threads timed out — phone did not respond")
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

	s.messagesMu.Lock()
	if !forceRefresh && cursor == "" && s.messagesCache != nil {
		if msgs, ok := s.messagesCache[threadID]; ok && len(msgs) > 0 {
			cached := SMSMessagesResult{
				ThreadID: threadID,
				Messages: append([]core.SMSMessage{}, msgs...),
			}
			s.messagesMu.Unlock()
			return cached, nil
		}
	}
	s.messagesMu.Unlock()

	if !s.IsPaired() {
		return SMSMessagesResult{ThreadID: threadID}, errors.New("phone is offline — reconnect first")
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

	ch := make(chan SMSMessagesResult, 1)
	s.messagesMu.Lock()
	if s.pendingMessagesReqs == nil {
		s.pendingMessagesReqs = make(map[string]chan SMSMessagesResult)
	}
	s.pendingMessagesReqs[reqID] = ch
	s.messagesMu.Unlock()

	defer func() {
		s.messagesMu.Lock()
		delete(s.pendingMessagesReqs, reqID)
		s.messagesMu.Unlock()
	}()

	payload := core.SMSMessagesReqPayload{
		ReqID:    reqID,
		ThreadID: threadID,
		Cursor:   cursor,
		Limit:    limit,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSMessagesReq, &payload); err != nil {
		return SMSMessagesResult{ThreadID: threadID}, fmt.Errorf("send sms-messages-req: %w", err)
	}

	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(8 * time.Second):
		return SMSMessagesResult{ThreadID: threadID}, errors.New("sms messages timed out — phone did not respond")
	}
}

// SendSMS sends an SMS to the recipient address through the connected phone.
func (s *Service) SendSMS(recipient, body string) (SMSSendResult, error) {
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

	if !s.IsPaired() {
		return SMSSendResult{Ok: false, Error: "phone is offline — reconnect first"}, errors.New("phone is offline — reconnect first")
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
		ReqID:     reqID,
		ClientID:  clientID,
		Recipient: recipient,
		Body:      body,
	}
	if err := s.sendFeatureToPhone(core.TypeSMSSendReq, &payload); err != nil {
		return SMSSendResult{Ok: false, ClientID: clientID, Error: err.Error()}, fmt.Errorf("send sms-send-req: %w", err)
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
			s.emitMessagesChanged()
		}
		return res, nil
	case <-time.After(12 * time.Second):
		return SMSSendResult{Ok: false, ClientID: clientID, Error: "sms send timed out — phone did not reply"}, errors.New("sms send timed out")
	}
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

		s.messagesMu.Lock()
		if resp.NextCursor == "" {
			s.threadsCache = resp.Threads
		} else {
			s.threadsCache = append(s.threadsCache, resp.Threads...)
		}
		ch, ok := s.pendingThreadsReqs[resp.ReqID]
		s.messagesMu.Unlock()

		if ok && ch != nil {
			ch <- SMSThreadsResult{
				Threads:    resp.Threads,
				NextCursor: resp.NextCursor,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}
		}

	case core.TypeSMSMessagesResp:
		var resp core.SMSMessagesRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}

		s.messagesMu.Lock()
		if s.messagesCache == nil {
			s.messagesCache = make(map[int64][]core.SMSMessage)
		}
		if resp.NextCursor == "" {
			s.messagesCache[resp.ThreadID] = resp.Messages
		} else {
			s.messagesCache[resp.ThreadID] = append(s.messagesCache[resp.ThreadID], resp.Messages...)
		}
		ch, ok := s.pendingMessagesReqs[resp.ReqID]
		s.messagesMu.Unlock()

		if ok && ch != nil {
			ch <- SMSMessagesResult{
				ThreadID:   resp.ThreadID,
				Messages:   resp.Messages,
				NextCursor: resp.NextCursor,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}
		}

	case core.TypeSMSSendResp:
		var resp core.SMSSendRespPayload
		if err := json.Unmarshal(env.Payload, &resp); err != nil {
			return
		}

		s.messagesMu.Lock()
		ch, ok := s.pendingSendReqs[resp.ReqID]
		s.messagesMu.Unlock()

		if ok && ch != nil {
			ch <- SMSSendResult{
				Ok:         resp.OK,
				ClientID:   resp.ClientID,
				MessageID:  resp.MessageID,
				ThreadID:   resp.ThreadID,
				Error:      resp.Error,
				ErrorCode:  resp.ErrorCode,
				Permission: resp.Permission,
			}
		}

	case core.TypeSMSPush:
		var push core.SMSPushPayload
		if err := json.Unmarshal(env.Payload, &push); err != nil {
			return
		}

		s.messagesMu.Lock()
		threadID := push.Message.ThreadID
		// If messages cache for this thread is active, append incoming message
		if s.messagesCache != nil && threadID > 0 {
			if msgs, exists := s.messagesCache[threadID]; exists {
				s.messagesCache[threadID] = append(msgs, push.Message)
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
				found = true
				break
			}
		}
		if !found && threadID > 0 {
			// Thread not in cache, prepend new thread entry
			newThread := core.SMSThread{
				ThreadID:    threadID,
				Address:     push.Message.Address,
				Snippet:     push.Message.Body,
				Date:        push.Message.Date,
				UnreadCount: 1,
				Read:        false,
			}
			s.threadsCache = append([]core.SMSThread{newThread}, s.threadsCache...)
		}
		s.messagesMu.Unlock()

	case core.TypeSMSChanged:
		s.messagesMu.Lock()
		s.threadsCache = nil
		s.messagesCache = nil
		s.messagesMu.Unlock()
	}
}
