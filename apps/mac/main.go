// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

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

func isDemoMode() bool {
	if os.Getenv("FUSEITALL_DEMO") == "1" || os.Getenv("FUSEITALL_DEMO") == "true" ||
		os.Getenv("DEMO") == "1" || os.Getenv("DEMO") == "true" {
		return true
	}
	for _, arg := range os.Args[1:] {
		if arg == "--demo" || arg == "-demo" {
			return true
		}
	}
	return false
}

func main() {
	logBuf := backend.NewLogBuffer(200)
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if isDemoMode() {
		logger.Info("demo mode: real data, disk state, and network connections disabled")
		svc := backend.NewDemoService(logBuf, logger)
		runApp(svc, logger)
		return
	}

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
		core.CapabilityContacts,
		core.CapabilityMessages,
	}, logger, tlsCert, tlsFingerprint)
	if err != nil {
		logger.Error("create pair server", "err", err)
		os.Exit(1)
	}
	candidates := lanCandidates(logger)
	host := "127.0.0.1"
	if len(candidates) > 0 {
		host = candidates[0]
	}
	candStr := strings.Join(candidates, ",")
	pair := core.MakePairPayload(deviceName(logger), "macos", host, pairPort, srv.CertFingerprint(), identity.PublicKey, token, candStr)
	raw, err := core.EncodePairQR(pair)
	if err != nil {
		logger.Error("encode pair qr", "err", err)
		os.Exit(1)
	}
	svc := backend.NewService(string(raw), srv.CertFingerprint(), token, logBuf)
	svc.ConfigurePairing(pair.DeviceName, pair.Platform, pair.Host, pair.Port, pair.Fingerprint, pair.PubKey, candStr)
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
				respHost := host
				if udpRemote, ok := remote.(*net.UDPAddr); ok {
					if routeConn, rErr := net.DialUDP("udp4", nil, udpRemote); rErr == nil {
						if localAddr, ok := routeConn.LocalAddr().(*net.UDPAddr); ok && !localAddr.IP.IsLoopback() && !localAddr.IP.IsUnspecified() {
							respHost = localAddr.IP.String()
						}
						_ = routeConn.Close()
					}
				}
				respRec := rec
				respRec.Host = respHost
				if respBytes, bErr := core.EncodeDiscoveryBeacon(respRec); bErr == nil {
					_, _ = conn.WriteTo(respBytes, remote)
				} else {
					_, _ = conn.WriteTo(beaconBytes, remote)
				}
				logger.Debug("answered discovery probe from phone", "remote", remote.String(), "host", respHost)
			}
		}
	}()
	logger.Info("lan discovery responder active", "port", core.DefaultBeaconPort)

	// Returning from a demo session (or any restart) must heal without a
	// fresh QR scan: redial the remembered phone with bounded backoff.
	// One-shot retries on this startup event only — no ticker, no polling.
	// Phone-initiated inbound (beacon probe answer + WS dial) heals the
	// other direction in parallel; whichever lands first wins.
	if dev := svc.GetLastDevice(); dev.HasDevice {
		go func() {
			delays := []time.Duration{0, 2 * time.Second, 6 * time.Second, 15 * time.Second}
			for i, d := range delays {
				if d > 0 {
					time.Sleep(d)
				}
				if svc.IsPaired() {
					return
				}
				msg, err := svc.ReconnectToLastDevice()
				if err == nil {
					logger.Info("auto-reconnect to phone ok", "attempt", i+1, "msg", msg)
					return
				}
				logger.Info("auto-reconnect attempt failed", "attempt", i+1, "err", err)
			}
		}()
	}

	runApp(svc, logger)
}

func runApp(svc *backend.Service, logger *slog.Logger) {
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
		Height:         800,
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
// It enumerates interfaces (up, non-loopback, non-virtual) so VPN/Docker/
// bridge addresses never outrank Wi-Fi, and puts the default-route source IP
// first so the QR primary is the address that actually routes to the LAN.
// The full list rides in the QR `candidates` field, so a wrong primary still
// heals via fallback — but the primary should already be right.
func lanCandidates(logger *slog.Logger) []string {
	defIP := defaultRouteIP(logger)
	ifaces, err := net.Interfaces()
	if err != nil {
		logger.Warn("interface lookup failed, using loopback", "err", err)
		return nil
	}
	var found []string
	seen := map[string]bool{}
	push := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			return
		}
		if parsed := net.ParseIP(ip); parsed == nil || parsed.IsLoopback() {
			return
		}
		seen[ip] = true
		found = append(found, ip)
	}
	// Default-route source first: this is the NIC that reaches the internet,
	// i.e. the Wi-Fi/LAN the phone is almost certainly on.
	if defIP != "" {
		push(defIP)
	}
	var ranked []namedIP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			if ip := ipNet.IP.To4(); ip != nil {
				s := ip.String()
				if seen[s] {
					continue
				}
				seen[s] = true
				ranked = append(ranked, namedIP{ip: s, iface: iface.Name})
			}
		}
	}
	for i := 1; i < len(ranked); i++ {
		for j := i; j > 0 && lanRankLess(ranked[j], ranked[j-1]); j-- {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
		}
	}
	for _, r := range ranked {
		found = append(found, r.ip)
	}
	if len(found) > 0 {
		logger.Info("lan candidates", "hosts", found)
	}
	return found
}

// defaultRouteIP returns the source IPv4 that the default route would use,
// by dialing UDP toward a public address (no packets are sent). Empty when
// offline or on error — callers fall back to interface enumeration.
func defaultRouteIP(logger *slog.Logger) string {
	_ = logger
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer func() { _ = conn.Close() }()
	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr.IP == nil {
		return ""
	}
	if ip := udpAddr.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
		return ip.String()
	}
	return ""
}

// isVirtualInterface reports Mac virtual/tunnel interfaces whose addresses
// must never outrank real LAN NICs (VPN, tunnels, bridges, VMs, AWDL).
func isVirtualInterface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"utun", "awdl", "llw", "bridge", "vbox", "vmnet", "docker", "veth", "tailscale", "ham", "gif", "stf", "anpi", "xhc", "thunderbolt"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// rankLanIP orders RFC1918 private ranges for phone reachability:
// 192.168/16 first, then 172.16/12, then 10/8, then anything else.
// 172.x outside 16-31 is NOT private (e.g. 172.5.x) and sorts last.
func rankLanIP(ip string) int {
	parsed := net.ParseIP(strings.TrimSpace(ip)).To4()
	if parsed == nil {
		return 3
	}
	if parsed[0] == 192 && parsed[1] == 168 {
		return 0
	}
	if parsed[0] == 172 && parsed[1] >= 16 && parsed[1] <= 31 {
		return 1
	}
	if parsed[0] == 10 {
		return 2
	}
	return 3
}

type namedIP struct {
	ip    string
	iface string
}

// lanRankLess prefers lower rankLanIP, breaking ties toward en0/en1 (Wi-Fi)
// so multi-NIC hosts deterministically pick wireless over wired dongles.
func lanRankLess(a, b namedIP) bool {
	ra, rb := rankLanIP(a.ip), rankLanIP(b.ip)
	if ra != rb {
		return ra < rb
	}
	pa, pb := ifacePriority(a.iface), ifacePriority(b.iface)
	if pa != pb {
		return pa < pb
	}
	return a.ip < b.ip
}

func ifacePriority(name string) int {
	n := strings.ToLower(name)
	switch {
	case n == "en0":
		return 0
	case n == "en1":
		return 1
	case strings.HasPrefix(n, "en"):
		return 2
	case strings.HasPrefix(n, "eth"), strings.HasPrefix(n, "wl"), strings.HasPrefix(n, "wifi"):
		return 3
	default:
		return 4
	}
}
