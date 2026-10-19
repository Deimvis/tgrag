package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Deimvis/tgrag/src/models"
)

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	id INTEGER PRIMARY KEY,
	kind TEXT NOT NULL,
	title TEXT NOT NULL,
	username TEXT NOT NULL DEFAULT '',
	access_hash INTEGER NOT NULL DEFAULT 0,
	latest_message_ts INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	chat_id INTEGER NOT NULL,
	id INTEGER NOT NULL,
	sender_user_id INTEGER NOT NULL DEFAULT 0,
	sender_name TEXT NOT NULL DEFAULT '',
	ts INTEGER NOT NULL,
	edit_ts INTEGER NOT NULL DEFAULT 0,
	reply_to_message_id INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL,
	is_outgoing INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (chat_id, id)
);
CREATE INDEX IF NOT EXISTS messages_chat_ts ON messages (chat_id, ts);
CREATE TABLE IF NOT EXISTS windows (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	chat_id INTEGER NOT NULL,
	first_message_id INTEGER NOT NULL,
	last_message_id INTEGER NOT NULL,
	first_ts INTEGER NOT NULL,
	last_ts INTEGER NOT NULL,
	participants TEXT NOT NULL,
	text TEXT NOT NULL,
	text_hash TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS windows_chat ON windows (chat_id, first_message_id);
CREATE TABLE IF NOT EXISTS embeddings (
	model TEXT NOT NULL,
	text_hash TEXT NOT NULL,
	dimension_count INTEGER NOT NULL,
	vector BLOB NOT NULL,
	PRIMARY KEY (model, text_hash)
);
CREATE VIRTUAL TABLE IF NOT EXISTS windows_fts USING fts5 (
	text,
	participants,
	content='windows',
	content_rowid='id',
	tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS windows_ai AFTER INSERT ON windows BEGIN
	INSERT INTO windows_fts (rowid, text, participants) VALUES (new.id, new.text, new.participants);
END;
CREATE TRIGGER IF NOT EXISTS windows_ad AFTER DELETE ON windows BEGIN
	INSERT INTO windows_fts (windows_fts, rowid, text, participants) VALUES ('delete', old.id, old.text, old.participants);
END;
CREATE TABLE IF NOT EXISTS sync_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	finish_ts INTEGER NOT NULL,
	synced_chat_count INTEGER NOT NULL,
	new_message_count INTEGER NOT NULL
);
`

const schemaAfterColumnMigrations = `
CREATE INDEX IF NOT EXISTS windows_text_hash ON windows (text_hash);
`

type Storage struct {
	db *sql.DB
}

func Open(path string) (*Storage, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	err = migrate(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Storage{db: db}, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) UpsertChat(ctx context.Context, chat models.Chat) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chats (id, kind, title, username, access_hash, latest_message_ts)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			kind = excluded.kind,
			title = excluded.title,
			username = excluded.username,
			access_hash = CASE WHEN excluded.access_hash != 0 THEN excluded.access_hash ELSE chats.access_hash END,
			latest_message_ts = MAX(chats.latest_message_ts, excluded.latest_message_ts)`,
		chat.Id, string(chat.Kind), chat.Title, chat.Username, chat.AccessHash, toUnix(chat.LatestMessageTs))
	return err
}

func (s *Storage) UpsertMessages(ctx context.Context, messages []models.Message) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO messages (chat_id, id, sender_user_id, sender_name, ts, edit_ts, reply_to_message_id, text, is_outgoing)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat_id, id) DO UPDATE SET
			sender_user_id = excluded.sender_user_id,
			sender_name = excluded.sender_name,
			ts = excluded.ts,
			edit_ts = excluded.edit_ts,
			reply_to_message_id = excluded.reply_to_message_id,
			text = excluded.text,
			is_outgoing = excluded.is_outgoing`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range messages {
		_, err = stmt.ExecContext(ctx, m.ChatId, m.Id, m.SenderUserId, m.SenderName, toUnix(m.Ts), toUnix(m.EditTs), m.ReplyToMessageId, m.Text, boolToInt(m.IsOutgoing))
		if err != nil {
			return fmt.Errorf("insert message %d/%d: %w", m.ChatId, m.Id, err)
		}
	}
	return tx.Commit()
}

func (s *Storage) DeleteChatData(ctx context.Context, chatId int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM windows WHERE chat_id = ?`,
		`DELETE FROM messages WHERE chat_id = ?`,
	} {
		_, err = tx.ExecContext(ctx, q, chatId)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) MaxMessageId(ctx context.Context, chatId int64) (int64, error) {
	var maxId sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(id) FROM messages WHERE chat_id = ?`, chatId).Scan(&maxId)
	if err != nil {
		return 0, err
	}
	return maxId.Int64, nil
}

func (s *Storage) ChatMessages(ctx context.Context, chatId int64) ([]models.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_id, id, sender_user_id, sender_name, ts, edit_ts, reply_to_message_id, text, is_outgoing
		FROM messages WHERE chat_id = ? ORDER BY id`, chatId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Storage) ReplaceWindows(ctx context.Context, chatId int64, windows []models.Window) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM windows WHERE chat_id = ?`, chatId)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO windows (chat_id, first_message_id, last_message_id, first_ts, last_ts, participants, text, text_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, w := range windows {
		_, err = stmt.ExecContext(ctx, w.ChatId, w.FirstMessageId, w.LastMessageId, toUnix(w.FirstTs), toUnix(w.LastTs), encodeParticipants(w.Participants), w.Text, TextHash(w.Text))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE chats SET latest_message_ts = COALESCE((SELECT MAX(ts) FROM messages WHERE chat_id = ?), 0) WHERE id = ?`, chatId, chatId)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Storage) ListChats(ctx context.Context, titleQuery string, limit int) ([]models.Chat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.kind, c.title, c.username, c.access_hash, c.latest_message_ts,
			(SELECT COUNT(*) FROM messages m WHERE m.chat_id = c.id)
		FROM chats c
		WHERE (? = '' OR LOWER(c.title) LIKE '%' || LOWER(?) || '%' OR LOWER(c.username) LIKE '%' || LOWER(?) || '%')
		ORDER BY c.latest_message_ts DESC
		LIMIT ?`, titleQuery, titleQuery, titleQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []models.Chat
	for rows.Next() {
		var c models.Chat
		var kind string
		var latestTs int64
		err = rows.Scan(&c.Id, &kind, &c.Title, &c.Username, &c.AccessHash, &latestTs, &c.MessageCount)
		if err != nil {
			return nil, err
		}
		c.Kind = models.ChatKind(kind)
		c.LatestMessageTs = fromUnix(latestTs)
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *Storage) GetChat(ctx context.Context, chatId int64) (models.Chat, error) {
	var c models.Chat
	var kind string
	var latestTs int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, title, username, access_hash, latest_message_ts FROM chats WHERE id = ?`, chatId).
		Scan(&c.Id, &kind, &c.Title, &c.Username, &c.AccessHash, &latestTs)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("chat %d not found", chatId)
	}
	if err != nil {
		return c, err
	}
	c.Kind = models.ChatKind(kind)
	c.LatestMessageTs = fromUnix(latestTs)
	return c, nil
}

func (s *Storage) Search(ctx context.Context, params models.SearchParams) ([]models.SearchHit, error) {
	ftsQuery := BuildFtsQuery(params.Query)
	if ftsQuery == "" {
		return nil, errors.New("empty query")
	}
	snippetTokens := max(8, min(64, params.SnippetChars/6))
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id, w.chat_id, c.kind, c.title, c.username, w.first_message_id, w.last_message_id, w.first_ts, w.last_ts, w.participants,
			snippet(windows_fts, 0, '[', ']', '…', ?), bm25(windows_fts, 1.0, 0.5)
		FROM windows_fts
		JOIN windows w ON w.id = windows_fts.rowid
		JOIN chats c ON c.id = w.chat_id
		WHERE windows_fts MATCH ?
			AND (? = 0 OR w.chat_id = ?)
			AND (? = '' OR LOWER(w.participants) LIKE '%' || LOWER(?) || '%')
			AND (? = 0 OR w.last_ts >= ?)
			AND (? = 0 OR w.first_ts <= ?)
		ORDER BY bm25(windows_fts, 1.0, 0.5)
		LIMIT ?`,
		snippetTokens, ftsQuery,
		params.ChatId, params.ChatId,
		params.SenderName, params.SenderName,
		toUnix(params.After), toUnix(params.After),
		toUnix(params.Before), toUnix(params.Before),
		params.Limit)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", ftsQuery, err)
	}
	defer rows.Close()
	var result []models.SearchHit
	for rows.Next() {
		var h models.SearchHit
		var kind, username, participants string
		var firstTs, lastTs int64
		err = rows.Scan(&h.WindowId, &h.ChatId, &kind, &h.ChatTitle, &username, &h.FirstMessageId, &h.LastMessageId, &firstTs, &lastTs, &participants, &h.Snippet, &h.Rank)
		if err != nil {
			return nil, err
		}
		h.FirstTs = fromUnix(firstTs)
		h.LastTs = fromUnix(lastTs)
		h.Participants = decodeParticipants(participants)
		h.Link = MessageLink(models.Chat{Id: h.ChatId, Kind: models.ChatKind(kind), Username: username}, h.FirstMessageId)
		h.Source = models.SearchHitSourceFts
		result = append(result, h)
	}
	return result, rows.Err()
}

func (s *Storage) GetWindow(ctx context.Context, windowId int64) (models.Window, error) {
	var w models.Window
	var firstTs, lastTs int64
	var participants string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, chat_id, first_message_id, last_message_id, first_ts, last_ts, participants, text FROM windows WHERE id = ?`, windowId).
		Scan(&w.Id, &w.ChatId, &w.FirstMessageId, &w.LastMessageId, &firstTs, &lastTs, &participants, &w.Text)
	if errors.Is(err, sql.ErrNoRows) {
		return w, fmt.Errorf("window %d not found", windowId)
	}
	if err != nil {
		return w, err
	}
	w.FirstTs = fromUnix(firstTs)
	w.LastTs = fromUnix(lastTs)
	w.Participants = decodeParticipants(participants)
	return w, nil
}

func (s *Storage) GetContext(ctx context.Context, chatId int64, messageId int64, before int, after int) ([]models.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT * FROM (
			SELECT chat_id, id, sender_user_id, sender_name, ts, edit_ts, reply_to_message_id, text, is_outgoing
			FROM messages WHERE chat_id = ? AND id < ? ORDER BY id DESC LIMIT ?
		)
		UNION ALL
		SELECT * FROM (
			SELECT chat_id, id, sender_user_id, sender_name, ts, edit_ts, reply_to_message_id, text, is_outgoing
			FROM messages WHERE chat_id = ? AND id >= ? ORDER BY id ASC LIMIT ?
		)
		ORDER BY id`, chatId, messageId, before, chatId, messageId, after+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Storage) GetThread(ctx context.Context, chatId int64, messageId int64, maxDepth int) ([]models.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE chain(id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT m.reply_to_message_id, chain.depth + 1
			FROM messages m JOIN chain ON m.id = chain.id AND m.chat_id = ?
			WHERE m.reply_to_message_id != 0 AND chain.depth < ?
		)
		SELECT m.chat_id, m.id, m.sender_user_id, m.sender_name, m.ts, m.edit_ts, m.reply_to_message_id, m.text, m.is_outgoing
		FROM messages m
		WHERE m.chat_id = ? AND (m.id IN (SELECT id FROM chain) OR m.reply_to_message_id = ?)
		ORDER BY m.id`, messageId, chatId, maxDepth, chatId, messageId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Storage) RecordSyncRun(ctx context.Context, syncedChatCount int, newMessageCount int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_runs (finish_ts, synced_chat_count, new_message_count) VALUES (?, ?, ?)`,
		time.Now().Unix(), syncedChatCount, newMessageCount)
	return err
}

type Stats struct {
	ChatCount           int64     `json:"chat_count"`
	MessageCount        int64     `json:"message_count"`
	WindowCount         int64     `json:"window_count"`
	EmbeddedWindowCount int64     `json:"embedded_window_count"`
	EmbeddingModel      string    `json:"embedding_model,omitempty"`
	LatestSyncTs        time.Time `json:"latest_sync_ts"`
	LatestMessageTs     time.Time `json:"latest_message_ts"`
}

func (s *Storage) Stats(ctx context.Context, embeddingModel string) (Stats, error) {
	st := Stats{EmbeddingModel: embeddingModel}
	var syncTs, msgTs sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM chats), (SELECT COUNT(*) FROM messages), (SELECT COUNT(*) FROM windows),
			(SELECT COUNT(*) FROM windows w WHERE EXISTS (SELECT 1 FROM embeddings e WHERE e.model = ? AND e.text_hash = w.text_hash)),
			(SELECT MAX(finish_ts) FROM sync_runs), (SELECT MAX(ts) FROM messages)`, embeddingModel).
		Scan(&st.ChatCount, &st.MessageCount, &st.WindowCount, &st.EmbeddedWindowCount, &syncTs, &msgTs)
	if err != nil {
		return st, err
	}
	st.LatestSyncTs = fromUnix(syncTs.Int64)
	st.LatestMessageTs = fromUnix(msgTs.Int64)
	return st, nil
}

func scanMessages(rows *sql.Rows) ([]models.Message, error) {
	var result []models.Message
	for rows.Next() {
		var m models.Message
		var ts, editTs, isOutgoing int64
		err := rows.Scan(&m.ChatId, &m.Id, &m.SenderUserId, &m.SenderName, &ts, &editTs, &m.ReplyToMessageId, &m.Text, &isOutgoing)
		if err != nil {
			return nil, err
		}
		m.Ts = fromUnix(ts)
		m.EditTs = fromUnix(editTs)
		m.IsOutgoing = isOutgoing != 0
		result = append(result, m)
	}
	return result, rows.Err()
}

func MessageLink(chat models.Chat, messageId int64) string {
	switch chat.Kind {
	case models.ChatKindPrivate:
		return fmt.Sprintf("tg://openmessage?user_id=%d&message_id=%d", chat.Id, messageId)
	case models.ChatKindChannel:
		if chat.Username != "" {
			return fmt.Sprintf("https://t.me/%s/%d", chat.Username, messageId)
		}
		return fmt.Sprintf("https://t.me/c/%d/%d", chat.Id, messageId)
	default:
		return fmt.Sprintf("tg://openmessage?chat_id=%d&message_id=%d", chat.Id, messageId)
	}
}

func BuildFtsQuery(query string) string {
	var terms []string
	for _, token := range splitQueryTokens(query) {
		switch {
		case token == "AND" || token == "OR" || token == "NOT":
			terms = append(terms, token)
		case strings.HasPrefix(token, `"`):
			terms = append(terms, token)
		case strings.HasSuffix(token, "*"):
			terms = append(terms, `"`+strings.ReplaceAll(strings.TrimSuffix(token, "*"), `"`, `""`)+`"*`)
		default:
			terms = append(terms, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
		}
	}
	return strings.Join(terms, " ")
}

func splitQueryTokens(query string) []string {
	var tokens []string
	var current strings.Builder
	isQuoted := false
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			current.WriteRune(r)
			isQuoted = !isQuoted
			if !isQuoted {
				flush()
			}
		case !isQuoted && (r == ' ' || r == '\t' || r == '\n'):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if isQuoted {
		current.WriteRune('"')
	}
	flush()
	return tokens
}

func encodeParticipants(participants []string) string {
	raw, _ := json.Marshal(participants)
	return string(raw)
}

func decodeParticipants(raw string) []string {
	var participants []string
	_ = json.Unmarshal([]byte(raw), &participants)
	return participants
}

func toUnix(ts time.Time) int64 {
	if ts.IsZero() {
		return 0
	}
	return ts.Unix()
}

func fromUnix(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
