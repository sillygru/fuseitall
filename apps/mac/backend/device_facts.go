// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"encoding/json"

	"fuseitall/core"
)

// DeviceFacts is the phone's self-advertised identity from an accepted ping
// payload. Every field is optional; Has* reports presence so callers keep
// the previous reading for absent fields (older phones simply advertise
// nothing). Pure data, no I/O.
type DeviceFacts struct {
	DeviceName  string
	HasName     bool
	Model       string
	HasModel    bool
	BatteryPct  int
	HasBattery  bool
	Charging    bool
	HasCharging bool
}

// ParsePeerDevice extracts the advertised device facts (device_name, model,
// battery_pct, charging) from a raw ping envelope body. Fail-soft per field:
// malformed JSON, wrong type, or out-of-range values yield absent fields,
// never an error — the ping itself is still valid. Charging without a valid
// battery level is dropped (it would otherwise read as "not charging").
// Pure: no I/O, no state.
func ParsePeerDevice(body []byte) DeviceFacts {
	var facts DeviceFacts
	var env struct {
		Type    string `json:"type"`
		Payload struct {
			DeviceName string `json:"device_name"`
			Model      string `json:"model"`
			BatteryPct *int   `json:"battery_pct"`
			Charging   *bool  `json:"charging"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return facts
	}
	if env.Type != core.TypePing {
		return facts
	}
	if name, ok := core.SanitizeDeviceLabel(env.Payload.DeviceName); ok {
		facts.DeviceName, facts.HasName = name, true
	}
	if model, ok := core.SanitizeDeviceLabel(env.Payload.Model); ok {
		facts.Model, facts.HasModel = model, true
	}
	if env.Payload.BatteryPct != nil && core.SanitizeBatteryPct(*env.Payload.BatteryPct) {
		facts.BatteryPct, facts.HasBattery = *env.Payload.BatteryPct, true
		if env.Payload.Charging != nil {
			facts.Charging, facts.HasCharging = *env.Payload.Charging, true
		}
	}
	return facts
}

// displayPhoneName resolves what the UI shows for the phone: the Mac-local
// rename alias when set, else the advertised name, else "" (the UI falls
// back to "Phone" so older frontends keep working). Pure.
func displayPhoneName(custom, advertised string) string {
	if custom != "" {
		return custom
	}
	return advertised
}
