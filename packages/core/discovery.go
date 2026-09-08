// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package core

import (
	"encoding/json"
	"errors"
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
	// DefaultBeaconPort is the UDP port for LAN presence broadcasts.
	DefaultBeaconPort = 18790
)

// DiscoveryRecord is the payload shape for a LAN advertisement.
// Fingerprint is the TLS cert hash (TOFU pin, public); Host/Port are the
// current LAN coordinates; Build gates via CheckPeerVersion.
type DiscoveryRecord struct {
	V           int    `json:"v,omitempty"`
	Fingerprint string `json:"fp"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Build       int    `json:"build,omitempty"`
}

// EncodeDiscoveryBeacon renders the JSON datagram for UDP broadcast. Pure.
func EncodeDiscoveryBeacon(rec DiscoveryRecord) ([]byte, error) {
	if rec.V == 0 {
		rec.V = DiscoveryTXTVersion
	}
	rec.Fingerprint = strings.ToLower(strings.TrimSpace(rec.Fingerprint))
	rec.Host = strings.TrimSpace(rec.Host)
	data, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("marshal discovery beacon: %w", err)
	}
	return data, nil
}

// ParseDiscoveryBeacon parses a JSON discovery beacon from the wire.
// Pure: no I/O, no state.
func ParseDiscoveryBeacon(data []byte) (DiscoveryRecord, error) {
	var rec DiscoveryRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return DiscoveryRecord{}, fmt.Errorf("unmarshal discovery beacon: %w", err)
	}
	if rec.V == 0 {
		rec.V = DiscoveryTXTVersion
	}
	if rec.V > DiscoveryTXTVersion {
		return DiscoveryRecord{}, fmt.Errorf("unsupported beacon version %d: %w", rec.V, ErrUnsupportedProtocol)
	}
	if rec.Port < 1 || rec.Port > 65535 {
		return DiscoveryRecord{}, errors.New("beacon port out of range")
	}
	rec.Host = strings.TrimSpace(rec.Host)
	if rec.Host == "" {
		return DiscoveryRecord{}, errors.New("empty beacon host")
	}
	rec.Fingerprint = strings.ToLower(strings.TrimSpace(rec.Fingerprint))
	return rec, nil
}

// BroadcastBeaconUDP sends a UDP broadcast announcement to 255.255.255.255:port.
func BroadcastBeaconUDP(port int, rec DiscoveryRecord) error {
	data, err := EncodeDiscoveryBeacon(rec)
	if err != nil {
		return err
	}
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("255.255.255.255:%d", port))
	if err != nil {
		return fmt.Errorf("resolve broadcast addr: %w", err)
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dial broadcast udp: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write broadcast udp: %w", err)
	}
	return nil
}

// DiscoveryProbe is the datagram sent by an opening phone to locate its paired Mac.
// Fingerprint identifies the target Mac's TLS certificate (TOFU pin).
type DiscoveryProbe struct {
	V           int    `json:"v,omitempty"`
	Type        string `json:"type"`
	Fingerprint string `json:"fp"`
}

// EncodeDiscoveryProbe renders the JSON datagram for a UDP discovery probe. Pure.
func EncodeDiscoveryProbe(fp string) ([]byte, error) {
	probe := DiscoveryProbe{
		V:           DiscoveryTXTVersion,
		Type:        "probe",
		Fingerprint: strings.ToLower(strings.TrimSpace(fp)),
	}
	data, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshal discovery probe: %w", err)
	}
	return data, nil
}

// ParseDiscoveryProbe parses a JSON discovery probe from the wire.
// Pure: no I/O, no state.
func ParseDiscoveryProbe(data []byte) (DiscoveryProbe, error) {
	var probe DiscoveryProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return DiscoveryProbe{}, fmt.Errorf("unmarshal discovery probe: %w", err)
	}
	if probe.Type != "probe" {
		return DiscoveryProbe{}, errors.New("not a discovery probe")
	}
	probe.Fingerprint = strings.ToLower(strings.TrimSpace(probe.Fingerprint))
	if probe.Fingerprint == "" {
		return DiscoveryProbe{}, errors.New("empty probe fingerprint")
	}
	return probe, nil
}

// BroadcastProbeUDP sends a UDP discovery probe to 255.255.255.255:port.
func BroadcastProbeUDP(port int, targetFingerprint string) error {
	data, err := EncodeDiscoveryProbe(targetFingerprint)
	if err != nil {
		return err
	}
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("255.255.255.255:%d", port))
	if err != nil {
		return fmt.Errorf("resolve broadcast addr: %w", err)
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dial broadcast udp: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write broadcast probe udp: %w", err)
	}
	return nil
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
