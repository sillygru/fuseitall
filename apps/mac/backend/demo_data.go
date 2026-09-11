// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"encoding/base64"
	"fmt"
	"time"

	"fuseitall/core"
)

// demoSVGThumb generates a crisp vector thumbnail with a modern gradient,
// subtle geometric landscape, and video overlay if applicable.
func demoSVGThumb(title, bgStart, bgEnd string, isVideo bool, duration string) string {
	playIcon := ""
	if isVideo {
		playIcon = fmt.Sprintf(`
		<circle cx="128" cy="128" r="32" fill="rgba(0,0,0,0.5)" />
		<polygon points="120,114 144,128 120,142" fill="#ffffff" />
		<rect x="180" y="218" width="64" height="26" rx="6" fill="rgba(0,0,0,0.65)" />
		<text x="212" y="235" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" font-size="12" font-weight="600" fill="#ffffff" text-anchor="middle">%s</text>
		`, duration)
	}

	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256">
  <defs>
    <linearGradient id="grad-%s" x1="0%%" y1="0%%" x2="100%%" y2="100%%">
      <stop offset="0%%" stop-color="%s" />
      <stop offset="100%%" stop-color="%s" />
    </linearGradient>
  </defs>
  <rect width="256" height="256" fill="url(#grad-%s)" />
  <circle cx="190" cy="70" r="28" fill="rgba(255,255,255,0.2)" />
  <path d="M0 200 L70 140 L130 180 L190 120 L256 170 L256 256 L0 256 Z" fill="rgba(255,255,255,0.15)" />
  <path d="M0 220 L90 170 L150 200 L210 160 L256 195 L256 256 L0 256 Z" fill="rgba(0,0,0,0.15)" />
  <text x="18" y="36" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" font-size="14" font-weight="600" fill="rgba(255,255,255,0.9)">%s</text>
  %s
</svg>`, title, bgStart, bgEnd, title, title, playIcon)

	return base64.StdEncoding.EncodeToString([]byte(svg))
}



func defaultDemoFiles() map[string][]FileEntryView {
	now := time.Now().Unix()
	day := int64(86400)
	hour := int64(3600)

	return map[string][]FileEntryView{
		"": {
			{Name: "Documents", Path: "Documents", IsDir: true, Size: 0, ModTime: now - 2*day},
			{Name: "Downloads", Path: "Downloads", IsDir: true, Size: 0, ModTime: now - 1*day},
			{Name: "Pictures", Path: "Pictures", IsDir: true, Size: 0, ModTime: now - 3*day},
			{Name: "DCIM", Path: "DCIM", IsDir: true, Size: 0, ModTime: now - 4*hour},
			{Name: "Music", Path: "Music", IsDir: true, Size: 0, ModTime: now - 5*day},
			{Name: "Movies", Path: "Movies", IsDir: true, Size: 0, ModTime: now - 7*day},
		},
		"Documents": {
			{Name: "Project Brief.pdf", Path: "Documents/Project Brief.pdf", IsDir: false, Size: 1245820, ModTime: now - 2*day, Mime: "application/pdf"},
			{Name: "Quarterly Report.xlsx", Path: "Documents/Quarterly Report.xlsx", IsDir: false, Size: 491520, ModTime: now - 5*day, Mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
			{Name: "Architecture Spec.md", Path: "Documents/Architecture Spec.md", IsDir: false, Size: 32410, ModTime: now - 1*day, Mime: "text/markdown"},
			{Name: "Meeting Notes.txt", Path: "Documents/Meeting Notes.txt", IsDir: false, Size: 12800, ModTime: now - 3*hour, Mime: "text/plain"},
		},
		"Downloads": {
			{Name: "fuseitall-android-v0.2.0.apk", Path: "Downloads/fuseitall-android-v0.2.0.apk", IsDir: false, Size: 18450200, ModTime: now - 1*day, Mime: "application/vnd.android.package-archive"},
			{Name: "design_assets.zip", Path: "Downloads/design_assets.zip", IsDir: false, Size: 42100500, ModTime: now - 3*day, Mime: "application/zip"},
			{Name: "contract_signed.pdf", Path: "Downloads/contract_signed.pdf", IsDir: false, Size: 840120, ModTime: now - 4*day, Mime: "application/pdf"},
		},
		"Pictures": {
			{Name: "Wallpapers", Path: "Pictures/Wallpapers", IsDir: true, Size: 0, ModTime: now - 6*day},
			{Name: "diagram_flow.png", Path: "Pictures/diagram_flow.png", IsDir: false, Size: 840000, ModTime: now - 2*day, Mime: "image/png"},
			{Name: "mockup_screen.png", Path: "Pictures/mockup_screen.png", IsDir: false, Size: 1250000, ModTime: now - 1*day, Mime: "image/png"},
		},
		"Pictures/Wallpapers": {
			{Name: "gradient_minimal.png", Path: "Pictures/Wallpapers/gradient_minimal.png", IsDir: false, Size: 3100000, ModTime: now - 10*day, Mime: "image/png"},
			{Name: "mountain_sunset.jpg", Path: "Pictures/Wallpapers/mountain_sunset.jpg", IsDir: false, Size: 4500000, ModTime: now - 12*day, Mime: "image/jpeg"},
		},
		"Music": {
			{Name: "Midnight City.flac", Path: "Music/Midnight City.flac", IsDir: false, Size: 28500000, ModTime: now - 15*day, Mime: "audio/flac"},
			{Name: "Resonance.mp3", Path: "Music/Resonance.mp3", IsDir: false, Size: 8900000, ModTime: now - 20*day, Mime: "audio/mpeg"},
			{Name: "Sunset Drive.mp3", Path: "Music/Sunset Drive.mp3", IsDir: false, Size: 6700000, ModTime: now - 8*day, Mime: "audio/mpeg"},
		},
		"Movies": {
			{Name: "Product Demo 2026.mp4", Path: "Movies/Product Demo 2026.mp4", IsDir: false, Size: 52400000, ModTime: now - 4*day, Mime: "video/mp4"},
		},
		"DCIM": {
			{Name: "Camera", Path: "DCIM/Camera", IsDir: true, Size: 0, ModTime: now - 4*hour},
		},
		"DCIM/Camera": {
			{Name: "IMG_20260911_120410.jpg", Path: "DCIM/Camera/IMG_20260911_120410.jpg", IsDir: false, Size: 3400000, ModTime: now - 4*hour, Mime: "image/jpeg"},
			{Name: "IMG_20260910_184205.jpg", Path: "DCIM/Camera/IMG_20260910_184205.jpg", IsDir: false, Size: 4100000, ModTime: now - 22*hour, Mime: "image/jpeg"},
			{Name: "VID_20260908_153012.mp4", Path: "DCIM/Camera/VID_20260908_153012.mp4", IsDir: false, Size: 24800000, ModTime: now - 3*day, Mime: "video/mp4"},
		},
	}
}

func defaultDemoPhotos() []PhotoEntryView {
	nowMs := time.Now().UnixMilli()
	hourMs := int64(3600000)
	dayMs := int64(86400000)

	return []PhotoEntryView{
		// Today
		{PhotoID: "p01", TakenAt: nowMs - 2*hourMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3420000, MediaType: "photo"},
		{PhotoID: "p02", TakenAt: nowMs - 4*hourMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4150000, MediaType: "photo"},
		{PhotoID: "v01", TakenAt: nowMs - 6*hourMs, Width: 3840, Height: 2160, Mime: "video/mp4", Size: 24800000, MediaType: "video", DurationMs: 42000},
		// Yesterday
		{PhotoID: "p03", TakenAt: nowMs - 25*hourMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3890000, MediaType: "photo"},
		{PhotoID: "p04", TakenAt: nowMs - 28*hourMs, Width: 1080, Height: 2400, Mime: "image/png", Size: 1250000, MediaType: "photo"},
		{PhotoID: "p05", TakenAt: nowMs - 31*hourMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4200000, MediaType: "photo"},
		// Earlier this week
		{PhotoID: "p06", TakenAt: nowMs - 3*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3600000, MediaType: "photo"},
		{PhotoID: "v02", TakenAt: nowMs - 3*dayMs - 4*hourMs, Width: 1920, Height: 1080, Mime: "video/mp4", Size: 45000000, MediaType: "video", DurationMs: 75000},
		{PhotoID: "p07", TakenAt: nowMs - 4*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3100000, MediaType: "photo"},
		{PhotoID: "p08", TakenAt: nowMs - 5*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4400000, MediaType: "photo"},
		{PhotoID: "p09", TakenAt: nowMs - 6*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 2900000, MediaType: "photo"},
		// Last week
		{PhotoID: "p10", TakenAt: nowMs - 8*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3750000, MediaType: "photo"},
		{PhotoID: "v03", TakenAt: nowMs - 9*dayMs, Width: 3840, Height: 2160, Mime: "video/mp4", Size: 38000000, MediaType: "video", DurationMs: 58000},
		{PhotoID: "p11", TakenAt: nowMs - 11*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4800000, MediaType: "photo"},
		{PhotoID: "p12", TakenAt: nowMs - 13*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3950000, MediaType: "photo"},
		// Two weeks ago
		{PhotoID: "p13", TakenAt: nowMs - 15*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 5200000, MediaType: "photo"},
		{PhotoID: "p14", TakenAt: nowMs - 18*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3150000, MediaType: "photo"},
		// Three weeks ago
		{PhotoID: "p15", TakenAt: nowMs - 21*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4100000, MediaType: "photo"},
		{PhotoID: "v04", TakenAt: nowMs - 23*dayMs, Width: 1920, Height: 1080, Mime: "video/mp4", Size: 52000000, MediaType: "video", DurationMs: 94000},
		{PhotoID: "p16", TakenAt: nowMs - 27*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4600000, MediaType: "photo"},
		// Last month
		{PhotoID: "p17", TakenAt: nowMs - 35*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3300000, MediaType: "photo"},
		{PhotoID: "p18", TakenAt: nowMs - 38*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4900000, MediaType: "photo"},
		{PhotoID: "v05", TakenAt: nowMs - 42*dayMs, Width: 3840, Height: 2160, Mime: "video/mp4", Size: 41000000, MediaType: "video", DurationMs: 62000},
		{PhotoID: "p19", TakenAt: nowMs - 46*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3850000, MediaType: "photo"},
		// Two months ago
		{PhotoID: "p20", TakenAt: nowMs - 65*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 5600000, MediaType: "photo"},
		{PhotoID: "p21", TakenAt: nowMs - 72*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4300000, MediaType: "photo"},
		// Older months (reused thumbnail assets under new IDs)
		{PhotoID: "p22", TakenAt: nowMs - 80*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3650000, MediaType: "photo"},
		{PhotoID: "p23", TakenAt: nowMs - 88*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4450000, MediaType: "photo"},
		{PhotoID: "v06", TakenAt: nowMs - 95*dayMs, Width: 3840, Height: 2160, Mime: "video/mp4", Size: 36000000, MediaType: "video", DurationMs: 51000},
		{PhotoID: "p24", TakenAt: nowMs - 103*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3980000, MediaType: "photo"},
		{PhotoID: "p25", TakenAt: nowMs - 111*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3250000, MediaType: "photo"},
		{PhotoID: "p26", TakenAt: nowMs - 119*dayMs, Width: 1080, Height: 2400, Mime: "image/png", Size: 1420000, MediaType: "photo"},
		{PhotoID: "p27", TakenAt: nowMs - 128*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4700000, MediaType: "photo"},
		{PhotoID: "p28", TakenAt: nowMs - 136*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3550000, MediaType: "photo"},
		{PhotoID: "p29", TakenAt: nowMs - 145*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4250000, MediaType: "photo"},
		{PhotoID: "v07", TakenAt: nowMs - 153*dayMs, Width: 1920, Height: 1080, Mime: "video/mp4", Size: 48000000, MediaType: "video", DurationMs: 83000},
		{PhotoID: "p30", TakenAt: nowMs - 161*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3900000, MediaType: "photo"},
		{PhotoID: "p31", TakenAt: nowMs - 170*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3050000, MediaType: "photo"},
		{PhotoID: "p32", TakenAt: nowMs - 178*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 5100000, MediaType: "photo"},
		{PhotoID: "p33", TakenAt: nowMs - 187*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 4350000, MediaType: "photo"},
		{PhotoID: "p34", TakenAt: nowMs - 195*dayMs, Width: 1080, Height: 2400, Mime: "image/png", Size: 1180000, MediaType: "photo"},
		{PhotoID: "p35", TakenAt: nowMs - 204*dayMs, Width: 4032, Height: 3024, Mime: "image/jpeg", Size: 3700000, MediaType: "photo"},
	}
}

func defaultDemoPhotoThumb(id string) (string, string) {
	return "image/jpeg", demoPhotoThumbB64(id)
}

func defaultDemoContacts() []core.ContactEntry {
	return append([]core.ContactEntry{
		{
			ContactID:    "c01",
			DisplayName:  "Alex Rivera",
			Starred:      true,
			AvatarB64:    demoContactAvatarB64("c01"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 014-2849", Type: "Mobile", IsPrimary: true, Normalized: "+15550142849"},
				{Number: "+1 (555) 014-2850", Type: "Work"},
			},
			Emails: []core.ContactEmail{
				{Address: "alex.rivera@example.com", Type: "Work"},
			},
			Organization: &core.ContactOrganization{
				Company: "Acme Design Studio",
				Title:   "Lead Product Designer",
			},
			Note: "Met at Design Systems 2025 conference.",
		},
		{
			ContactID:    "c02",
			DisplayName:  "Maya Lin",
			Starred:      true,
			AvatarB64:    demoContactAvatarB64("c02"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 019-8371", Type: "Mobile", IsPrimary: true, Normalized: "+15550198371"},
			},
			Emails: []core.ContactEmail{
				{Address: "maya.lin@example.org", Type: "Work"},
			},
			Organization: &core.ContactOrganization{
				Company: "Horizon Labs",
				Title:   "Engineering Manager",
			},
		},
		{
			ContactID:    "c03",
			DisplayName:  "David Miller",
			Starred:      false,
			AvatarB64:    demoContactAvatarB64("c03"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 017-6420", Type: "Mobile", IsPrimary: true, Normalized: "+15550176420"},
			},
			Emails: []core.ContactEmail{
				{Address: "david.m@example.com", Type: "Work"},
			},
			Organization: &core.ContactOrganization{
				Company: "TechCorp Global",
				Title:   "Staff Software Engineer",
			},
		},
		{
			ContactID:    "c04",
			DisplayName:  "Sarah Chen",
			Starred:      false,
			AvatarB64:    demoContactAvatarB64("c04"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 018-5192", Type: "Mobile", IsPrimary: true, Normalized: "+15550185192"},
			},
			Emails: []core.ContactEmail{
				{Address: "sarah.chen@example.com", Type: "Personal"},
			},
			Organization: &core.ContactOrganization{
				Company: "CloudFlow Systems",
				Title:   "DevOps Architect",
			},
		},
		{
			ContactID:    "c05",
			DisplayName:  "Priya Patel",
			Starred:      true,
			AvatarB64:    demoContactAvatarB64("c05"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 012-3490", Type: "Mobile", IsPrimary: true, Normalized: "+15550123490"},
			},
			Emails: []core.ContactEmail{
				{Address: "priya.p@example.net", Type: "Work"},
			},
			Organization: &core.ContactOrganization{
				Company: "Global Innovations",
				Title:   "Director of Research",
			},
		},
		{
			ContactID:    "c06",
			DisplayName:  "Marcus Vance",
			Starred:      false,
			AvatarB64:    demoContactAvatarB64("c06"),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: "+1 (555) 016-4731", Type: "Mobile", IsPrimary: true, Normalized: "+15550164731"},
			},
			Emails: []core.ContactEmail{
				{Address: "marcus.v@example.com", Type: "Work"},
			},
			Organization: &core.ContactOrganization{
				Company: "NorthStar Systems",
				Title:   "Solutions Architect",
			},
		},
	}, extraDemoContacts()...)
}

func defaultDemoThreads() []core.SMSThread {
	nowMs := time.Now().UnixMilli()
	minute := int64(60000)
	hour := int64(3600000)

	return append([]core.SMSThread{
		{
			ThreadID:     1,
			Address:      "+1 (555) 014-2849",
			ContactName:  "Alex Rivera",
			ContactID:    "c01",
			PhotoVersion: "1",
			Snippet:      "Perfect, see you there at 2:30pm!",
			Date:         nowMs - 12*minute,
			MessageCount: 3,
			UnreadCount:  0,
			Read:         true,
		},
		{
			ThreadID:     2,
			Address:      "+1 (555) 019-8371",
			ContactName:  "Maya Lin",
			ContactID:    "c02",
			PhotoVersion: "1",
			Snippet:      "The updated design mocks look fantastic.",
			Date:         nowMs - 2*hour,
			MessageCount: 2,
			UnreadCount:  0,
			Read:         true,
		},
		{
			ThreadID:     3,
			Address:      "+1 (555) 010-0923",
			ContactName:  "Acme Logistics",
			Snippet:      "Your package #84920 has been delivered.",
			Date:         nowMs - 18*hour,
			MessageCount: 1,
			UnreadCount:  1,
			Read:         false,
		},
		{
			ThreadID:     4,
			Address:      "+1 (555) 017-6420",
			ContactName:  "David Miller",
			ContactID:    "c03",
			PhotoVersion: "1",
			Snippet:      "Locked in! 🏔️ Cabin weekend is officially happening.",
			Date:         nowMs - 28*hour,
			MessageCount: 10,
			UnreadCount:  0,
			Read:         true,
		},
	}, extraDemoThreads(nowMs)...)
}

func defaultDemoMessages() map[int64][]core.SMSMessage {
	nowMs := time.Now().UnixMilli()
	minute := int64(60000)
	hour := int64(3600000)

	base := map[int64][]core.SMSMessage{
		1: {
			{
				ID:          101,
				ThreadID:    1,
				Address:     "+1 (555) 014-2849",
				Body:        "Hey! Are you still free to grab coffee this afternoon?",
				Date:        nowMs - 25*minute,
				Type:        1, // incoming
				Read:        true,
				ContactName: "Alex Rivera",
			},
			{
				ID:          102,
				ThreadID:    1,
				Address:     "+1 (555) 014-2849",
				Body:        "Yes, absolutely! The cafe on 4th street?",
				Date:        nowMs - 18*minute,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "Alex Rivera",
			},
			{
				ID:          103,
				ThreadID:    1,
				Address:     "+1 (555) 014-2849",
				Body:        "Perfect, see you there at 2:30pm!",
				Date:        nowMs - 12*minute,
				Type:        1, // incoming
				Read:        true,
				ContactName: "Alex Rivera",
			},
		},
		2: {
			{
				ID:          201,
				ThreadID:    2,
				Address:     "+1 (555) 019-8371",
				Body:        "I reviewed the new macOS HIG layout guidelines.",
				Date:        nowMs - 3*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "Maya Lin",
			},
			{
				ID:          202,
				ThreadID:    2,
				Address:     "+1 (555) 019-8371",
				Body:        "The updated design mocks look fantastic. The team really liked the split view sidebar layout.",
				Date:        nowMs - 2*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "Maya Lin",
			},
		},
		3: {
			{
				ID:          301,
				ThreadID:    3,
				Address:     "+1 (555) 010-0923",
				Body:        "Your package #84920 has been delivered to your front door. Track at https://example.com/track/84920",
				Date:        nowMs - 18*hour,
				Type:        1, // incoming
				Read:        false,
				ContactName: "Acme Logistics",
			},
		},
		4: {
			{
				ID:          401,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Cabin weekend in October — are we actually doing this?",
				Date:        nowMs - 60*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          402,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "YES. I've already blocked the weekend. No backing out.",
				Date:        nowMs - 55*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          403,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Haha okay! The place by the lake with the hot tub?",
				Date:        nowMs - 50*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          404,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "That's the one. Sleeps 6, so it's us + Elena + Tom and Jen.",
				Date:        nowMs - 46*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          405,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "I'll bring the grill stuff and my speaker.",
				Date:        nowMs - 42*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          406,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "I'll handle breakfast supplies. Pancakes Saturday, obviously.",
				Date:        nowMs - 38*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          407,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Obviously. Should we hike Sunday or just be lazy?",
				Date:        nowMs - 34*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          408,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Both — short morning hike, then dock + hot tub all afternoon.",
				Date:        nowMs - 31*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          409,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Perfect plan. I'll send the deposit tonight.",
				Date:        nowMs - 29*hour,
				Type:        2, // outgoing
				Read:        true,
				ContactName: "David Miller",
			},
			{
				ID:          410,
				ThreadID:    4,
				Address:     "+1 (555) 017-6420",
				Body:        "Locked in! 🏔️ Cabin weekend is officially happening.",
				Date:        nowMs - 28*hour,
				Type:        1, // incoming
				Read:        true,
				ContactName: "David Miller",
			},
		},
	}
	for tid, msgs := range extraDemoMessages(nowMs) {
		base[tid] = msgs
	}
	return base
}

func defaultDemoNotifications() []NotifItem {
	now := time.Now().Unix()
	minute := int64(60)

	return append([]NotifItem{
		{
			ID:          "demo-n1",
			PackageName: "com.google.android.apps.messaging",
			App:         "Messages",
			Title:       "Alex Rivera",
			Text:        "See you at the coffee shop at 2:30pm!",
			PostedUnix:  now - 12*minute,
			IconB64:     demoAppIconPNG("Messages"),
		},
		{
			ID:          "demo-n2",
			PackageName: "com.slack",
			App:         "Slack",
			Title:       "#engineering",
			Text:        "Release v1.2.0 passed all automated CI tests and is ready for rollout.",
			PostedUnix:  now - 35*minute,
			IconB64:     demoAppIconPNG("Slack"),
		},
		{
			ID:          "demo-n3",
			PackageName: "com.google.android.calendar",
			App:         "Calendar",
			Title:       "Product Design Review",
			Text:        "Starting in 15 minutes · Design Studio Room 4B",
			PostedUnix:  now - 50*minute,
			IconB64:     demoAppIconPNG("Calendar"),
		},
		{
			ID:          "demo-n4",
			PackageName: "com.github.android",
			App:         "GitHub",
			Title:       "FuseItAll Repository",
			Text:        "PR #142 'Support demo mode for clean screenshots' merged into main",
			PostedUnix:  now - 75*minute,
			IconB64:     demoAppIconPNG("GitHub"),
		},
	}, extraDemoNotifications(now)...)
}

func defaultDemoKnownApps() []KnownNotifApp {
	return []KnownNotifApp{
		{PackageName: "com.google.android.apps.messaging", App: "Messages", Count: 34, IconB64: demoAppIconPNG("Messages")},
		{PackageName: "com.slack", App: "Slack", Count: 57, IconB64: demoAppIconPNG("Slack")},
		{PackageName: "com.google.android.calendar", App: "Calendar", Count: 12, IconB64: demoAppIconPNG("Calendar")},
		{PackageName: "com.github.android", App: "GitHub", Count: 21, IconB64: demoAppIconPNG("GitHub")},
		{PackageName: "com.spotify.music", App: "Spotify", Count: 16, IconB64: demoAppIconPNG("Spotify")},
		{PackageName: "com.whatsapp", App: "WhatsApp", Count: 42, IconB64: demoAppIconPNG("WhatsApp")},
		{PackageName: "com.google.android.gm", App: "Gmail", Count: 38, IconB64: demoAppIconPNG("Gmail")},
		{PackageName: "com.google.android.apps.photos", App: "Photos", Count: 7, IconB64: demoAppIconPNG("Photos")},
	}
}
