// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package main

import (
	"embed"
	"fmt"
	"log/slog"
	"net"
	"os"

	"fuseitall/core"
	"fuseitall/mac/backend"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails embeds the built frontend into the binary; every file under
// frontend/dist is served to the webview. Run `pnpm build` in
// frontend/ (or `wails3 build`) before `go vet` / `go build`.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIconBytes []byte

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
		core.CapabilityFiles,
		core.CapabilityPhotos,
		core.CapabilityPlayback,
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
	svc.StartClipboardWatcher()

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
		// Single LAN discovery beacon on startup so any already-open phone on
		// the local network discovers the Mac immediately and connects via
		// WebSocket. Phones opening later broadcast a probe that this listener
		// answers unicast — no repeat burst, zero polling.
		rec := core.DiscoveryRecord{
			V:           core.DiscoveryTXTVersion,
			Fingerprint: srv.CertFingerprint(),
			Host:        host,
			Port:        pairPort,
			Build:       core.CurrentBuild,
		}
		_ = core.BroadcastBeaconUDP(core.DefaultBeaconPort, rec)

		// Listen for UDP discovery probes from phones opening later, answering
		// with this Mac's coordinates so connections establish in < 15ms with zero polling.
		addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", core.DefaultBeaconPort))
		if err != nil {
			logger.Warn("resolve udp probe listener addr", "err", err)
			return
		}
		conn, err := net.ListenUDP("udp4", addr)
		if err != nil {
			logger.Warn("listen udp discovery probes", "err", err)
			return
		}
		defer func() { _ = conn.Close() }()

		beaconBytes, err := core.EncodeDiscoveryBeacon(rec)
		if err != nil {
			return
		}

		buf := make([]byte, 2048)
		for {
			n, remote, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			probe, err := core.ParseDiscoveryProbe(buf[:n])
			if err == nil && probe.Fingerprint == srv.CertFingerprint() {
				_, _ = conn.WriteTo(beaconBytes, remote)
				logger.Debug("answered discovery probe from phone", "remote", remote.String())
			}
		}
	}()
	logger.Info("lan discovery responder active", "port", core.DefaultBeaconPort)

	app := application.New(application.Options{
		Name:        "FuseItAll",
		Description: "Pair your Android phone with this Mac.",
		Icon:        appIconBytes,
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

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:          "FuseItAll",
		Width:          1200,
		Height:         760,
		MinWidth:       920,
		MinHeight:      600,
		EnableFileDrop: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		// Transparent so the translucent backdrop and frosted chrome show live
		// desktop blur at all times, not just before the frontend paints.
		// Content layers stay opaque in CSS; only the frost bars/sidebar float.
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	})

	win.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		files := event.Context().DroppedFiles()
		details := event.Context().DropTargetDetails()
		targetPath := ""
		if details != nil && details.Attributes != nil {
			targetPath = details.Attributes["data-drop-path"]
		}
		app.Event.Emit("files-dropped", map[string]any{
			"paths":      files,
			"targetPath": targetPath,
		})
	})

	// Wire Wails event push for clipboard live updates.
	backend.SetWailsEmitter(func(name string, data any) {
		_ = recover()
		if app != nil {
			app.Event.Emit(name, data)
		}
		_ = win
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
