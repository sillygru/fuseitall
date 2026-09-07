// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Discovery convention for FuseItAll LAN peers (mDNS service type shared by
// the Mac advertiser and the Android browser). Pure helpers only: no I/O,
// no UI — adapters own Bonjour/NSD wiring and call these for record shape.
const (
	// DiscoveryServiceType is the mDNS service advertised on the LAN.
	DiscoveryServiceType = "_fuseitall._tcp"
	// DiscoveryTXTVersion is the TXT record schema version (v=1 additive).
	DiscoveryTXTVersion = 1
	// MaxCandidateHosts caps remembered peer IPs (most-recent-first).
	MaxCandidateHosts = 4
)

// DiscoveryRecord is the TXT payload shape for a LAN advertisement.
// Fingerprint is the TLS cert hash (TOFU pin, public); Host/Port are the
// current LAN coordinates; Build gates via CheckPeerVersion.
type DiscoveryRecord struct {
	Fingerprint string
	Host        string
	Port        int
	Build       int
}

// BuildDiscoveryTXT renders the TXT record map for advertisement. Pure.
func BuildDiscoveryTXT(rec DiscoveryRecord) map[string]string {
	return map[string]string{
		"v":     fmt.Sprintf("%d", DiscoveryTXTVersion),
		"fp":    strings.ToLower(strings.TrimSpace(rec.Fingerprint)),
		"host":  strings.TrimSpace(rec.Host),
		"port":  fmt.Sprintf("%d", rec.Port),
		"build": fmt.Sprintf("%d", rec.Build),
	}
}

// ParseDiscoveryTXT parses a received TXT map. Returns ok=false on bad
// version or out-of-range port. Fingerprint may be empty (older peer).
// Pure: no I/O, no state.
func ParseDiscoveryTXT(m map[string]string) (DiscoveryRecord, bool) {
	if m["v"] != fmt.Sprintf("%d", DiscoveryTXTVersion) {
		return DiscoveryRecord{}, false
	}
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(m["port"]), "%d", &port); err != nil {
		return DiscoveryRecord{}, false
	}
	if port < 1 || port > 65535 {
		return DiscoveryRecord{}, false
	}
	var build int
	if _, err := fmt.Sscanf(strings.TrimSpace(m["build"]), "%d", &build); err != nil {
		build = 0
	}
	host := strings.TrimSpace(m["host"])
	if host == "" {
		return DiscoveryRecord{}, false
	}
	return DiscoveryRecord{
		Fingerprint: strings.ToLower(strings.TrimSpace(m["fp"])),
		Host:        host,
		Port:        port,
		Build:       build,
	}, true
}

// MergeCandidateHosts returns newest-first deduped hosts with latest first,
// capped at MaxCandidateHosts. Pure.
func MergeCandidateHosts(latest string, existing []string) []string {
	seen := map[string]bool{}
	out := []string{}
	push := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			return
		}
		if net.ParseIP(h) == nil {
			return
		}
		seen[h] = true
		out = append(out, h)
	}
	push(latest)
	for _, h := range existing {
		push(h)
	}
	if len(out) > MaxCandidateHosts {
		out = out[:MaxCandidateHosts]
	}
	return out
}

// OrderedPeerTargets returns dial order: primary first, then remembered
// candidates (deduped), each as host:port with the remembered port. Pure.
func OrderedPeerTargets(primaryHost string, primaryPort int, candidates []string) []string {
	type target struct {
		host string
	}
	seen := map[string]bool{}
	var hosts []target
	push := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" || seen[h] {
			return
		}
		if net.ParseIP(h) == nil {
			return
		}
		seen[h] = true
		hosts = append(hosts, target{host: h})
	}
	push(primaryHost)
	for _, h := range candidates {
		push(h)
	}
	sort.SliceStable(hosts, func(i, j int) bool {
		if hosts[i].host == strings.TrimSpace(primaryHost) {
			return true
		}
		if hosts[j].host == strings.TrimSpace(primaryHost) {
			return false
		}
		return i < j
	})
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, net.JoinHostPort(h.host, fmt.Sprintf("%d", primaryPort)))
	}
	return out
}
