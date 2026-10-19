package models

import "time"

type ChatKind string

const (
	ChatKindPrivate ChatKind = "private"
	ChatKindGroup   ChatKind = "group"
	ChatKindChannel ChatKind = "channel"
)

type Chat struct {
	Id              int64     `json:"id"`
	Kind            ChatKind  `json:"kind"`
	Title           string    `json:"title"`
	Username        string    `json:"username,omitempty"`
	AccessHash      int64     `json:"-"`
	LatestMessageTs time.Time `json:"latest_message_ts"`
	MessageCount    int64     `json:"message_count"`
}

type Message struct {
	ChatId           int64     `json:"chat_id"`
	Id               int64     `json:"id"`
	SenderUserId     int64     `json:"sender_user_id"`
	SenderName       string    `json:"sender_name"`
	Ts               time.Time `json:"ts"`
	EditTs           time.Time `json:"edit_ts,omitempty"`
	ReplyToMessageId int64     `json:"reply_to_message_id,omitempty"`
	Text             string    `json:"text"`
	IsOutgoing       bool      `json:"is_outgoing"`
}

type Window struct {
	Id             int64     `json:"id"`
	ChatId         int64     `json:"chat_id"`
	FirstMessageId int64     `json:"first_message_id"`
	LastMessageId  int64     `json:"last_message_id"`
	FirstTs        time.Time `json:"first_ts"`
	LastTs         time.Time `json:"last_ts"`
	Participants   []string  `json:"participants"`
	Text           string    `json:"text"`
}

type SearchHitSource string

const (
	SearchHitSourceFts      SearchHitSource = "fts"
	SearchHitSourceSemantic SearchHitSource = "semantic"
	SearchHitSourceHybrid   SearchHitSource = "hybrid"
)

type SearchHit struct {
	WindowId       int64           `json:"window_id"`
	ChatId         int64           `json:"chat_id"`
	ChatTitle      string          `json:"chat_title"`
	FirstMessageId int64           `json:"first_message_id"`
	LastMessageId  int64           `json:"last_message_id"`
	FirstTs        time.Time       `json:"first_ts"`
	LastTs         time.Time       `json:"last_ts"`
	Participants   []string        `json:"participants"`
	Snippet        string          `json:"snippet"`
	Source         SearchHitSource `json:"source"`
	Rank           float64         `json:"rank,omitempty"`
	Similarity     float64         `json:"similarity,omitempty"`
	Link           string          `json:"link"`
}

type SearchParams struct {
	Query        string
	ChatId       int64
	SenderName   string
	After        time.Time
	Before       time.Time
	Limit        int
	SnippetChars int
}

type MessageRef struct {
	ChatId int64
	Id     int64
}
