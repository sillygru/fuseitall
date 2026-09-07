// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"strings"

	"fuseitall/core"
)

// notifyUserWithIcon posts a bundle-attributed system notification for a
// mirrored phone notification. It is the only path that shows the per-app
// context (title uses the phone's title, subtitle hints the source app).
// Failures are silent so mirroring never breaks presence.
func notifyUserWithIcon(p core.NotifPostPayload) {
	title := strings.TrimSpace(p.Title)
	body := strings.TrimSpace(p.Text)
	if title == "" && body == "" {
		// Still surface the app name when both are empty (e.g. media controls).
		title = p.App
		if strings.TrimSpace(title) == "" {
			title = p.PackageName
		}
		if title == "" {
			return
		}
		if body == "" {
			body = "New notification"
		}
	}
	// Prefer a crisp headline: show the source app as subtitle so the banner
	// groups by sender (FuseItAll) but still reads "WhatsApp — John").
	subtitle := ""
	if strings.TrimSpace(p.App) != "" && strings.TrimSpace(p.Title) != "" {
		subtitle = strings.TrimSpace(p.App)
	}
	if subtitle == "" && strings.TrimSpace(p.PackageName) != "" {
		// Derive a readable label from package when App is absent (e.g.
		// com.whatsapp → WhatsApp). Fallback keeps package verbatim.
		subtitle = readableLabel(p.PackageName)
	}
	if subtitle != "" {
		if body != "" && title != "" {
			// Classic macOS two-line banner: title on top, body below, subtitle
			// as second title. Use title as headline, move subtitle into body
			// prefix when platform API lacks subtitle field.
			title = title + " · " + subtitle
		} else if title == "" {
			title = subtitle
		}
	}
	notifyUserInternal(title, body, core.SanitizeNotifIconB64(p.IconB64))
}

// readableLabel turns com.whatsapp into Whatsapp-like fallback. Best-effort.
func readableLabel(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	// Take last segment after dot.
	if idx := strings.LastIndex(pkg, "."); idx >= 0 && idx+1 < len(pkg) {
		pkg = pkg[idx+1:]
	}
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	return pkg
}

// notifyUserInternal is implemented per GOOS (darwin vs other). See
// notifications_notify_darwin.go / notifications_notify_other.go.
