// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"
	"testing"

	"fuseitall/core"
)

func TestDNDStore(t *testing.T) {
	store := NewDNDStore()
	initial := store.Get()
	if initial.HasState || initial.Enabled {
		t.Fatalf("expected empty initial state, got %+v", initial)
	}

	applied := store.ApplyRemote(core.DNDStatePayload{
		Nonce:         "n1",
		Enabled:       true,
		UpdatedMs:     1000,
		HasPermission: true,
	})
	if !applied {
		t.Fatalf("expected initial ApplyRemote to return true")
	}

	cur := store.Get()
	if !cur.HasState || !cur.Enabled || !cur.HasPermission || cur.UpdatedMs != 1000 {
		t.Fatalf("unexpected state after ApplyRemote: %+v", cur)
	}

	// Stale timestamp should be rejected
	stale := store.ApplyRemote(core.DNDStatePayload{
		Nonce:         "n2",
		Enabled:       false,
		UpdatedMs:     500,
		HasPermission: true,
	})
	if stale {
		t.Fatalf("expected stale ApplyRemote to return false")
	}
	if !store.Get().Enabled {
		t.Fatalf("expected state to remain enabled")
	}

	// Newer timestamp should be accepted
	newer := store.ApplyRemote(core.DNDStatePayload{
		Nonce:         "n3",
		Enabled:       false,
		UpdatedMs:     2000,
		HasPermission: true,
	})
	if !newer {
		t.Fatalf("expected newer ApplyRemote to return true")
	}
	if store.Get().Enabled {
		t.Fatalf("expected state to update to disabled")
	}

	// Optimistic update
	store.SetOptimistic(true)
	if !store.Get().Enabled {
		t.Fatalf("expected optimistic toggle to enable DND")
	}

	// Clear
	store.Clear()
	if store.Get().HasState {
		t.Fatalf("expected HasState to be false after Clear")
	}
}

func TestServiceDND(t *testing.T) {
	svc := NewService("", "", "tok", NewLogBuffer(10))
	if cur := svc.GetDND(); cur.HasState || cur.Enabled {
		t.Fatalf("expected empty initial DND state from service, got %+v", cur)
	}

	// Offline fails loud.
	if _, err := svc.SetDND(true); err == nil || err.Error() != "phone is offline — reconnect first" {
		t.Fatalf("expected offline error, got %v", err)
	}

	// Ingest valid dnd-state envelope body
	sender := core.CurrentSender("android")
	caps := []string{core.CapabilityPing, core.CapabilityDND}
	state := core.DNDStatePayload{
		Nonce:         "n1",
		Enabled:       true,
		UpdatedMs:     5000,
		HasPermission: true,
		Origin:        "android",
	}
	env, err := core.NewEnvelope(core.TypeDNDState, sender, caps, state)
	if err != nil {
		t.Fatalf("build envelope: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	svc.ingestDNDBody(raw)
	cur := svc.GetDND()
	if !cur.HasState || !cur.Enabled || !cur.HasPermission || cur.UpdatedMs != 5000 {
		t.Fatalf("expected ingested DND state, got %+v", cur)
	}
}
