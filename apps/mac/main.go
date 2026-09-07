// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"fuseitall/core"
	"fuseitall/mac/backend"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails embeds the built frontend into the binary; every file under
// frontend/dist is served to the webview. Run `pnpm build` in
// frontend/ (or `wails3 build`) before `go vet` / `go build`.

//go:embed all:frontend/dist
var assets embed.FS

// pairPort is the LAN port the core ping server listens on. Fixed for the
// BASE milestone so the QR payload always matches the listener.
const pairPort = 18789

func main() {
	logBuf := backend.NewLogBuffer(200)
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	identity, token, tlsCert, tlsFingerprint, err := backend.LoadOrCreatePairState(logger)
	if err != nil {
		logger.Error("load pair state", "err", err)
		os.Exit(1)
	}
	srv, err := core.NewServerWithCert(token, "macos", []string{
		core.CapabilityPing,
		core.CapabilityNotifications,
		core.CapabilityClipboard,
		core.CapabilitySettingsSync,
	}, logger, tlsCert, tlsFingerprint)
	if err != nil {
		logger.Error("create pair server", "err", err)
		os.Exit(1)
	}
	host := lanHost(logger)
	pair := core.MakePairPayload(deviceName(logger), "macos", host, pairPort, srv.CertFingerprint(), identity.PublicKey, token)
	raw, err := core.EncodePairQR(pair)
	if err != nil {
		logger.Error("encode pair qr", "err", err)
		os.Exit(1)
	}
	svc := backend.NewService(string(raw), srv.CertFingerprint(), token, logBuf)
	svc.ConfigurePairing(pair.DeviceName, pair.Platform, pair.Host, pair.Port, pair.Fingerprint, pair.PubKey)

	go func() {
		// Serve through the backend wrapper so accepted phone pings teach
		// the service the return path (peer host + reply_port).
		if err := backend.ServePairServer(svc, srv, fmt.Sprintf(":%d", pairPort)); err != nil {
			logger.Error("pair server exited", "err", err)
			os.Exit(1)
		}
	}()
	logger.Info("pair server listening", "host", host, "port", pairPort)

	go func() {
		// Auto-reconnect heartbeat (mirrors the Android 20s heartbeat):
		// refreshes presence while paired, redials the remembered phone
		// while expired. Failures only land in the log via HeartbeatTick.
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			svc.HeartbeatTick()
		}
	}()
	logger.Info("auto-reconnect heartbeat started", "interval_s", 20)

	// Automatic clipboard sync: change-triggered pushes only (idle = one
	// local pbpaste per 3s, zero network). Stops with the app.
	stopClip := svc.StartClipboardWatcher(context.Background())
	defer stopClip()

	app := application.New(application.Options{
		Name:        "FuseItAll",
		Description: "Pair your Android phone with this Mac.",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "FuseItAll",
		Width:     980,
		Height:    620,
		MinWidth:  860,
		MinHeight: 540,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(236, 236, 236),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		logger.Error("run application", "err", err)
		os.Exit(1)
	}
}

// deviceName reports the hostname for the QR payload, falling back to "Mac".
func deviceName(logger *slog.Logger) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		logger.Warn("hostname lookup failed, using default", "err", err)
		return "Mac"
	}
	return name
}

// lanHost returns the best non-loopback IPv4 address so the phone can reach
// us on the LAN, falling back to loopback when no LAN is present. Hotspot and
// multi-NIC hosts have several candidates (Wi-Fi, bridge, VPN, utun); the
// first InterfaceAddrs entry is unordered, so rank RFC1918 private ranges
// (192.168/16, 172.16/12, 10/8) above link-local/CG NAT and log every
// candidate. The QR carries only one host, so a wrong pick strands the phone
// — the full candidate list lands in the log for the activity feed.
func lanHost(logger *slog.Logger) string {
	candidates := lanCandidates(logger)
	if len(candidates) == 0 {
		return "127.0.0.1"
	}
	return candidates[0]
}

// lanCandidates lists usable IPv4 addresses ranked for phone reachability.
func lanCandidates(logger *slog.Logger) []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Warn("interface lookup failed, using loopback", "err", err)
		return nil
	}
	var found []string
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip := ipNet.IP.To4(); ip != nil {
			found = append(found, ip.String())
		}
	}
	// Rank: 192.168/16 (most home/hotspot nets) first, then 172.16/12
	// (phone hotspots often land here), then 10/8, then anything else.
	rank := func(ip string) int {
		switch {
		case len(ip) >= 8 && ip[:8] == "192.168.":
			return 0
		case len(ip) >= 4 && ip[:4] == "172.":
			return 1
		case len(ip) >= 3 && ip[:3] == "10.":
			return 2
		default:
			return 3
		}
	}
	for i := 1; i < len(found); i++ {
		for j := i; j > 0 && rank(found[j]) < rank(found[j-1]); j-- {
			found[j], found[j-1] = found[j-1], found[j]
		}
	}
	if len(found) > 0 {
		logger.Info("lan candidates", "hosts", found)
	}
	return found
}
