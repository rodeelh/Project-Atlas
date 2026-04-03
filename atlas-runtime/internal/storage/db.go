// Package storage provides a SQLite database layer for the Go runtime.
// The schema matches the Swift MemoryStore so both runtimes can share the
// same database file during Phase 5 dual-run.
package storage

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// DB wraps a SQLite connection and exposes typed query methods.
type DB struct {
	conn *sql.DB
}

// Open opens the SQLite database at path and runs all schema migrations.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite is single-writer; one connection is correct.

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("storage: migrate: %w", err)
	}
	return db, nil
}

// Close closes the underlying SQLite connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate creates or updates the schema to match the Swift MemoryStore schema.
// Each migration is idempotent (CREATE TABLE IF NOT EXISTS / ALTER TABLE ADD COLUMN).
func (db *DB) migrate() error {
	stmts := []string{
		// conversations — matches Swift MemoryStore conversations table
		`CREATE TABLE IF NOT EXISTS conversations (
			conversation_id  TEXT PRIMARY KEY,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL,
			platform_context TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_updated_at
			ON conversations(updated_at DESC)`,

		// messages — matches Swift MemoryStore messages table
		`CREATE TABLE IF NOT EXISTS messages (
			message_id      TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL REFERENCES conversations(conversation_id),
			role            TEXT NOT NULL,
			content         TEXT NOT NULL,
			timestamp       TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_conversation_id
			ON messages(conversation_id)`,

		// web_sessions — matches Swift MemoryStore web_sessions table exactly.
		// created_at / expires_at / refreshed_at stored as Unix timestamp doubles
		// (REAL) to match the Swift Double column type.
		`CREATE TABLE IF NOT EXISTS web_sessions (
			session_id   TEXT PRIMARY KEY,
			created_at   REAL NOT NULL,
			refreshed_at REAL NOT NULL,
			expires_at   REAL NOT NULL,
			is_remote    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_web_sessions_expires_at
			ON web_sessions(expires_at)`,

		// deferred_executions — matches Swift MemoryStore deferred_executions table.
		// created_at / updated_at stored as ISO8601 TEXT (SQLite.swift Date serialization).
		`CREATE TABLE IF NOT EXISTS deferred_executions (
			deferred_id            TEXT PRIMARY KEY,
			source_type            TEXT NOT NULL,
			skill_id               TEXT,
			tool_id                TEXT,
			action_id              TEXT,
			tool_call_id           TEXT NOT NULL,
			normalized_input_json  TEXT NOT NULL,
			conversation_id        TEXT,
			originating_message_id TEXT,
			approval_id            TEXT NOT NULL,
			summary                TEXT NOT NULL DEFAULT '',
			permission_level       TEXT NOT NULL DEFAULT 'execute',
			risk_level             TEXT NOT NULL DEFAULT 'execute',
			status                 TEXT NOT NULL,
			last_error             TEXT,
			result_json            TEXT,
			created_at             TEXT NOT NULL,
			updated_at             TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_deferred_tool_call_id
			ON deferred_executions(tool_call_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_deferred_approval_id
			ON deferred_executions(approval_id)`,
		`CREATE INDEX IF NOT EXISTS idx_deferred_status
			ON deferred_executions(status)`,

		// telegram_sessions — matches Swift MemoryStore telegram_sessions table.
		// chat_id is INTEGER (Int64 in Swift), timestamps are ISO8601 TEXT.
		`CREATE TABLE IF NOT EXISTS telegram_sessions (
			chat_id                INTEGER PRIMARY KEY,
			user_id                INTEGER,
			active_conversation_id TEXT NOT NULL,
			created_at             TEXT NOT NULL,
			updated_at             TEXT NOT NULL,
			last_message_id        INTEGER
		)`,

		// communication_sessions — matches Swift MemoryStore communication_sessions table.
		// Primary key is composite (platform, channel_id, thread_id).
		`CREATE TABLE IF NOT EXISTS communication_sessions (
			platform               TEXT NOT NULL,
			channel_id             TEXT NOT NULL,
			thread_id              TEXT NOT NULL DEFAULT '',
			channel_name           TEXT,
			user_id                TEXT,
			active_conversation_id TEXT NOT NULL,
			created_at             TEXT NOT NULL,
			updated_at             TEXT NOT NULL,
			last_message_id        TEXT,
			PRIMARY KEY (platform, channel_id, thread_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_comm_sessions_platform
			ON communication_sessions(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_comm_sessions_updated_at
			ON communication_sessions(updated_at DESC)`,

		// memories — matches Swift MemoryStore memories table.
		// created_at / updated_at / last_retrieved_at stored as ISO8601 TEXT
		// (SQLite.swift Expression<Date> serialization).
		`CREATE TABLE IF NOT EXISTS memories (
			memory_id               TEXT PRIMARY KEY,
			category                TEXT NOT NULL,
			title                   TEXT NOT NULL,
			content                 TEXT NOT NULL,
			source                  TEXT NOT NULL,
			confidence              REAL NOT NULL DEFAULT 0.0,
			importance              REAL NOT NULL DEFAULT 0.0,
			created_at              TEXT NOT NULL,
			updated_at              TEXT NOT NULL,
			last_retrieved_at       TEXT,
			is_user_confirmed       INTEGER NOT NULL DEFAULT 0,
			is_sensitive            INTEGER NOT NULL DEFAULT 0,
			tags_json               TEXT NOT NULL DEFAULT '[]',
			related_conversation_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_category
			ON memories(category)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_importance
			ON memories(importance DESC, updated_at DESC)`,

		// gremlin_runs — stores automation run history.
		// started_at / finished_at stored as Unix timestamp doubles (REAL).
		`CREATE TABLE IF NOT EXISTS gremlin_runs (
			run_id          TEXT PRIMARY KEY,
			gremlin_id      TEXT NOT NULL,
			started_at      REAL NOT NULL,
			finished_at     REAL,
			status          TEXT NOT NULL,
			output          TEXT,
			error_message   TEXT,
			conversation_id TEXT,
			workflow_run_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gremlin_runs_gremlin_id
			ON gremlin_runs(gremlin_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gremlin_runs_started_at
			ON gremlin_runs(started_at DESC)`,

		// browser_sessions — persists login cookies across Atlas restarts.
		// cookies_json holds a JSON array of simplified cookie records.
		// Sessions expire after 7 days of non-use.
		`CREATE TABLE IF NOT EXISTS browser_sessions (
			host         TEXT PRIMARY KEY,
			cookies_json TEXT NOT NULL DEFAULT '[]',
			last_used_at TEXT NOT NULL,
			created_at   TEXT NOT NULL
		)`,
	}

	// Idempotent migrations for rows added to deferred_executions after its initial creation.
	// SQLite returns an error when a column already exists; swallow those errors.
	alterDeferred := []string{
		`ALTER TABLE deferred_executions ADD COLUMN skill_id TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN tool_id TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN action_id TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN conversation_id TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN originating_message_id TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE deferred_executions ADD COLUMN permission_level TEXT NOT NULL DEFAULT 'execute'`,
		`ALTER TABLE deferred_executions ADD COLUMN risk_level TEXT NOT NULL DEFAULT 'execute'`,
		`ALTER TABLE deferred_executions ADD COLUMN last_error TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN result_json TEXT`,
		`ALTER TABLE deferred_executions ADD COLUMN preview_diff TEXT`,
	}

	// Idempotent migrations for browser_sessions columns added after initial creation.
	alterBrowserSessions := []string{
		`ALTER TABLE browser_sessions ADD COLUMN session_name TEXT NOT NULL DEFAULT ''`,
	}

	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed (%s...): %w", stmt[:min(40, len(stmt))], err)
		}
	}
	// Swallow errors — column already exists is expected on re-open.
	for _, stmt := range alterDeferred {
		db.conn.Exec(stmt) //nolint:errcheck
	}
	for _, stmt := range alterBrowserSessions {
		db.conn.Exec(stmt) //nolint:errcheck
	}
	return nil
}

// ── Web sessions ─────────────────────────────────────────────────────────────

// SessionRecord is the raw DB row for a web session.
type SessionRecord struct {
	ID          string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RefreshedAt time.Time
	IsRemote    bool
}

// SaveWebSession inserts or replaces a session record.
func (db *DB) SaveWebSession(id string, createdAt, expiresAt time.Time, isRemote bool) error {
	now := time.Now()
	_, err := db.conn.Exec(
		`INSERT OR REPLACE INTO web_sessions(session_id, created_at, refreshed_at, expires_at, is_remote)
		 VALUES (?, ?, ?, ?, ?)`,
		id,
		createdAt.Unix(),
		now.Unix(),
		expiresAt.Unix(),
		boolToInt(isRemote),
	)
	return err
}

// FetchWebSession returns the session record for id, or nil if not found / expired.
func (db *DB) FetchWebSession(id string) (*SessionRecord, error) {
	row := db.conn.QueryRow(
		`SELECT session_id, created_at, refreshed_at, expires_at, is_remote
		 FROM web_sessions WHERE session_id = ?`, id)

	var rec SessionRecord
	var createdTS, refreshedTS, expiresTS float64
	var isRemoteInt int
	if err := row.Scan(&rec.ID, &createdTS, &refreshedTS, &expiresTS, &isRemoteInt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rec.CreatedAt = time.Unix(int64(createdTS), 0)
	rec.RefreshedAt = time.Unix(int64(refreshedTS), 0)
	rec.ExpiresAt = time.Unix(int64(expiresTS), 0)
	rec.IsRemote = isRemoteInt != 0
	return &rec, nil
}

// RefreshWebSession slides the refreshed_at timestamp forward for a session.
func (db *DB) RefreshWebSession(id string) error {
	_, err := db.conn.Exec(
		`UPDATE web_sessions SET refreshed_at = ? WHERE session_id = ?`,
		time.Now().Unix(), id,
	)
	return err
}

// DeleteWebSession removes a single session record.
func (db *DB) DeleteWebSession(id string) error {
	_, err := db.conn.Exec(`DELETE FROM web_sessions WHERE session_id = ?`, id)
	return err
}

// DeleteAllRemoteWebSessions removes all remote sessions (e.g. after API key rotation).
func (db *DB) DeleteAllRemoteWebSessions() error {
	_, err := db.conn.Exec(`DELETE FROM web_sessions WHERE is_remote = 1`)
	return err
}

// DeleteAllConversations removes all conversations and their messages from the database.
func (db *DB) DeleteAllConversations() error {
	_, err := db.conn.Exec(`DELETE FROM messages`)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`DELETE FROM conversations`)
	return err
}

// ── Conversations ─────────────────────────────────────────────────────────────

// ConversationRow is a lightweight conversation record (no messages).
type ConversationRow struct {
	ID              string
	CreatedAt       string
	UpdatedAt       string
	PlatformContext *string
}

// ConversationSummaryRow extends ConversationRow with summary fields for the
// web UI list view, matching the contracts.ts ConversationSummary interface.
type ConversationSummaryRow struct {
	ID                   string
	CreatedAt            string
	UpdatedAt            string
	PlatformContext      *string
	MessageCount         int
	FirstUserMessage     *string
	LastAssistantMessage *string
}

// ListConversationSummaries returns recent conversations with message counts and
// excerpt fields, ordered by updated_at DESC. This is the richer version used
// by the web UI list view (contracts.ts ConversationSummary).
func (db *DB) ListConversationSummaries(limit int) ([]ConversationSummaryRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(`
		SELECT
			c.conversation_id,
			c.created_at,
			c.updated_at,
			c.platform_context,
			(SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.conversation_id) AS message_count,
			(SELECT m2.content FROM messages m2
			 WHERE m2.conversation_id = c.conversation_id AND m2.role = 'user'
			 ORDER BY m2.timestamp ASC LIMIT 1) AS first_user_message,
			(SELECT m3.content FROM messages m3
			 WHERE m3.conversation_id = c.conversation_id AND m3.role = 'assistant'
			 ORDER BY m3.timestamp DESC LIMIT 1) AS last_assistant_message
		FROM conversations c
		ORDER BY c.updated_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConversationSummaryRow
	for rows.Next() {
		var r ConversationSummaryRow
		if err := rows.Scan(
			&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.PlatformContext,
			&r.MessageCount, &r.FirstUserMessage, &r.LastAssistantMessage,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchConversationSummaries returns conversations whose messages contain query,
// ordered by updated_at DESC. Uses the same summary shape as ListConversationSummaries.
func (db *DB) SearchConversationSummaries(query string, limit int) ([]ConversationSummaryRow, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + query + "%"
	rows, err := db.conn.Query(`
		SELECT
			c.conversation_id,
			c.created_at,
			c.updated_at,
			c.platform_context,
			(SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.conversation_id) AS message_count,
			(SELECT m2.content FROM messages m2
			 WHERE m2.conversation_id = c.conversation_id AND m2.role = 'user'
			 ORDER BY m2.timestamp ASC LIMIT 1) AS first_user_message,
			(SELECT m3.content FROM messages m3
			 WHERE m3.conversation_id = c.conversation_id AND m3.role = 'assistant'
			 ORDER BY m3.timestamp DESC LIMIT 1) AS last_assistant_message
		FROM conversations c
		WHERE EXISTS (
			SELECT 1 FROM messages mx
			WHERE mx.conversation_id = c.conversation_id
			AND LOWER(mx.content) LIKE LOWER(?)
		)
		ORDER BY c.updated_at DESC
		LIMIT ?`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConversationSummaryRow
	for rows.Next() {
		var r ConversationSummaryRow
		if err := rows.Scan(
			&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.PlatformContext,
			&r.MessageCount, &r.FirstUserMessage, &r.LastAssistantMessage,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveConversation inserts a new conversation record. No-op if ID already exists.
func (db *DB) SaveConversation(id, createdAt, updatedAt string, platformContext *string) error {
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO conversations(conversation_id, created_at, updated_at, platform_context)
		 VALUES (?, ?, ?, ?)`,
		id, createdAt, updatedAt, platformContext,
	)
	return err
}

// TouchConversation updates updated_at for an existing conversation.
func (db *DB) TouchConversation(id, updatedAt string) error {
	_, err := db.conn.Exec(
		`UPDATE conversations SET updated_at = ? WHERE conversation_id = ?`,
		updatedAt, id,
	)
	return err
}

// ListConversations returns recent conversations ordered by updated_at DESC.
func (db *DB) ListConversations(limit int) ([]ConversationRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(
		`SELECT conversation_id, created_at, updated_at, platform_context
		 FROM conversations ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConversationRow
	for rows.Next() {
		var r ConversationRow
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.PlatformContext); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FetchConversation returns a single conversation by ID, or nil if not found.
func (db *DB) FetchConversation(id string) (*ConversationRow, error) {
	row := db.conn.QueryRow(
		`SELECT conversation_id, created_at, updated_at, platform_context
		 FROM conversations WHERE conversation_id = ?`, id)
	var r ConversationRow
	if err := row.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt, &r.PlatformContext); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ── Messages ──────────────────────────────────────────────────────────────────

// MessageRow is a single message record.
type MessageRow struct {
	ID             string
	ConversationID string
	Role           string
	Content        string
	Timestamp      string
}

// SaveMessage inserts a message and updates the conversation's updated_at.
func (db *DB) SaveMessage(id, convID, role, content, timestamp string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO messages(message_id, conversation_id, role, content, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		id, convID, role, content, timestamp,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE conversations SET updated_at = ? WHERE conversation_id = ?`,
		timestamp, convID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMessages returns all messages for a conversation ordered by timestamp ASC.
func (db *DB) ListMessages(convID string) ([]MessageRow, error) {
	rows, err := db.conn.Query(
		`SELECT message_id, conversation_id, role, content, timestamp
		 FROM messages WHERE conversation_id = ? ORDER BY timestamp ASC`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MessageRow
	for rows.Next() {
		var r MessageRow
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.Role, &r.Content, &r.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── Deferred executions ───────────────────────────────────────────────────────

// DeferredExecRow is a raw deferred_executions row.
type DeferredExecRow struct {
	DeferredID           string
	SourceType           string
	SkillID              *string
	ToolID               *string
	ActionID             *string
	ToolCallID           string
	NormalizedInputJSON  string
	ConversationID       *string
	OriginatingMessageID *string
	ApprovalID           string
	Summary              string
	PermissionLevel      string
	RiskLevel            string
	Status               string
	LastError            *string
	ResultJSON           *string
	CreatedAt            string
	UpdatedAt            string
	PreviewDiff          *string
}

const deferredCols = `deferred_id, source_type, skill_id, tool_id, action_id,
	tool_call_id, normalized_input_json, conversation_id, originating_message_id,
	approval_id, summary, permission_level, risk_level, status, last_error,
	result_json, created_at, updated_at, preview_diff`

func scanDeferredRow(row interface{ Scan(...any) error }) (*DeferredExecRow, error) {
	var r DeferredExecRow
	err := row.Scan(
		&r.DeferredID, &r.SourceType, &r.SkillID, &r.ToolID, &r.ActionID,
		&r.ToolCallID, &r.NormalizedInputJSON, &r.ConversationID, &r.OriginatingMessageID,
		&r.ApprovalID, &r.Summary, &r.PermissionLevel, &r.RiskLevel, &r.Status, &r.LastError,
		&r.ResultJSON, &r.CreatedAt, &r.UpdatedAt, &r.PreviewDiff,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveDeferredExecution inserts a new deferred_executions row.
func (db *DB) SaveDeferredExecution(r DeferredExecRow) error {
	_, err := db.conn.Exec(
		`INSERT INTO deferred_executions(
			deferred_id, source_type, skill_id, tool_id, action_id,
			tool_call_id, normalized_input_json, conversation_id, originating_message_id,
			approval_id, summary, permission_level, risk_level, status, last_error,
			result_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.DeferredID, r.SourceType, r.SkillID, r.ToolID, r.ActionID,
		r.ToolCallID, r.NormalizedInputJSON, r.ConversationID, r.OriginatingMessageID,
		r.ApprovalID, r.Summary, r.PermissionLevel, r.RiskLevel, r.Status, r.LastError,
		r.ResultJSON, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

// FetchDeferredsByConversationID returns all deferred_executions for a conversation with the given status.
func (db *DB) FetchDeferredsByConversationID(convID, status string) ([]DeferredExecRow, error) {
	rows, err := db.conn.Query(
		`SELECT `+deferredCols+`
		 FROM deferred_executions WHERE conversation_id = ? AND status = ?
		 ORDER BY created_at DESC`, convID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeferredExecRow
	for rows.Next() {
		r, err := scanDeferredRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// CountConversations returns the total number of conversations in the DB.
func (db *DB) CountConversations() int {
	var n int
	db.conn.QueryRow(`SELECT COUNT(*) FROM conversations`).Scan(&n)
	return n
}

// ListPendingApprovals returns up to limit pending deferred_executions rows, oldest first.
func (db *DB) ListPendingApprovals(limit int) ([]DeferredExecRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.conn.Query(
		`SELECT `+deferredCols+`
		 FROM deferred_executions WHERE status = 'pending_approval'
		 ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeferredExecRow
	for rows.Next() {
		r, err := scanDeferredRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// CountPendingApprovals returns the number of deferred_executions with status='pending_approval'.
func (db *DB) CountPendingApprovals() int {
	var n int
	db.conn.QueryRow(`SELECT COUNT(*) FROM deferred_executions WHERE status = 'pending_approval'`).Scan(&n)
	return n
}

// ListAllApprovals returns all deferred_executions rows ordered by created_at DESC.
func (db *DB) ListAllApprovals(limit int) ([]DeferredExecRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.conn.Query(
		`SELECT `+deferredCols+`
		 FROM deferred_executions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeferredExecRow
	for rows.Next() {
		r, err := scanDeferredRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// FetchDeferredByToolCallID returns the deferred_executions row for a given tool_call_id.
func (db *DB) FetchDeferredByToolCallID(toolCallID string) (*DeferredExecRow, error) {
	row := db.conn.QueryRow(
		`SELECT `+deferredCols+`
		 FROM deferred_executions WHERE tool_call_id = ?`, toolCallID)
	r, err := scanDeferredRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// UpdateDeferredStatus sets the status and updated_at for a deferred_executions row
// identified by tool_call_id.
func (db *DB) UpdateDeferredStatus(toolCallID, status, updatedAt string) error {
	_, err := db.conn.Exec(
		`UPDATE deferred_executions SET status = ?, updated_at = ? WHERE tool_call_id = ?`,
		status, updatedAt, toolCallID,
	)
	return err
}

// SetPreviewDiff stores a pre-computed unified diff preview for the approval UI.
// Called after SaveDeferredExecution for write/patch operations.
func (db *DB) SetPreviewDiff(toolCallID, diff string) error {
	_, err := db.conn.Exec(
		`UPDATE deferred_executions SET preview_diff = ? WHERE tool_call_id = ?`,
		diff, toolCallID,
	)
	return err
}

// ── Telegram sessions ─────────────────────────────────────────────────────────

// TelegramSessionRow is a raw telegram_sessions row.
type TelegramSessionRow struct {
	ChatID               int64
	UserID               *int64
	ActiveConversationID string
	CreatedAt            string
	UpdatedAt            string
	LastMessageID        *int64
}

// ListTelegramSessions returns all telegram_sessions rows ordered by updated_at DESC.
func (db *DB) ListTelegramSessions() ([]TelegramSessionRow, error) {
	rows, err := db.conn.Query(
		`SELECT chat_id, user_id, active_conversation_id, created_at, updated_at, last_message_id
		 FROM telegram_sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TelegramSessionRow
	for rows.Next() {
		var r TelegramSessionRow
		if err := rows.Scan(&r.ChatID, &r.UserID, &r.ActiveConversationID,
			&r.CreatedAt, &r.UpdatedAt, &r.LastMessageID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── Communication sessions ────────────────────────────────────────────────────

// CommSessionRow is a raw communication_sessions row.
type CommSessionRow struct {
	Platform             string
	ChannelID            string
	ThreadID             string
	ChannelName          *string
	UserID               *string
	ActiveConversationID string
	CreatedAt            string
	UpdatedAt            string
	LastMessageID        *string
}

// ListCommunicationChannels returns all communication_sessions rows ordered by updated_at DESC.
// Pass a non-empty platform string to filter by platform.
func (db *DB) ListCommunicationChannels(platform string) ([]CommSessionRow, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if platform != "" {
		rows, err = db.conn.Query(
			`SELECT platform, channel_id, thread_id, channel_name, user_id,
			        active_conversation_id, created_at, updated_at, last_message_id
			 FROM communication_sessions WHERE platform = ? ORDER BY updated_at DESC`, platform)
	} else {
		rows, err = db.conn.Query(
			`SELECT platform, channel_id, thread_id, channel_name, user_id,
			        active_conversation_id, created_at, updated_at, last_message_id
			 FROM communication_sessions ORDER BY updated_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CommSessionRow
	for rows.Next() {
		var r CommSessionRow
		if err := rows.Scan(&r.Platform, &r.ChannelID, &r.ThreadID, &r.ChannelName, &r.UserID,
			&r.ActiveConversationID, &r.CreatedAt, &r.UpdatedAt, &r.LastMessageID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ── Gremlin runs ──────────────────────────────────────────────────────────────

// GremlinRunRow is a raw gremlin_runs row.
type GremlinRunRow struct {
	RunID          string
	GremlinID      string
	StartedAt      float64
	FinishedAt     *float64
	Status         string
	Output         *string
	ErrorMessage   *string
	ConversationID *string
	WorkflowRunID  *string
}

// ListGremlinRuns returns runs for a gremlin (or all runs when gremlinID is empty),
// ordered by started_at DESC, limited to limit rows.
func (db *DB) ListGremlinRuns(gremlinID string, limit int) ([]GremlinRunRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if gremlinID != "" {
		rows, err = db.conn.Query(
			`SELECT run_id, gremlin_id, started_at, finished_at, status, output, error_message, conversation_id, workflow_run_id
			 FROM gremlin_runs WHERE gremlin_id = ? ORDER BY started_at DESC LIMIT ?`, gremlinID, limit)
	} else {
		rows, err = db.conn.Query(
			`SELECT run_id, gremlin_id, started_at, finished_at, status, output, error_message, conversation_id, workflow_run_id
			 FROM gremlin_runs ORDER BY started_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GremlinRunRow
	for rows.Next() {
		var r GremlinRunRow
		if err := rows.Scan(&r.RunID, &r.GremlinID, &r.StartedAt, &r.FinishedAt,
			&r.Status, &r.Output, &r.ErrorMessage, &r.ConversationID, &r.WorkflowRunID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveGremlinRun inserts a new gremlin_run row.
func (db *DB) SaveGremlinRun(r GremlinRunRow) error {
	_, err := db.conn.Exec(
		`INSERT INTO gremlin_runs
		 (run_id, gremlin_id, started_at, finished_at, status, output, error_message, conversation_id, workflow_run_id)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		r.RunID, r.GremlinID, r.StartedAt, r.FinishedAt, r.Status,
		r.Output, r.ErrorMessage, r.ConversationID, r.WorkflowRunID,
	)
	return err
}

// UpdateGremlinRun sets finished_at, status, and output on an existing run.
func (db *DB) UpdateGremlinRun(runID, status string, output *string, finishedAt float64) error {
	_, err := db.conn.Exec(
		`UPDATE gremlin_runs SET finished_at=?, status=?, output=? WHERE run_id=?`,
		finishedAt, status, output, runID,
	)
	return err
}

// ── Memories ──────────────────────────────────────────────────────────────────

// MemoryRow is a raw memories row.
type MemoryRow struct {
	ID                    string
	Category              string
	Title                 string
	Content               string
	Source                string
	Confidence            float64
	Importance            float64
	CreatedAt             string
	UpdatedAt             string
	LastRetrievedAt       *string
	IsUserConfirmed       bool
	IsSensitive           bool
	TagsJSON              string
	RelatedConversationID *string
}

const memoryCols = `memory_id, category, title, content, source, confidence, importance,
	created_at, updated_at, last_retrieved_at, is_user_confirmed, is_sensitive,
	tags_json, related_conversation_id`

func scanMemoryRow(row interface{ Scan(...any) error }) (*MemoryRow, error) {
	var r MemoryRow
	var isConfirmedInt, isSensitiveInt int
	err := row.Scan(
		&r.ID, &r.Category, &r.Title, &r.Content, &r.Source,
		&r.Confidence, &r.Importance,
		&r.CreatedAt, &r.UpdatedAt, &r.LastRetrievedAt,
		&isConfirmedInt, &isSensitiveInt,
		&r.TagsJSON, &r.RelatedConversationID,
	)
	if err != nil {
		return nil, err
	}
	r.IsUserConfirmed = isConfirmedInt != 0
	r.IsSensitive = isSensitiveInt != 0
	return &r, nil
}

// ListMemories returns memories ordered by importance DESC, updated_at DESC.
// Pass a non-empty category to filter. limit <= 0 defaults to 100.
func (db *DB) ListMemories(limit int, category string) ([]MemoryRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if category != "" {
		rows, err = db.conn.Query(
			`SELECT `+memoryCols+`
			 FROM memories WHERE category = ?
			 ORDER BY importance DESC, updated_at DESC LIMIT ?`, category, limit)
	} else {
		rows, err = db.conn.Query(
			`SELECT `+memoryCols+`
			 FROM memories
			 ORDER BY importance DESC, updated_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemoryRow
	for rows.Next() {
		r, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// SearchMemories performs a case-insensitive search on title and content.
func (db *DB) SearchMemories(query string, limit int) ([]MemoryRow, error) {
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + query + "%"
	rows, err := db.conn.Query(
		`SELECT `+memoryCols+`
		 FROM memories
		 WHERE title LIKE ? OR content LIKE ?
		 ORDER BY importance DESC, updated_at DESC LIMIT ?`,
		pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemoryRow
	for rows.Next() {
		r, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// DeleteMemory removes a memory by its ID. No-op if not found.
func (db *DB) DeleteMemory(id string) error {
	_, err := db.conn.Exec(`DELETE FROM memories WHERE memory_id = ?`, id)
	return err
}

// SaveMemory inserts a new memory row. ID, CreatedAt, and UpdatedAt must be pre-populated.
func (db *DB) SaveMemory(r MemoryRow) error {
	_, err := db.conn.Exec(
		`INSERT INTO memories (`+memoryCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Category, r.Title, r.Content, r.Source, r.Confidence, r.Importance,
		r.CreatedAt, r.UpdatedAt, r.LastRetrievedAt,
		boolToInt(r.IsUserConfirmed), boolToInt(r.IsSensitive),
		r.TagsJSON, r.RelatedConversationID,
	)
	return err
}

// UpdateMemory updates the mutable fields of an existing memory row.
func (db *DB) UpdateMemory(r MemoryRow) error {
	_, err := db.conn.Exec(
		`UPDATE memories SET title=?, content=?, confidence=?, importance=?, updated_at=?,
		 is_user_confirmed=?, is_sensitive=?, tags_json=? WHERE memory_id=?`,
		r.Title, r.Content, r.Confidence, r.Importance, r.UpdatedAt,
		boolToInt(r.IsUserConfirmed), boolToInt(r.IsSensitive),
		r.TagsJSON, r.ID,
	)
	return err
}

// FetchMemory returns a single memory by ID, or nil if not found.
func (db *DB) FetchMemory(id string) (*MemoryRow, error) {
	row := db.conn.QueryRow(
		`SELECT `+memoryCols+` FROM memories WHERE memory_id=?`, id)
	r, err := scanMemoryRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// ConfirmMemory sets is_user_confirmed=1 on the memory with the given ID.
func (db *DB) ConfirmMemory(id string) error {
	_, err := db.conn.Exec(
		`UPDATE memories SET is_user_confirmed=1, updated_at=? WHERE memory_id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), id,
	)
	return err
}

// FindDuplicateMemory returns an existing memory matching the given category and title, or nil.
func (db *DB) FindDuplicateMemory(category, title string) (*MemoryRow, error) {
	row := db.conn.QueryRow(
		`SELECT `+memoryCols+` FROM memories WHERE category=? AND title=? LIMIT 1`,
		category, title,
	)
	r, err := scanMemoryRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// CountMemories returns the total number of memories in the database.
func (db *DB) CountMemories() int {
	var n int
	db.conn.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n) //nolint:errcheck
	return n
}

// ListAllMemories returns every memory row with no limit, ordered by category
// then importance DESC. Used by the dream cycle for consolidation scans.
func (db *DB) ListAllMemories() ([]MemoryRow, error) {
	rows, err := db.conn.Query(
		`SELECT ` + memoryCols + `
		 FROM memories
		 ORDER BY category, importance DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MemoryRow
	for rows.Next() {
		r, err := scanMemoryRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// DeleteStaleMemories removes old, low-value memories. Returns the count deleted.
// Rules:
//   - confidence < minConfidence AND age > maxAge
//   - last_retrieved_at IS NULL AND age > unretrievedMaxAge AND importance < minImportance
func (db *DB) DeleteStaleMemories(maxAgeDays, unretrievedMaxAgeDays int, minConfidence, minImportance float64) int {
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -maxAgeDays).Format(time.RFC3339Nano)
	unretrievedCutoff := now.AddDate(0, 0, -unretrievedMaxAgeDays).Format(time.RFC3339Nano)

	// Low-confidence old memories.
	r1, _ := db.conn.Exec(
		`DELETE FROM memories WHERE confidence < ? AND created_at < ?`,
		minConfidence, cutoff)
	n1, _ := r1.RowsAffected()

	// Never-retrieved old memories with low importance.
	r2, _ := db.conn.Exec(
		`DELETE FROM memories WHERE last_retrieved_at IS NULL AND created_at < ? AND importance < ?`,
		unretrievedCutoff, minImportance)
	n2, _ := r2.RowsAffected()

	return int(n1 + n2)
}

// RelevantMemories returns memories scored by a weighted combination of keyword
// relevance (0.5), static importance (0.3), and time-decayed recency (0.2).
// Falls back to ListMemories when the query yields no keyword matches.
func (db *DB) RelevantMemories(query string, limit int) ([]MemoryRow, error) {
	if limit <= 0 {
		limit = 4
	}
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		return db.ListMemories(limit, "")
	}

	// Pre-filter: fetch top 50 by importance as candidates.
	all, err := db.ListMemories(50, "")
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	now := time.Now()

	type scored struct {
		row   MemoryRow
		score float64
	}
	var results []scored

	for _, m := range all {
		// Keyword relevance: fraction of query keywords found in title+content+tags.
		haystack := strings.ToLower(m.Title + " " + m.Content + " " + m.TagsJSON)
		hits := 0
		for _, kw := range keywords {
			if strings.Contains(haystack, kw) {
				hits++
			}
		}
		keywordScore := float64(hits) / float64(len(keywords))

		// Time-decayed recency: exponential decay with 7-day half-life.
		var hoursAge float64
		if t, err := time.Parse(time.RFC3339Nano, m.UpdatedAt); err == nil {
			hoursAge = now.Sub(t).Hours()
		}
		recencyScore := math.Exp(-0.693 * hoursAge / (7.0 * 24.0))

		combined := keywordScore*0.5 + m.Importance*0.3 + recencyScore*0.2
		results = append(results, scored{row: m, score: combined})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// If no keyword matched at all, the ordering is purely importance+recency
	// which is fine — we still return the best candidates.
	if len(results) > limit {
		results = results[:limit]
	}
	out := make([]MemoryRow, len(results))
	for i, r := range results {
		out[i] = r.row
	}
	return out, nil
}

// UpdateLastRetrieved sets last_retrieved_at = now for a batch of memory IDs.
func (db *DB) UpdateLastRetrieved(ids []string) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		db.conn.Exec(`UPDATE memories SET last_retrieved_at=? WHERE memory_id=?`, now, id) //nolint:errcheck
	}
}

// extractKeywords splits a query into lowercased words, filtering stop words.
func extractKeywords(query string) []string {
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
		"were": true, "be": true, "been": true, "being": true, "have": true,
		"has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true,
		"might": true, "can": true, "shall": true, "to": true, "of": true,
		"in": true, "for": true, "on": true, "with": true, "at": true,
		"by": true, "from": true, "as": true, "into": true, "about": true,
		"it": true, "its": true, "i": true, "me": true, "my": true,
		"we": true, "our": true, "you": true, "your": true, "he": true,
		"she": true, "they": true, "them": true, "this": true, "that": true,
		"and": true, "or": true, "but": true, "not": true, "so": true,
		"if": true, "what": true, "how": true, "when": true, "where": true,
		"who": true, "which": true, "why": true, "just": true, "also": true,
	}
	words := strings.Fields(strings.ToLower(query))
	var out []string
	for _, w := range words {
		// Strip punctuation from edges.
		w = strings.Trim(w, ".,!?;:'\"()[]{}/-")
		if len(w) < 2 || stop[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// FetchTelegramSession returns the telegram_sessions row for chatID, or nil if not found.
func (db *DB) FetchTelegramSession(chatID int64) (*TelegramSessionRow, error) {
	row := db.conn.QueryRow(
		`SELECT chat_id, user_id, active_conversation_id, created_at, updated_at, last_message_id
		 FROM telegram_sessions WHERE chat_id = ?`, chatID)
	var r TelegramSessionRow
	if err := row.Scan(&r.ChatID, &r.UserID, &r.ActiveConversationID,
		&r.CreatedAt, &r.UpdatedAt, &r.LastMessageID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// UpsertTelegramSession inserts or replaces a telegram_sessions row.
func (db *DB) UpsertTelegramSession(r TelegramSessionRow) error {
	_, err := db.conn.Exec(
		`INSERT INTO telegram_sessions
		     (chat_id, user_id, active_conversation_id, created_at, updated_at, last_message_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(chat_id) DO UPDATE SET
		     user_id                = excluded.user_id,
		     active_conversation_id = excluded.active_conversation_id,
		     updated_at             = excluded.updated_at,
		     last_message_id        = excluded.last_message_id`,
		r.ChatID, r.UserID, r.ActiveConversationID, r.CreatedAt, r.UpdatedAt, r.LastMessageID,
	)
	return err
}

// FetchCommSession returns the communication_sessions row, or nil if not found.
func (db *DB) FetchCommSession(platform, channelID, threadID string) (*CommSessionRow, error) {
	row := db.conn.QueryRow(
		`SELECT platform, channel_id, thread_id, channel_name, user_id,
		        active_conversation_id, created_at, updated_at, last_message_id
		 FROM communication_sessions
		 WHERE platform = ? AND channel_id = ? AND thread_id = ?`,
		platform, channelID, threadID)
	var r CommSessionRow
	if err := row.Scan(&r.Platform, &r.ChannelID, &r.ThreadID, &r.ChannelName, &r.UserID,
		&r.ActiveConversationID, &r.CreatedAt, &r.UpdatedAt, &r.LastMessageID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// UpsertCommSession inserts or replaces a communication_sessions row.
func (db *DB) UpsertCommSession(r CommSessionRow) error {
	_, err := db.conn.Exec(
		`INSERT INTO communication_sessions
		     (platform, channel_id, thread_id, channel_name, user_id,
		      active_conversation_id, created_at, updated_at, last_message_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(platform, channel_id, thread_id) DO UPDATE SET
		     channel_name           = excluded.channel_name,
		     user_id                = excluded.user_id,
		     active_conversation_id = excluded.active_conversation_id,
		     updated_at             = excluded.updated_at,
		     last_message_id        = excluded.last_message_id`,
		r.Platform, r.ChannelID, r.ThreadID, r.ChannelName, r.UserID,
		r.ActiveConversationID, r.CreatedAt, r.UpdatedAt, r.LastMessageID,
	)
	return err
}

// ── Browser sessions ──────────────────────────────────────────────────────────

// SaveBrowserSession upserts the cookie blob for a host.
func (db *DB) SaveBrowserSession(host, cookiesJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`INSERT INTO browser_sessions (host, cookies_json, last_used_at, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(host) DO UPDATE SET
		   cookies_json = excluded.cookies_json,
		   last_used_at = excluded.last_used_at`,
		host, cookiesJSON, now, now,
	)
	return err
}

// LoadBrowserSession returns the stored cookie blob for a host.
// Returns ("", false, nil) when no session exists.
func (db *DB) LoadBrowserSession(host string) (cookiesJSON string, found bool, err error) {
	var lastUsed string
	row := db.conn.QueryRow(
		`SELECT cookies_json, last_used_at FROM browser_sessions WHERE host = ?`, host,
	)
	if scanErr := row.Scan(&cookiesJSON, &lastUsed); scanErr != nil {
		if scanErr == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, scanErr
	}
	// Expire sessions older than 7 days.
	t, parseErr := time.Parse(time.RFC3339, lastUsed)
	if parseErr != nil || time.Since(t) > 7*24*time.Hour {
		_ = db.DeleteBrowserSession(host)
		return "", false, nil
	}
	return cookiesJSON, true, nil
}

// DeleteBrowserSession removes the stored session for a host.
func (db *DB) DeleteBrowserSession(host string) error {
	_, err := db.conn.Exec(`DELETE FROM browser_sessions WHERE host = ?`, host)
	return err
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
