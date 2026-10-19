package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/embeddings"
	"github.com/Deimvis/tgrag/src/storage"
	"github.com/Deimvis/tgrag/src/windows"
)

const embedPageSize = 256

type Indexer struct {
	storage  *storage.Storage
	embedder *embeddings.Embedder
	params   windows.Params
	log      io.Writer
}

func New(cfg configs.Config, storage *storage.Storage, embedder *embeddings.Embedder) *Indexer {
	return &Indexer{
		storage:  storage,
		embedder: embedder,
		params: windows.Params{
			Gap:      time.Duration(cfg.Windows.GapMinutes) * time.Minute,
			MaxChars: cfg.Windows.MaxChars,
		},
		log: os.Stderr,
	}
}

func (i *Indexer) ReindexChat(ctx context.Context, chatId int64) (int, error) {
	messages, err := i.storage.ChatMessages(ctx, chatId)
	if err != nil {
		return 0, fmt.Errorf("load chat %d messages: %w", chatId, err)
	}
	built := windows.Build(chatId, messages, i.params)
	err = i.storage.ReplaceWindows(ctx, chatId, built)
	if err != nil {
		return 0, fmt.Errorf("replace chat %d windows: %w", chatId, err)
	}
	if i.embedder.IsEnabled() {
		_, err = i.EmbedChat(ctx, chatId)
		if err != nil {
			return len(built), fmt.Errorf("embed chat %d windows: %w", chatId, err)
		}
	}
	return len(built), nil
}

func (i *Indexer) ReindexAll(ctx context.Context) (int, error) {
	chats, err := i.storage.ListChats(ctx, "", 1_000_000)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, c := range chats {
		n, err := i.ReindexChat(ctx, c.Id)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

type EmbedResult struct {
	EmbeddedWindowCount  int   `json:"embedded_window_count"`
	PrunedEmbeddingCount int64 `json:"pruned_embedding_count"`
}

func (i *Indexer) EmbedChat(ctx context.Context, chatId int64) (int, error) {
	return i.embedMissing(ctx, chatId)
}

func (i *Indexer) EmbedAll(ctx context.Context) (EmbedResult, error) {
	var result EmbedResult
	if !i.embedder.IsEnabled() {
		return result, embeddings.ErrDisabled
	}
	pruned, err := i.storage.PruneEmbeddings(ctx, i.embedder.Model())
	if err != nil {
		return result, fmt.Errorf("prune embeddings: %w", err)
	}
	result.PrunedEmbeddingCount = pruned
	count, err := i.embedMissing(ctx, 0)
	result.EmbeddedWindowCount = count
	return result, err
}

func (i *Indexer) embedMissing(ctx context.Context, chatId int64) (int, error) {
	total := 0
	for {
		inputs, err := i.storage.WindowsWithoutEmbeddings(ctx, i.embedder.Model(), chatId, embedPageSize)
		if err != nil {
			return total, err
		}
		if len(inputs) == 0 {
			return total, nil
		}
		texts := make([]string, 0, len(inputs))
		for _, in := range inputs {
			texts = append(texts, in.Text)
		}
		vectors, err := i.embedder.Embed(ctx, texts)
		if err != nil {
			return total, err
		}
		err = i.storage.UpsertEmbeddings(ctx, i.embedder.Model(), inputs, vectors)
		if err != nil {
			return total, err
		}
		total += len(inputs)
		fmt.Fprintf(i.log, "embedded %d windows\n", total)
	}
}
