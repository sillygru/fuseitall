// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"errors"
	"testing"
)

func TestVersionMatrix(t *testing.T) {
	t.Run("same build passes", func(t *testing.T) {
		sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild, MinPeerBuild: CurrentMinPeerBuild}
		if err := CheckPeerVersion(sender); err != nil {
			t.Fatalf("CheckPeerVersion(same) = %v, want nil", err)
		}
	})

	t.Run("peer older build is peer outdated", func(t *testing.T) {
		sender := SenderInfo{Platform: "android", AppBuild: CurrentMinPeerBuild - 1, MinPeerBuild: 0}
		if err := CheckPeerVersion(sender); !errors.Is(err, ErrPeerOutdated) {
			t.Fatalf("CheckPeerVersion(peer-older) = %v, want ErrPeerOutdated", err)
		}
	})

	t.Run("peer demanding newer build means we are outdated", func(t *testing.T) {
		sender := SenderInfo{Platform: "android", AppBuild: CurrentBuild + 9, MinPeerBuild: CurrentBuild + 1}
		if err := CheckPeerVersion(sender); !errors.Is(err, ErrLocalOutdated) {
			t.Fatalf("CheckPeerVersion(we-older) = %v, want ErrLocalOutdated", err)
		}
	})

	t.Run("newer protocol is unsupported", func(t *testing.T) {
		if err := CheckProtocolVersion(CurrentProtocolV + 1); !errors.Is(err, ErrUnsupportedProtocol) {
			t.Fatalf("CheckProtocolVersion(newer) = %v, want ErrUnsupportedProtocol", err)
		}
		if err := CheckProtocolVersion(CurrentProtocolV); err != nil {
			t.Fatalf("CheckProtocolVersion(current) = %v, want nil", err)
		}
	})

	t.Run("unknown protocol has no required build", func(t *testing.T) {
		if _, err := RequiredBuildFor(99); !errors.Is(err, ErrUnsupportedProtocol) {
			t.Fatalf("RequiredBuildFor(99) = %v, want ErrUnsupportedProtocol", err)
		}
		build, err := RequiredBuildFor(CurrentProtocolV)
		if err != nil || build != CurrentMinPeerBuild {
			t.Fatalf("RequiredBuildFor(current) = %d, %v; want %d, nil", build, err, CurrentMinPeerBuild)
		}
	})

	t.Run("qr without v defaults to 1 and ignores unknown fields", func(t *testing.T) {
		raw := []byte(`{"device_name":"mac","platform":"mac","host":"10.0.0.2","port":4433,` +
			`"fingerprint":"` + zeros64() + `","pubkey":"eA==","token":"` + zeros32() + `","future":"x"}`)
		payload, err := ParsePairQR(raw)
		if err != nil {
			t.Fatalf("ParsePairQR(missing v) = %v, want nil", err)
		}
		if payload.V != 1 {
			t.Fatalf("ParsePairQR V = %d, want 1", payload.V)
		}
	})

	t.Run("qr newer v is unsupported", func(t *testing.T) {
		raw := []byte(`{"v":2,"device_name":"mac","platform":"mac","host":"h","port":1,` +
			`"fingerprint":"f","pubkey":"p","token":"t"}`)
		if _, err := ParsePairQR(raw); !errors.Is(err, ErrUnsupportedProtocol) {
			t.Fatalf("ParsePairQR(v=2) = %v, want ErrUnsupportedProtocol", err)
		}
	})

	t.Run("header gates newer protocol before peer logic", func(t *testing.T) {
		raw := []byte(`{"protocol_v":99,"type":"ping","sender":{"platform":"android",` +
			`"app_build":0,"min_peer_build":0},"capabilities":[]}`)
		if _, err := ParseAndGateHeader(raw); !errors.Is(err, ErrUnsupportedProtocol) {
			t.Fatalf("ParseAndGateHeader(v99) = %v, want ErrUnsupportedProtocol", err)
		}
	})

	t.Run("header gates outdated peer", func(t *testing.T) {
		raw := []byte(`{"protocol_v":1,"type":"ping","sender":{"platform":"android",` +
			`"app_build":0,"min_peer_build":0},"capabilities":[]}`)
		if _, err := ParseAndGateHeader(raw); !errors.Is(err, ErrPeerOutdated) {
			t.Fatalf("ParseAndGateHeader(peer-older) = %v, want ErrPeerOutdated", err)
		}
	})
}

func TestUpdateRequiredMessage(t *testing.T) {
	payload := NewUpdateRequiredPayload("mac", 7)
	want := "Update FuseItAll on mac to build >= 7"
	if payload.Message != want {
		t.Fatalf("message = %q, want %q", payload.Message, want)
	}
	if payload.Code != CodeUpdateRequired || payload.RequiredBuild != 7 || payload.Device != "mac" {
		t.Fatalf("payload = %+v, want code/device/build set", payload)
	}

	known := NewUpdateRequiredPayload("android", CurrentBuild)
	if known.RequiredVersion != CurrentAppVersion || known.CurrentVersion != CurrentAppVersion {
		t.Fatalf("known payload versions = %+v, want %q", known, CurrentAppVersion)
	}
	if known.CurrentBuild != CurrentBuild {
		t.Fatalf("known payload build = %d, want %d", known.CurrentBuild, CurrentBuild)
	}
	for _, want := range []string{CurrentAppVersion, "build >="} {
		if got := known.Message; !contains(got, want) {
			t.Fatalf("known message = %q, want substring %q", got, want)
		}
	}
}

func TestCurrentSenderStampsAppVersion(t *testing.T) {
	sender := CurrentSender("android")
	if sender.AppBuild != CurrentBuild || sender.MinPeerBuild != CurrentMinPeerBuild {
		t.Fatalf("sender builds = %+v, want %d/%d", sender, CurrentBuild, CurrentMinPeerBuild)
	}
	if sender.AppVersion != CurrentAppVersion || sender.Platform != "android" {
		t.Fatalf("sender = %+v, want platform/version set", sender)
	}
	if got := AppVersionForBuild(CurrentBuild); got != CurrentAppVersion {
		t.Fatalf("AppVersionForBuild(current) = %q, want %q", got, CurrentAppVersion)
	}
	if got := AppVersionForBuild(9999); got != "" {
		t.Fatalf("AppVersionForBuild(unknown) = %q, want empty", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestIsCapabilitySupported(t *testing.T) {
	if !IsCapabilitySupported([]string{"ping"}, CapabilityPing) {
		t.Fatal("IsCapabilitySupported(ping) = false, want true")
	}
	if IsCapabilitySupported([]string{}, CapabilityPing) {
		t.Fatal("IsCapabilitySupported(empty) = true, want false")
	}
	if IsCapabilitySupported(nil, CapabilityPing) {
		t.Fatal("IsCapabilitySupported(nil) = true, want false")
	}
}

func zeros64() string { return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" }

func zeros32() string { return "0123456789abcdef0123456789abcdef" }
