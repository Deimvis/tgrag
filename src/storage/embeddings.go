package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Deimvis/tgrag/src/models"
)

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}
	err = ensureColumn(db, "windows", "text_hash", "TEXT NOT NULL DEFAULT ''")
	if err != nil {
		return err
	}
	_, err = db.Exec(schemaAfterColumnMigrations)
	if err != nil {
		return err
	}
	return backfillWindowTextHashes(db)
}

func ensureColumn(db *sql.DB, table string, column string, definition string) error {
	var count int
	err := db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`, table), column).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, definition))
	return err
}

func backfillWindowTextHashes(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, text FROM windows WHERE text_hash = ''`)
	if err != nil {
		return err
	}
	hashes := map[int64]string{}
	for rows.Next() {
		var id int64
		var text string
		err = rows.Scan(&id, &text)
		if err != nil {
			rows.Close()
			return err
		}
		hashes[id] = TextHash(text)
	}
	rows.Close()
	err = rows.Err()
	if err != nil {
		return err
	}
	if len(hashes) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, hash := range hashes {
		_, err = tx.Exec(`UPDATE windows SET text_hash = ? WHERE id = ?`, hash, id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type EmbeddingInput struct {
	TextHash string
	Text     string
}

func (s *Storage) WindowsWithoutEmbeddings(ctx context.Context, model string, chatId int64, limit int) ([]EmbeddingInput, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.text_hash, MIN(w.text)
		FROM windows w
		WHERE (? = 0 OR w.chat_id = ?)
			AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.model = ? AND e.text_hash = w.text_hash)
		GROUP BY w.text_hash
		ORDER BY MIN(w.id)
		LIMIT ?`, chatId, chatId, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []EmbeddingInput
	for rows.Next() {
		var in EmbeddingInput
		err = rows.Scan(&in.TextHash, &in.Text)
		if err != nil {
			return nil, err
		}
		result = append(result, in)
	}
	return result, rows.Err()
}

func (s *Storage) UpsertEmbeddings(ctx context.Context, model string, inputs []EmbeddingInput, vectors [][]float32) error {
	if len(inputs) != len(vectors) {
		return fmt.Errorf("got %d vectors for %d inputs", len(vectors), len(inputs))
	}
	if len(inputs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO embeddings (model, text_hash, dimension_count, vector) VALUES (?, ?, ?, ?)
		ON CONFLICT (model, text_hash) DO UPDATE SET dimension_count = excluded.dimension_count, vector = excluded.vector`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, in := range inputs {
		_, err = stmt.ExecContext(ctx, model, in.TextHash, len(vectors[i]), EncodeVector(vectors[i]))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) PruneEmbeddings(ctx context.Context, keptModel string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM embeddings
		WHERE model != ? OR text_hash NOT IN (SELECT text_hash FROM windows)`, keptModel)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Storage) EmbeddingCount(ctx context.Context, model string) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings WHERE model = ?`, model).Scan(&count)
	return count, err
}

func (s *Storage) SemanticSearch(ctx context.Context, params models.SearchParams, model string, queryVector []float32) ([]models.SearchHit, error) {
	if len(queryVector) == 0 {
		return nil, errors.New("empty query vector")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id, w.chat_id, c.kind, c.title, c.username, w.first_message_id, w.last_message_id, w.first_ts, w.last_ts, w.participants, w.text, e.vector
		FROM windows w
		JOIN embeddings e ON e.model = ? AND e.text_hash = w.text_hash
		JOIN chats c ON c.id = w.chat_id
		WHERE (? = 0 OR w.chat_id = ?)
			AND (? = '' OR LOWER(w.participants) LIKE '%' || LOWER(?) || '%')
			AND (? = 0 OR w.last_ts >= ?)
			AND (? = 0 OR w.first_ts <= ?)`,
		model,
		params.ChatId, params.ChatId,
		params.SenderName, params.SenderName,
		toUnix(params.After), toUnix(params.After),
		toUnix(params.Before), toUnix(params.Before))
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	defer rows.Close()
	top := NewTopK[models.SearchHit](params.Limit)
	for rows.Next() {
		var h models.SearchHit
		var kind, username, participants, text string
		var firstTs, lastTs int64
		var rawVector []byte
		err = rows.Scan(&h.WindowId, &h.ChatId, &kind, &h.ChatTitle, &username, &h.FirstMessageId, &h.LastMessageId, &firstTs, &lastTs, &participants, &text, &rawVector)
		if err != nil {
			return nil, err
		}
		vector, err := DecodeVector(rawVector)
		if err != nil {
			return nil, fmt.Errorf("window %d: %w", h.WindowId, err)
		}
		if len(vector) != len(queryVector) {
			return nil, fmt.Errorf("window %d embedding has %d dimensions, query has %d: run `tgrag embed --rebuild`", h.WindowId, len(vector), len(queryVector))
		}
		h.FirstTs = fromUnix(firstTs)
		h.LastTs = fromUnix(lastTs)
		h.Participants = decodeParticipants(participants)
		h.Snippet = SnippetPrefix(text, params.SnippetChars)
		h.Link = MessageLink(models.Chat{Id: h.ChatId, Kind: models.ChatKind(kind), Username: username}, h.FirstMessageId)
		h.Source = models.SearchHitSourceSemantic
		top.Add(h, CosineSimilarity(queryVector, vector))
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}
	hits, similarities := top.SortedDescending()
	for i := range hits {
		hits[i].Similarity = similarities[i]
	}
	return hits, nil
}

func SnippetPrefix(text string, maxChars int) string {
	runes := []rune(text)
	if maxChars <= 0 || len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "…"
}
