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
	"strings"
	"time"

	"fuseitall/core"
)

// notifAppsPageTimeout bounds one inventory page round-trip (same 8s budget
// as file/photo listings: LAN is milliseconds, the timeout is only for a
// vanished phone mid-fetch).
const notifAppsPageTimeout = 8 * time.Second

// notifAppsCacheTTL keeps rapid Settings re-selects from re-paging the
// phone: a fresh fetch is at most this old before RequestPhoneNotifApps
// dials again. Manual Refresh always dials (bypasses the cache).
const notifAppsCacheTTL = 30 * time.Second

// ErrNotifAppsNotSupported signals a phone that predates the inventory
// types: it answered 400 wrong_type ("unexpected message type") to our
// request. Callers fall back to GetKnownNotifApps with an inline note,
// never a blocking update banner (no version bump for this additive
// feature).
var ErrNotifAppsNotSupported = errors.New("phone does not support app list sharing yet")

// ParseNotifAppsResp extracts an accepted notif-apps-resp payload from a raw
// feature envelope body. ok is false for wrong types or malformed pages.
// Pure.
func ParseNotifAppsResp(body []byte) (core.NotifAppsRespPayload, bool) {
	var env struct {
		Type    string                     `json:"type"`
		Payload core.NotifAppsRespPayload `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return core.NotifAppsRespPayload{}, false
	}
	if env.Type != core.TypeNotifAppsResp {
		return core.NotifAppsRespPayload{}, false
	}
	if !core.SanitizeNotifAppsResp(env.Payload) {
		return core.NotifAppsRespPayload{}, false
	}
	return env.Payload, true
}

// LearnIcon remembers one package icon (fail-soft: invalid/empty icons are
// dropped, never stored). Safe for concurrent use.
func (s *NotifStore) LearnIcon(pkg, iconB64 string) {
	pkg = core.SanitizePackageName(pkg)
	icon := core.SanitizeNotifIconB64(iconB64)
	if pkg == "" || icon == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iconCache == nil {
		s.iconCache = make(map[string]string)
	}
	s.iconCache[pkg] = icon
}

// listPhoneNotifAppsPage requests one inventory page and waits for the
// notif-apps-resp push correlated by req_id. Cursor empty means the first
// page. It returns the raw response payload (entries + next cursor).
func (s *Service) listPhoneNotifAppsPage(cursor string, limit int) (core.NotifAppsRespPayload, error) {
	if cursor != "" && core.SanitizePackageName(cursor) == "" {
		return core.NotifAppsRespPayload{}, errors.New("invalid cursor")
	}
	if limit == 0 {
		limit = core.DefaultNotifAppsLimit
	}
	if limit < 1 || limit > core.MaxNotifAppsPerResp {
		return core.NotifAppsRespPayload{}, errors.New("invalid limit")
	}
	if !s.IsPaired() {
		return core.NotifAppsRespPayload{}, errors.New("phone is offline — reconnect first")
	}
	if err := s.checkPeerCapability(core.CapabilityNotifications, 1); err != nil {
		return core.NotifAppsRespPayload{}, err
	}
	reqID, err := freshTransferID()
	if err != nil {
		return core.NotifAppsRespPayload{}, err
	}
	reqID = reqID[:16]
	ch := make(chan core.NotifAppsRespPayload, 1)
	s.notifAppsMu.Lock()
	if s.pendingNotifApps == nil {
		s.pendingNotifApps = make(map[string]chan core.NotifAppsRespPayload)
	}
	s.pendingNotifApps[reqID] = ch
	s.notifAppsMu.Unlock()
	defer func() {
		s.notifAppsMu.Lock()
		delete(s.pendingNotifApps, reqID)
		s.notifAppsMu.Unlock()
	}()

	withIcons := true
	payload := core.NotifAppsReqPayload{ReqID: reqID, Cursor: cursor, Limit: limit, WithIcons: &withIcons}
	if err := s.sendFeatureToPhone(core.TypeNotifAppsReq, &payload); err != nil {
		if isNotifAppsUnsupported(err) {
			return core.NotifAppsRespPayload{}, ErrNotifAppsNotSupported
		}
		return core.NotifAppsRespPayload{}, fmt.Errorf("send notif-apps-req: %w", err)
	}
	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(notifAppsPageTimeout):
		return core.NotifAppsRespPayload{}, errors.New("app list timed out — phone did not respond")
	}
}

// isNotifAppsUnsupported reports whether a send error means the phone
// predates the inventory types (core gate rejects unknown types with
// "unexpected message type", surfaced as a peer BAD_REQUEST error).
// Fragile by nature (string match across the wire): callers treat a miss
// as a generic send failure, which falls back the same way.
func isNotifAppsUnsupported(err error) bool {
	if errors.Is(err, ErrNotifAppsNotSupported) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "unexpected message type")
}

// RequestPhoneNotifApps fetches the full phone inventory (paged, icons
// included), merges it over the mirrored known-apps view (phone labels and
// icons win; mute/allow state and mirror counts are local), feeds the icon
// cache, and returns the merged rows sorted like GetKnownNotifApps (count
// desc, label asc). Results are cached briefly; pass refresh=true (the
// Settings Refresh button) to bypass the cache and dial again.
//
// On any failure (offline, timeout, pre-inventory phone) it returns the
// error and the caller falls back to GetKnownNotifApps: the filter toggles
// keep working with whatever the mirror already knows.
func (s *Service) RequestPhoneNotifApps(refresh bool) ([]KnownNotifApp, error) {
	if !refresh {
		if cached, ok := s.cachedPhoneNotifApps(); ok {
			return cached, nil
		}
	}
	merged, err := s.fetchPhoneNotifApps()
	if err != nil {
		return nil, err
	}
	s.notifAppsMu.Lock()
	s.lastPhoneNotifApps = merged
	s.lastPhoneNotifAppsAt = time.Now()
	s.notifAppsMu.Unlock()
	return merged, nil
}

// cachedPhoneNotifApps returns the last merged inventory when fresh.
func (s *Service) cachedPhoneNotifApps() ([]KnownNotifApp, bool) {
	s.notifAppsMu.Lock()
	defer s.notifAppsMu.Unlock()
	if len(s.lastPhoneNotifApps) == 0 {
		return nil, false
	}
	if time.Since(s.lastPhoneNotifAppsAt) >= notifAppsCacheTTL {
		return nil, false
	}
	return append([]KnownNotifApp{}, s.lastPhoneNotifApps...), true
}

// fetchPhoneNotifApps pages the phone until next_cursor empties or the
// total cap is reached, then merges over GetKnownNotifApps.
func (s *Service) fetchPhoneNotifApps() ([]KnownNotifApp, error) {
	base := s.GetKnownNotifApps()
	byPkg := make(map[string]*KnownNotifApp, len(base)+core.MaxNotifAppsPerResp)
	order := make([]string, 0, len(base)+core.MaxNotifAppsPerResp)
	for i := range base {
		pkg := base[i].PackageName
		if pkg == "" {
			continue
		}
		if _, ok := byPkg[pkg]; ok {
			continue
		}
		row := base[i]
		byPkg[pkg] = &row
		order = append(order, pkg)
	}
	ensure := func(pkg, label string) *KnownNotifApp {
		if a, ok := byPkg[pkg]; ok {
			if a.App == "" || a.App == pkg {
				if label != "" {
					a.App = label
				}
			}
			return a
		}
		a := &KnownNotifApp{PackageName: pkg, App: label}
		if a.App == "" {
			a.App = pkg
		}
		byPkg[pkg] = a
		order = append(order, pkg)
		return a
	}

	cursor := ""
	for len(byPkg) < core.MaxNotifAppsTotal {
		page, err := s.listPhoneNotifAppsPage(cursor, core.MaxNotifAppsPerResp)
		if err != nil {
			return nil, err
		}
		if len(page.Entries) == 0 && page.NextCursor == "" {
			break
		}
		for _, e := range page.Entries {
			pkg := core.SanitizePackageName(e.PackageName)
			if pkg == "" {
				continue
			}
			label := core.TruncateNotifField(e.App, core.MaxNotifAppLen)
			if label == "" {
				label = pkg
			}
			a := ensure(pkg, label)
			if a.App == pkg && label != "" {
				a.App = label
			}
			if icon := core.SanitizeNotifIconB64(e.IconB64); icon != "" {
				a.IconB64 = icon
				s.notifs.LearnIcon(pkg, icon)
			} else if a.IconB64 == "" {
				a.IconB64 = s.notifs.IconForPackage(pkg)
			}
			if len(byPkg) >= core.MaxNotifAppsTotal {
				break
			}
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			// Phone repeated a cursor: stop instead of looping forever.
			break
		}
		cursor = page.NextCursor
	}
	return flattenNotifApps(byPkg, order), nil
}

// flattenNotifApps sorts rows count desc, label asc (same order as
// GetKnownNotifApps) and guarantees a non-nil slice for the binding.
func flattenNotifApps(byPkg map[string]*KnownNotifApp, order []string) []KnownNotifApp {
	out := make([]KnownNotifApp, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, pkg := range order {
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		if a, ok := byPkg[pkg]; ok {
			if a.App == "" {
				a.App = pkg
			}
			out = append(out, *a)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			swap := false
			if out[j].Count != out[j-1].Count {
				swap = out[j].Count > out[j-1].Count
			} else {
				swap = out[j].App < out[j-1].App
			}
			if !swap {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if out == nil {
		out = []KnownNotifApp{}
	}
	return out
}

// ingestNotifAppsRespBody resolves a pending inventory page (or drops it
// when nobody waits: a late/duplicate page after timeout). Accepted bodies
// already passed the token gate, so the peer version is refreshed like the
// file/photo ingest paths.
func (s *Service) ingestNotifAppsRespBody(body []byte) {
	var env struct {
		Type         string          `json:"type"`
		Sender       core.SenderInfo `json:"sender"`
		Capabilities []string        `json:"capabilities"`
		Payload      json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	s.learnPeer(env.Sender.Platform, env.Sender.AppBuild, env.Sender.AppVersion, env.Capabilities)
	p, ok := ParseNotifAppsResp(body)
	if !ok {
		return
	}
	s.notifAppsMu.Lock()
	ch, waiting := s.pendingNotifApps[p.ReqID]
	s.notifAppsMu.Unlock()
	if !waiting {
		return
	}
	select {
	case ch <- p:
	default:
	}
}

// failPendingNotifApps unblocks all in-flight inventory pages when the peer
// disconnects so Settings Refresh fails fast instead of hanging 8s/page.
func (s *Service) failPendingNotifApps(err error) {
	s.notifAppsMu.Lock()
	defer s.notifAppsMu.Unlock()
	for reqID, ch := range s.pendingNotifApps {
		select {
		case ch <- core.NotifAppsRespPayload{ReqID: reqID, Error: err.Error(), ErrorCode: core.ErrorCodeInternal}:
		default:
		}
		delete(s.pendingNotifApps, reqID)
	}
}
