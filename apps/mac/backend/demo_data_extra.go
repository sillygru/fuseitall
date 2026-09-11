// SPDX-License-Identifier: AGPL-3.0-only

package backend

import (
	"fuseitall/core"
)

// Extra demo directory entries (c07+). Table-driven to stay compact: one row
// per contact, built into full core.ContactEntry values by extraDemoContacts.
// Avatars reuse the bundled c01..c06 portraits round-robin (see
// demoContactAvatarB64 fallback) so no new binary assets are needed.
type extraContactDef struct {
	id      string
	name    string
	starred bool
	phone   string
	ptype   string
	email   string
	etype   string
	company string
	title   string
	note    string
}

var extraContactTable = []extraContactDef{
	{"c07", "Elena Rodriguez", false, "+1 (555) 011-2233", "Mobile", "elena.r@example.com", "Personal", "Central Library", "Librarian", "Book club + pottery class."},
	{"c08", "James Park", false, "+1 (555) 013-7788", "Mobile", "james.park@example.com", "Personal", "St. Mary's Hospital", "ER Nurse", "Fantasy basketball league."},
	{"c09", "Aisha Khan", true, "+1 (555) 015-9901", "Mobile", "aisha.k@example.org", "Personal", "Greenfield Animal Clinic", "Veterinarian", "Has a golden retriever, Biscuit."},
	{"c10", "Tom Becker", false, "+1 (555) 016-2210", "Mobile", "tom.b@example.com", "Personal", "Lincoln High", "High-School Teacher", "Board-game nights host."},
	{"c11", "Sofia Marino", false, "+1 (555) 017-3345", "Mobile", "sofia.m@example.com", "Personal", "Bloom Studio", "Illustrator", ""},
	{"c12", "Daniel Okafor", false, "+1 (555) 018-5567", "Mobile", "daniel.o@example.com", "Personal", "Osteria Verde", "Chef", "Sourdough starter evangelist."},
	{"c13", "Grace Liu", true, "+1 (555) 019-1102", "Mobile", "grace.liu@example.com", "Work", "Horizon Labs", "Product Manager", "Launch sync every Friday."},
	{"c14", "Omar Haddad", false, "+1 (555) 012-8876", "Mobile", "omar.h@example.com", "Personal", "Crumb & Crust Bakery", "Bakery Owner", "Saves us day-old croissants."},
	{"c15", "Nina Kowalski", false, "+1 (555) 014-5566", "Mobile", "nina.k@example.org", "Personal", "Freelance", "Photographer", "Shot the cabin weekend album."},
	{"c16", "Leo Fischer", false, "+1 (555) 015-3344", "Mobile", "leo.f@example.com", "Personal", "SoundWave", "Music Producer", "Sends demos before release."},
	{"c17", "Hana Sato", false, "+1 (555) 016-9902", "Mobile", "hana.s@example.com", "Work", "Kyoto Design Office", "Architect", ""},
	{"c18", "Chris Novak", false, "+1 (555) 017-8811", "Mobile", "chris.n@example.com", "Personal", "City Fire Dept.", "Paramedic", "Softball team captain."},
	{"c19", "Fatima Alami", false, "+1 (555) 018-2239", "Mobile", "fatima.a@example.com", "Personal", "State University", "Biology PhD Student", ""},
	{"c20", "Ben Carter", false, "+1 (555) 019-4456", "Mobile", "ben.c@example.com", "Personal", "Hearth & Home Realty", "Realtor", "College roommate."},
	{"c21", "Julia Weber", false, "+1 (555) 010-5567", "Mobile", "julia.w@example.org", "Work", "Bloom Studio", "Copywriter", ""},
	{"c22", "Sam Delgado", false, "+1 (555) 011-8890", "Mobile", "sam.d@example.com", "Personal", "Riverside League", "Youth Soccer Coach", "Carpools to Saturday games."},
	{"c23", "Yuki Tanaka", false, "+1 (555) 012-1123", "Mobile", "yuki.t@example.com", "Personal", "SoundWave", "DJ", ""},
	{"c24", "Emma Wilson", true, "+1 (555) 013-4455", "Mobile", "emma.w@example.com", "Personal", "", "", "Sister — call on Sundays."},
}

func extraDemoContacts() []core.ContactEntry {
	out := make([]core.ContactEntry, 0, len(extraContactTable))
	for _, d := range extraContactTable {
		e := core.ContactEntry{
			ContactID:    d.id,
			DisplayName:  d.name,
			Starred:      d.starred,
			AvatarB64:    demoContactAvatarB64(d.id),
			PhotoVersion: "1",
			Phones: []core.ContactPhone{
				{Number: d.phone, Type: d.ptype, IsPrimary: true, Normalized: core.NormalizePhone(d.phone)},
			},
		}
		if d.email != "" {
			e.Emails = []core.ContactEmail{{Address: d.email, Type: d.etype}}
		}
		if d.company != "" || d.title != "" {
			e.Organization = &core.ContactOrganization{Company: d.company, Title: d.title}
		}
		if d.note != "" {
			e.Note = d.note
		}
		out = append(out, e)
	}
	return out
}

// Extra conversation threads (IDs 5..14). aggMin = minutes before now for the
// latest message; snippet must match the newest body in extraDemoMessages.
type extraThreadDef struct {
	id      int64
	addr    string
	name    string
	cid     string
	snippet string
	agoMin  int64
	count   int
	unread  int
}

var extraThreadTable = []extraThreadDef{
	{5, "+1 (555) 012-3490", "Priya Patel", "c05", "The methods section needs one more paragraph.", 155, 8, 2},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "c07", "Will do. See you Thursday at 7:30! 🥂", 260, 10, 0},
	{7, "+1 (555) 019-1102", "Grace Liu", "c13", "Launch checklist is green except legal sign-off.", 320, 7, 0},
	{8, "+1 (555) 015-9901", "Aisha Khan", "c09", "Oh and bring the bug spray — the mosquitoes by the lake are ruthless this year.", 430, 12, 1},
	{9, "+1 (555) 010-7745", "Delta Airlines", "", "DL 4821 boards at 6:40pm from Gate B12.", 560, 3, 1},
	{10, "+1 (555) 016-2210", "Tom Becker", "c10", "Bringing Codenames + the new expansion pack.", 1500, 10, 0},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "c15", "Gallery proofs are ready — 24 shots to review!", 2100, 10, 0},
	{12, "+1 (555) 011-8890", "Sam Delgado", "c22", "Actually I'll just grab both — see you Saturday! 🎉", 2900, 12, 0},
	{13, "+1 (555) 010-3321", "Chase Alerts", "", "A $184.20 purchase at Green Grocers was approved.", 3900, 2, 1},
	{14, "+1 (555) 013-4455", "Emma Wilson", "c24", "Mom says bring the potato salad on Sunday!", 5200, 11, 0},
}

func extraDemoThreads(nowMs int64) []core.SMSThread {
	out := make([]core.SMSThread, 0, len(extraThreadTable))
	for _, d := range extraThreadTable {
		t := core.SMSThread{
			ThreadID:     d.id,
			Address:      d.addr,
			ContactName:  d.name,
			Snippet:      d.snippet,
			Date:         nowMs - d.agoMin*60000,
			MessageCount: d.count,
			UnreadCount:  d.unread,
			Read:         d.unread == 0,
		}
		if d.cid != "" {
			t.ContactID = d.cid
			t.PhotoVersion = "1"
		}
		out = append(out, t)
	}
	return out
}

// Compact message rows: one line per bubble. out=false means incoming (type 1).
type extraMsgDef struct {
	thread  int64
	addr    string
	contact string
	body    string
	agoMin  int64
	out     bool
	read    bool
}

var extraMsgTable = []extraMsgDef{
	// 5 — Priya Patel, paper revisions
	{5, "+1 (555) 012-3490", "Priya Patel", "Coffee's on me next time — you've saved this paper twice now.", 420, true, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "Deal. Only if it's the good place, not the burnt-beans one. ☕", 360, false, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "Draft v3 is in the shared folder — results section rewritten.", 300, false, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "Reading now, the new chart finally makes the trend obvious.", 280, true, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "Reviewer 2 asked for ablations on the smaller cohort.", 240, false, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "I can rerun those overnight, cluster is free after 10pm.", 200, true, true},
	{5, "+1 (555) 012-3490", "Priya Patel", "Tables updated — can you sanity-check the p-values?", 170, false, false},
	{5, "+1 (555) 012-3490", "Priya Patel", "The methods section needs one more paragraph.", 155, false, false},
	// 6 — Elena Rodriguez, birthday dinner scouting
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Okay I need your opinion — birthday dinner for mom, our usual spot or somewhere new?", 560, true, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Somewhere new! She'd love an adventure. What did you have in mind?", 520, false, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Have you tried that new place on 5th? The photos look amazing.", 480, false, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Not yet — want to scope it out Thursday? Dinner, my treat.", 440, true, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Yes! Booked us at 7:30, they held the corner table.", 400, false, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Should I invite Marco and Jen too?", 360, true, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Let's keep it small this time — catch up properly?", 330, false, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Agreed, just us. I'll bring the birthday card for us both to sign.", 300, true, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Perfect — and ask about the patio for mom's party?", 280, false, true},
	{6, "+1 (555) 011-2233", "Elena Rodriguez", "Will do. See you Thursday at 7:30! 🥂", 260, true, true},
	// 7 — Grace Liu, launch sync
	{7, "+1 (555) 019-1102", "Grace Liu", "Heads up: launch review moved to Friday 10am.", 700, false, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "Noted — I'll have the metrics dashboard ready.", 620, true, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "Marketing signed off on the announcement copy.", 540, false, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "Staging deploy passed QA, rolling to 10% now.", 470, true, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "Crash-free rate is 99.8% on the rollout cohort.", 400, false, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "One blocker: need your approval on the changelog.", 350, false, true},
	{7, "+1 (555) 019-1102", "Grace Liu", "Launch checklist is green except legal sign-off.", 320, false, true},
	// 8 — Aisha Khan, Biscuit the dog + Saturday hike
	{8, "+1 (555) 015-9901", "Aisha Khan", "Biscuit says hi! 🐶 He found your old tennis ball under the couch.", 1500, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Haha tell him it's his now. How's the big guy doing?", 1400, true, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Spoiled as ever. Vet says he's in perfect shape, down 2 pounds!", 1200, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "The diet worked! No more sneaky treats from the neighbors?", 1100, true, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Well... Mrs. Chen still slips him biscuits. I've given up policing that.", 1000, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "That's basically his second home at this point 😄", 900, true, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Hey, are you free Saturday morning? Thinking of the lake trail with him.", 800, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Yes! What time? I can bring coffee and those cheese rolls.", 750, true, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Meet at the trailhead at 8? Before it gets hot. It's like 6 miles round trip.", 700, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "8 works. I'll pack extra water — for Biscuit, mostly.", 640, true, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "He'll carry his own bowl, don't worry. He insists. 🦮", 560, false, true},
	{8, "+1 (555) 015-9901", "Aisha Khan", "Oh and bring the bug spray — the mosquitoes by the lake are ruthless this year.", 430, false, false},
	// 9 — Delta Airlines, travel
	{9, "+1 (555) 010-7745", "Delta Airlines", "Your trip to DEN is in 3 days. Check in opens tomorrow at 6:40pm.", 900, false, true},
	{9, "+1 (555) 010-7745", "Delta Airlines", "Upgrade cleared! You're now in seat 4A.", 700, false, true},
	{9, "+1 (555) 010-7745", "Delta Airlines", "DL 4821 boards at 6:40pm from Gate B12.", 560, false, false},
	// 10 — Tom Becker, game night
	{10, "+1 (555) 016-2210", "Tom Becker", "Game night Saturday — you in?", 2400, false, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Count me in. What should I bring?", 2300, true, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Just snacks, we have everything else.", 2200, false, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "I'll bring the pretzels + that spicy dip.", 2100, true, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "The kids have been asking about you, by the way.", 2000, false, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Tell them I'll bring the card game they like — the llama one.", 1900, true, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "They'll lose their minds. 😄 What time should we come?", 1800, false, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Doors at 6, first game at 6:30 sharp. Tom rules.", 1700, true, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Noted — no late starts this time, I promise.", 1600, false, true},
	{10, "+1 (555) 016-2210", "Tom Becker", "Bringing Codenames + the new expansion pack.", 1500, false, true},
	// 11 — Nina Kowalski, photos
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Sneak peek from Saturday is ready!", 3000, false, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Already?? You work fast.", 2900, true, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Couldn't sleep, culled half the shoot at 2am. Worth it.", 2800, false, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Those golden-hour shots are unreal.", 2700, true, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Right?? The light did all the work. Full album drops tonight, 120+ edited.", 2600, false, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "120?! How am I supposed to choose favorites?", 2500, true, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Can you pick your 10 favorites for prints?", 2400, false, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Picked! #3, #7 and #19 are going on the wall.", 2300, true, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Great taste. #19 is my favorite too — printing it big for you.", 2200, false, true},
	{11, "+1 (555) 014-5566", "Nina Kowalski", "Gallery proofs are ready — 24 shots to review!", 2100, false, true},
	// 12 — Sam Delgado, soccer carpool + backyard BBQ
	{12, "+1 (555) 011-8890", "Sam Delgado", "Saturday game moved to 10am — field 3, not field 1.", 4200, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Got it. I can drive — room for 3 kids plus all the gear.", 4100, true, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Perfect, I'll bring the orange slices and the pop-up tent.", 4000, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Kids voted you best snack parent three weeks running, you know.", 3900, true, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Bribery works. Don't tell the league. 😄", 3800, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Also — backyard BBQ after? Burgers and that corn salad you like.", 3650, true, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "We're in! I'll bring the cooler and Mia's famous potato salad.", 3550, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Tell Mia she's a legend. What time do the games end?", 3450, true, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Ours wraps around noon, so 2pm at your place?", 3350, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "2pm it is. I'll fire up the grill at 1:30.", 3200, true, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Need me to grab ice or charcoal on the way?", 3050, false, true},
	{12, "+1 (555) 011-8890", "Sam Delgado", "Actually I'll just grab both — see you Saturday! 🎉", 2900, false, true},
	// 13 — Chase Alerts
	{13, "+1 (555) 010-3321", "Chase Alerts", "Reminder: autopay of $86.10 posts on the 15th.", 4200, false, true},
	{13, "+1 (555) 010-3321", "Chase Alerts", "A $184.20 purchase at Green Grocers was approved.", 3900, false, false},
	// 14 — Emma Wilson, dad's birthday
	{14, "+1 (555) 013-4455", "Emma Wilson", "Dad's birthday — same plan as last year? Lunch at our place?", 7000, false, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Sounds perfect. I'll handle the cake this time.", 6800, true, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Chocolate with the raspberry filling? He still talks about it.", 6600, false, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "That's the one. Already ordered from Crumb & Crust.", 6400, true, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Are you coming home for Dad's birthday?", 6000, false, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Wouldn't miss it. Driving down Saturday morning.", 5800, true, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Can you grab ice + buns on your way?", 5600, false, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "On the list. Anything else?", 5450, true, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Aunt Carol is bringing dessert, we're covered.", 5350, false, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Perfect. See you at noon!", 5280, true, true},
	{14, "+1 (555) 013-4455", "Emma Wilson", "Mom says bring the potato salad on Sunday!", 5200, false, true},
}

func extraDemoMessages(nowMs int64) map[int64][]core.SMSMessage {
	grouped := make(map[int64][]core.SMSMessage)
	var nextID int64 = 501
	for _, d := range extraMsgTable {
		typ := 1
		if d.out {
			typ = 2
		}
		grouped[d.thread] = append(grouped[d.thread], core.SMSMessage{
			ID:          nextID,
			ThreadID:    d.thread,
			Address:     d.addr,
			Body:        d.body,
			Date:        nowMs - d.agoMin*60000,
			Type:        typ,
			Read:        d.read,
			ContactName: d.contact,
		})
		nextID++
	}
	return grouped
}

// Extra notifications across the already-bundled app icons (no new binary
// assets: pkg/app must match an entry in demoAppIcons).
type extraNotifDef struct {
	id     string
	pkg    string
	app    string
	title  string
	text   string
	agoMin int64
}

var extraNotifTable = []extraNotifDef{
	{"demo-n5", "com.whatsapp", "WhatsApp", "Family Group", "Emma: Don't forget sunscreen for Sunday! ☀️", 8},
	{"demo-n6", "com.google.android.gm", "Gmail", "Figma", "Your weekly design digest: 12 new community files.", 22},
	{"demo-n7", "com.slack", "Slack", "#design", "Sofia: new illustrations are in the shared drive 🎨", 48},
	{"demo-n8", "com.google.android.apps.messaging", "Messages", "Sam Delgado", "2pm Saturday, don't forget the cooler! 🎉", 65},
	{"demo-n9", "com.spotify.music", "Spotify", "New Release", "SoundWave dropped a 5-track EP you might like.", 90},
	{"demo-n10", "com.google.android.calendar", "Calendar", "Dentist Appointment", "Tomorrow at 9:00am · Bright Smile Clinic", 120},
	{"demo-n11", "com.google.android.gm", "Gmail", "Delta Airlines", "Your receipt for flight DL 4821 (DEN).", 150},
	{"demo-n12", "com.whatsapp", "WhatsApp", "Leo Fischer", "Sent you a voice memo: new beat idea 🎧", 185},
	{"demo-n13", "com.github.android", "GitHub", "fuseitall", "CI passed on main (12 jobs, 4m 31s).", 240},
	{"demo-n14", "com.slack", "Slack", "Priya Patel", "Paper draft v3 is ready for your review 📝", 300},
	{"demo-n15", "com.google.android.apps.photos", "Photos", "New memory", "Cabin weekend · 24 new photos rediscovered.", 380},
	{"demo-n16", "com.google.android.gm", "Gmail", "Chase", "Your statement is ready — autopay scheduled.", 460},
	{"demo-n17", "com.google.android.calendar", "Calendar", "Game Night", "Saturday at 7pm · Tom's place", 540},
	{"demo-n18", "com.spotify.music", "Spotify", "Daily Mix", "Made for you: M83, Tycho, Washed Out + more.", 660},
}

func extraDemoNotifications(nowUnix int64) []NotifItem {
	out := make([]NotifItem, 0, len(extraNotifTable))
	for _, d := range extraNotifTable {
		out = append(out, NotifItem{
			ID:          d.id,
			PackageName: d.pkg,
			App:         d.app,
			Title:       d.title,
			Text:        d.text,
			PostedUnix:  nowUnix - d.agoMin*60,
			IconB64:     demoAppIconPNG(d.app),
		})
	}
	return out
}
