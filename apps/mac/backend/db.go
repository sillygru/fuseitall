// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"fuseitall/core"
)

// DB wraps the SQLite database for contacts, avatars, and SMS messages.
type DB struct {
	db *sql.DB
	mu sync.RWMutex
}

// DBFilePath returns ~/Library/Application Support/FuseItAll/fuseitall.db.
// In tests or when FUSEITALL_DB_PATH is set, it isolates the database
// so tests never pollute the user's live database.
func DBFilePath() (string, error) {
	if override := os.Getenv("FUSEITALL_DB_PATH"); override != "" {
		return override, nil
	}
	if flag.Lookup("test.v") != nil {
		return filepath.Join(os.TempDir(), fmt.Sprintf("fuseitall_test_%d.db", time.Now().UnixNano())), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "FuseItAll", "fuseitall.db"), nil
}

// OpenDB initializes or opens the SQLite database and runs migrations.
func OpenDB(path string) (*DB, error) {
	if path == "" {
		var err error
		path, err = DBFilePath()
		if err != nil {
			return nil, err
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Enable WAL mode and busy timeout for concurrent safety
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec %s: %w", pragma, err)
		}
	}

	schema := `
	CREATE TABLE IF NOT EXISTS contacts (
		contact_id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		payload TEXT NOT NULL,
		photo_version TEXT,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS avatars (
		cache_id TEXT PRIMARY KEY,
		data_b64 TEXT NOT NULL,
		photo_version TEXT,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS threads (
		thread_id INTEGER PRIMARY KEY,
		address TEXT NOT NULL,
		contact_name TEXT,
		contact_id TEXT,
		photo_version TEXT,
		snippet TEXT,
		date INTEGER NOT NULL,
		message_count INTEGER NOT NULL,
		unread_count INTEGER NOT NULL,
		read INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY,
		thread_id INTEGER NOT NULL,
		address TEXT NOT NULL,
		body TEXT NOT NULL,
		date INTEGER NOT NULL,
		type INTEGER NOT NULL,
		read INTEGER NOT NULL,
		status INTEGER NOT NULL,
		contact_name TEXT,
		contact_id TEXT,
		photo_version TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_messages_thread_date ON messages(thread_id, date DESC, id DESC);
	CREATE INDEX IF NOT EXISTS idx_threads_date ON threads(date DESC);
	CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(display_name COLLATE NOCASE);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init db schema: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.Close()
}

// SaveContacts persists or updates contacts in SQLite.
func (d *DB) SaveContacts(contacts []core.ContactEntry) error {
	if d == nil || d.db == nil || len(contacts) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO contacts (contact_id, display_name, payload, photo_version, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(contact_id) DO UPDATE SET
			display_name = excluded.display_name,
			payload = excluded.payload,
			photo_version = excluded.photo_version,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("prepare contact stmt: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UnixMilli()
	for _, c := range contacts {
		if c.ContactID == "" {
			continue
		}
		raw, err := json.Marshal(c)
		if err != nil {
			continue
		}
		if _, err := stmt.Exec(c.ContactID, c.DisplayName, string(raw), c.PhotoVersion, now); err != nil {
			return fmt.Errorf("exec contact stmt: %w", err)
		}
	}
	return tx.Commit()
}

// LoadAllContacts loads all persisted contacts sorted alphabetically by display_name.
func (d *DB) LoadAllContacts() ([]core.ContactEntry, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT payload FROM contacts ORDER BY display_name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query contacts: %w", err)
	}
	defer rows.Close()

	var results []core.ContactEntry
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var entry core.ContactEntry
		if err := json.Unmarshal([]byte(payload), &entry); err == nil {
			results = append(results, entry)
		}
	}
	return results, rows.Err()
}

// SaveAvatar stores an avatar in SQLite.
func (d *DB) SaveAvatar(cacheID, dataB64, photoVersion string) error {
	if d == nil || d.db == nil || cacheID == "" || dataB64 == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(`
		INSERT INTO avatars (cache_id, data_b64, photo_version, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(cache_id) DO UPDATE SET
			data_b64 = excluded.data_b64,
			photo_version = excluded.photo_version,
			updated_at = excluded.updated_at
	`, cacheID, dataB64, photoVersion, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("save avatar: %w", err)
	}
	return nil
}

// LoadAllAvatars loads all cached avatars and their versions into maps.
func (d *DB) LoadAllAvatars() (map[string]string, map[string]string, error) {
	if d == nil || d.db == nil {
		return nil, nil, nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT cache_id, data_b64, photo_version FROM avatars`)
	if err != nil {
		return nil, nil, fmt.Errorf("query avatars: %w", err)
	}
	defer rows.Close()

	avatars := make(map[string]string)
	versions := make(map[string]string)
	for rows.Next() {
		var cacheID, b64, ver string
		if err := rows.Scan(&cacheID, &b64, &ver); err == nil {
			avatars[cacheID] = b64
			if ver != "" {
				versions[cacheID] = ver
			}
		}
	}
	return avatars, versions, rows.Err()
}

// DeleteContact removes a contact and its avatar entries from SQLite.
func (d *DB) DeleteContact(contactID string) error {
	if d == nil || d.db == nil || contactID == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM contacts WHERE contact_id = ?`, contactID); err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM avatars WHERE cache_id = ? OR cache_id = ?`, contactID, contactID+"#full"); err != nil {
		return fmt.Errorf("delete avatars: %w", err)
	}
	return tx.Commit()
}

// SaveThreads persists or updates SMS conversation threads in SQLite.
func (d *DB) SaveThreads(threads []core.SMSThread) error {
	if d == nil || d.db == nil || len(threads) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO threads (thread_id, address, contact_name, contact_id, photo_version, snippet, date, message_count, unread_count, read)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			address = excluded.address,
			contact_name = CASE WHEN excluded.contact_name != '' THEN excluded.contact_name ELSE threads.contact_name END,
			contact_id = CASE WHEN excluded.contact_id != '' THEN excluded.contact_id ELSE threads.contact_id END,
			photo_version = CASE WHEN excluded.photo_version != '' THEN excluded.photo_version ELSE threads.photo_version END,
			snippet = excluded.snippet,
			date = excluded.date,
			message_count = excluded.message_count,
			unread_count = excluded.unread_count,
			read = excluded.read
	`)
	if err != nil {
		return fmt.Errorf("prepare thread stmt: %w", err)
	}
	defer stmt.Close()

	for _, t := range threads {
		if t.ThreadID <= 0 {
			continue
		}
		readInt := 0
		if t.Read {
			readInt = 1
		}
		if _, err := stmt.Exec(t.ThreadID, t.Address, t.ContactName, t.ContactID, t.PhotoVersion, t.Snippet, t.Date, t.MessageCount, t.UnreadCount, readInt); err != nil {
			return fmt.Errorf("exec thread stmt: %w", err)
		}
	}
	return tx.Commit()
}

// LoadAllThreads loads all persisted threads ordered by date DESC.
func (d *DB) LoadAllThreads() ([]core.SMSThread, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT thread_id, address, contact_name, contact_id, photo_version, snippet, date, message_count, unread_count, read
		FROM threads ORDER BY date DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	var threads []core.SMSThread
	for rows.Next() {
		var t core.SMSThread
		var contactName, contactID, photoVersion, snippet sql.NullString
		var readInt int
		if err := rows.Scan(&t.ThreadID, &t.Address, &contactName, &contactID, &photoVersion, &snippet, &t.Date, &t.MessageCount, &t.UnreadCount, &readInt); err != nil {
			continue
		}
		t.ContactName = contactName.String
		t.ContactID = contactID.String
		t.PhotoVersion = photoVersion.String
		t.Snippet = snippet.String
		t.Read = readInt == 1
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// SaveMessages persists messages in SQLite.
func (d *DB) SaveMessages(messages []core.SMSMessage) error {
	if d == nil || d.db == nil || len(messages) == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO messages (id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			body = excluded.body,
			date = excluded.date,
			type = excluded.type,
			read = excluded.read,
			status = excluded.status,
			contact_name = CASE WHEN excluded.contact_name != '' THEN excluded.contact_name ELSE messages.contact_name END,
			contact_id = CASE WHEN excluded.contact_id != '' THEN excluded.contact_id ELSE messages.contact_id END,
			photo_version = CASE WHEN excluded.photo_version != '' THEN excluded.photo_version ELSE messages.photo_version END
	`)
	if err != nil {
		return fmt.Errorf("prepare message stmt: %w", err)
	}
	defer stmt.Close()

	for _, m := range messages {
		if m.ID == 0 {
			continue
		}
		readInt := 0
		if m.Read {
			readInt = 1
		}
		if _, err := stmt.Exec(m.ID, m.ThreadID, m.Address, m.Body, m.Date, m.Type, readInt, m.Status, m.ContactName, m.ContactID, m.PhotoVersion); err != nil {
			return fmt.Errorf("exec message stmt: %w", err)
		}
	}
	return tx.Commit()
}

// LoadMessagesForThread loads the most recent messages for a thread (up to limit) in chronological order.
func (d *DB) LoadMessagesForThread(threadID int64, limit int) ([]core.SMSMessage, error) {
	if d == nil || d.db == nil || threadID <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`
		SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
		FROM (
			SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
			FROM messages
			WHERE thread_id = ?
			ORDER BY date DESC, id DESC
			LIMIT ?
		)
		ORDER BY date ASC, id ASC
	`, threadID, limit)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var msgs []core.SMSMessage
	for rows.Next() {
		var m core.SMSMessage
		var contactName, contactID, photoVersion sql.NullString
		var readInt int
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Address, &m.Body, &m.Date, &m.Type, &readInt, &m.Status, &contactName, &contactID, &photoVersion); err != nil {
			continue
		}
		m.ContactName = contactName.String
		m.ContactID = contactID.String
		m.PhotoVersion = photoVersion.String
		m.Read = readInt == 1
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// LoadMessagesBeforeCursor loads messages older than (cursorDate, cursorID) for a thread in chronological order.
func (d *DB) LoadMessagesBeforeCursor(threadID int64, cursorDate int64, cursorID int64, limit int) ([]core.SMSMessage, error) {
	if d == nil || d.db == nil || threadID <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	var rows *sql.Rows
	var err error
	if cursorID > 0 {
		rows, err = d.db.Query(`
			SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
			FROM (
				SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
				FROM messages
				WHERE thread_id = ?
				  AND (date < ? OR (date = ? AND id < ?))
				ORDER BY date DESC, id DESC
				LIMIT ?
			)
			ORDER BY date ASC, id ASC
		`, threadID, cursorDate, cursorDate, cursorID, limit)
	} else {
		rows, err = d.db.Query(`
			SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
			FROM (
				SELECT id, thread_id, address, body, date, type, read, status, contact_name, contact_id, photo_version
				FROM messages
				WHERE thread_id = ?
				  AND date < ?
				ORDER BY date DESC, id DESC
				LIMIT ?
			)
			ORDER BY date ASC, id ASC
		`, threadID, cursorDate, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query older messages: %w", err)
	}
	defer rows.Close()

	var msgs []core.SMSMessage
	for rows.Next() {
		var m core.SMSMessage
		var contactName, contactID, photoVersion sql.NullString
		var readInt int
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Address, &m.Body, &m.Date, &m.Type, &readInt, &m.Status, &contactName, &contactID, &photoVersion); err != nil {
			continue
		}
		m.ContactName = contactName.String
		m.ContactID = contactID.String
		m.PhotoVersion = photoVersion.String
		m.Read = readInt == 1
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MarkThreadReadInDB marks a thread and its messages as read in SQLite.
func (d *DB) MarkThreadReadInDB(threadID int64) error {
	if d == nil || d.db == nil || threadID <= 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`UPDATE threads SET unread_count = 0, read = 1 WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("update thread read: %w", err)
	}
	if _, err := tx.Exec(`UPDATE messages SET read = 1 WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("update messages read: %w", err)
	}
	return tx.Commit()
}

// ClearAll deletes all cached contacts, avatars, threads, and messages.
func (d *DB) ClearAll() error {
	if d == nil || d.db == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, tbl := range []string{"contacts", "avatars", "threads", "messages"} {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", tbl)); err != nil {
			return fmt.Errorf("delete from %s: %w", tbl, err)
		}
	}
	return tx.Commit()
}
