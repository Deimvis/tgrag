package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Deimvis/go-ext/go1.25/xoptional"

	"github.com/Deimvis/tgrag/src/models"
	"github.com/Deimvis/tgrag/src/storage"
)

const (
	DefaultSearchLimit  = 20
	DefaultSnippetChars = 200
	DefaultContextSize  = 20
	MaxThreadDepth      = 50
)

type Definition struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type Registry struct {
	storage *storage.Storage
}

func New(storage *storage.Storage) *Registry {
	return &Registry{storage: storage}
}

type ListChatsInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type SearchInput struct {
	Query  string `json:"query"`
	ChatId int64  `json:"chat_id"`
	Sender string `json:"sender"`
	After  string `json:"after"`
	Before string `json:"before"`
	Limit  int    `json:"limit"`
}

type GetContextInput struct {
	ChatId    int64            `json:"chat_id"`
	MessageId int64            `json:"message_id"`
	Before    xoptional.T[int] `json:"before"`
	After     xoptional.T[int] `json:"after"`
}

type GetThreadInput struct {
	ChatId    int64 `json:"chat_id"`
	MessageId int64 `json:"message_id"`
}

type MessageOutput struct {
	Id               int64  `json:"id"`
	Sender           string `json:"sender"`
	Ts               string `json:"ts"`
	ReplyToMessageId int64  `json:"reply_to_message_id,omitempty"`
	Text             string `json:"text"`
	Link             string `json:"link"`
}

func Definitions() []Definition {
	return []Definition{
		{
			Name:        "list_chats",
			Description: "List synced Telegram chats (most recently active first). Optional case-insensitive substring filter on chat title or username.",
			InputSchema: objectSchema(map[string]any{
				"query": map[string]any{"type": "string", "description": "Substring of chat title or username"},
				"limit": map[string]any{"type": "integer", "description": "Max chats to return, default 50"},
			}, nil),
		},
		{
			Name:        "search",
			Description: "Full-text (BM25) search over conversation windows of synced Telegram chats. Query syntax: space-separated terms are ANDed; use OR/NOT; a trailing * makes a prefix (e.g. квартир* matches квартира/квартиру); quote \"exact phrase\". Returns compact snippets with [matches] marked, message id ranges and links.",
			InputSchema: objectSchema(map[string]any{
				"query":   map[string]any{"type": "string", "description": "FTS query"},
				"chat_id": map[string]any{"type": "integer", "description": "Restrict to chat id"},
				"sender":  map[string]any{"type": "string", "description": "Restrict to windows where this participant name substring took part"},
				"after":   map[string]any{"type": "string", "description": "Only windows ending after this date (YYYY-MM-DD or RFC3339)"},
				"before":  map[string]any{"type": "string", "description": "Only windows starting before this date (YYYY-MM-DD or RFC3339)"},
				"limit":   map[string]any{"type": "integer", "description": "Max hits, default 20"},
			}, []string{"query"}),
		},
		{
			Name:        "get_context",
			Description: "Get raw messages around a message id in a chat (before/after counts, default 20 each). Use it to read the full conversation around a search hit.",
			InputSchema: objectSchema(map[string]any{
				"chat_id":    map[string]any{"type": "integer"},
				"message_id": map[string]any{"type": "integer"},
				"before":     map[string]any{"type": "integer"},
				"after":      map[string]any{"type": "integer"},
			}, []string{"chat_id", "message_id"}),
		},
		{
			Name:        "get_thread",
			Description: "Get the reply chain for a message: all ancestors it replies to and direct replies to it.",
			InputSchema: objectSchema(map[string]any{
				"chat_id":    map[string]any{"type": "integer"},
				"message_id": map[string]any{"type": "integer"},
			}, []string{"chat_id", "message_id"}),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func (r *Registry) Call(ctx context.Context, name string, rawInput []byte) (any, error) {
	switch name {
	case "list_chats":
		var in ListChatsInput
		err := json.Unmarshal(rawInput, &in)
		if err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		return r.ListChats(ctx, in)
	case "search":
		var in SearchInput
		err := json.Unmarshal(rawInput, &in)
		if err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		return r.Search(ctx, in)
	case "get_context":
		var in GetContextInput
		err := json.Unmarshal(rawInput, &in)
		if err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		return r.GetContext(ctx, in)
	case "get_thread":
		var in GetThreadInput
		err := json.Unmarshal(rawInput, &in)
		if err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		return r.GetThread(ctx, in)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (r *Registry) ListChats(ctx context.Context, in ListChatsInput) ([]models.Chat, error) {
	if in.Limit <= 0 {
		in.Limit = 50
	}
	chats, err := r.storage.ListChats(ctx, in.Query, in.Limit)
	if err != nil {
		return nil, err
	}
	if chats == nil {
		chats = []models.Chat{}
	}
	return chats, nil
}

func (r *Registry) Search(ctx context.Context, in SearchInput) ([]models.SearchHit, error) {
	after, err := ParseDate(in.After)
	if err != nil {
		return nil, fmt.Errorf("parse after: %w", err)
	}
	before, err := ParseDate(in.Before)
	if err != nil {
		return nil, fmt.Errorf("parse before: %w", err)
	}
	if !before.IsZero() && len(in.Before) == len("2006-01-02") {
		before = before.Add(24 * time.Hour)
	}
	if in.Limit <= 0 {
		in.Limit = DefaultSearchLimit
	}
	hits, err := r.storage.Search(ctx, models.SearchParams{
		Query:        in.Query,
		ChatId:       in.ChatId,
		SenderName:   in.Sender,
		After:        after,
		Before:       before,
		Limit:        in.Limit,
		SnippetChars: DefaultSnippetChars,
	})
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []models.SearchHit{}
	}
	return hits, nil
}

func (r *Registry) GetContext(ctx context.Context, in GetContextInput) ([]MessageOutput, error) {
	before := xoptional.ValueOr(in.Before, DefaultContextSize)
	after := xoptional.ValueOr(in.After, DefaultContextSize)
	if before < 0 || after < 0 {
		return nil, fmt.Errorf("before/after must be non-negative")
	}
	chat, err := r.storage.GetChat(ctx, in.ChatId)
	if err != nil {
		return nil, err
	}
	messages, err := r.storage.GetContext(ctx, in.ChatId, in.MessageId, before, after)
	if err != nil {
		return nil, err
	}
	return formatMessages(chat, messages), nil
}

func (r *Registry) GetThread(ctx context.Context, in GetThreadInput) ([]MessageOutput, error) {
	chat, err := r.storage.GetChat(ctx, in.ChatId)
	if err != nil {
		return nil, err
	}
	messages, err := r.storage.GetThread(ctx, in.ChatId, in.MessageId, MaxThreadDepth)
	if err != nil {
		return nil, err
	}
	return formatMessages(chat, messages), nil
}

func formatMessages(chat models.Chat, messages []models.Message) []MessageOutput {
	result := make([]MessageOutput, 0, len(messages))
	for _, m := range messages {
		result = append(result, MessageOutput{
			Id:               m.Id,
			Sender:           m.SenderName,
			Ts:               m.Ts.Format(time.RFC3339),
			ReplyToMessageId: m.ReplyToMessageId,
			Text:             m.Text,
			Link:             storage.MessageLink(chat, m.Id),
		})
	}
	return result
}

func ParseDate(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		ts, err := time.ParseInLocation(layout, raw, time.UTC)
		if err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format %q", raw)
}
