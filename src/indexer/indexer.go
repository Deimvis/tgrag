package indexer

import (
	"context"
	"fmt"
	"time"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/storage"
	"github.com/Deimvis/tgrag/src/windows"
)

type Indexer struct {
	storage *storage.Storage
	params  windows.Params
}

func New(cfg configs.Config, storage *storage.Storage) *Indexer {
	return &Indexer{
		storage: storage,
		params: windows.Params{
			Gap:      time.Duration(cfg.Windows.GapMinutes) * time.Minute,
			MaxChars: cfg.Windows.MaxChars,
		},
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
