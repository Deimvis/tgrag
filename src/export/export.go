package export

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Deimvis/tgrag/src/models"
)

type ChatExport struct {
	Chat     models.Chat
	Messages []models.Message
}

type rawRoot struct {
	Chats *struct {
		List []rawChat `json:"list"`
	} `json:"chats"`
	rawChat
}

type rawChat struct {
	Id       int64        `json:"id"`
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	Messages []rawMessage `json:"messages"`
}

type rawMessage struct {
	Id               int64           `json:"id"`
	Type             string          `json:"type"`
	DateUnixtime     string          `json:"date_unixtime"`
	Date             string          `json:"date"`
	Edited           string          `json:"edited"`
	From             string          `json:"from"`
	FromId           string          `json:"from_id"`
	ReplyToMessageId int64           `json:"reply_to_message_id"`
	Text             json.RawMessage `json:"text"`
}

func Parse(r io.Reader) ([]ChatExport, error) {
	var root rawRoot
	err := json.NewDecoder(r).Decode(&root)
	if err != nil {
		return nil, fmt.Errorf("decode export: %w", err)
	}
	var rawChats []rawChat
	if root.Chats != nil {
		rawChats = root.Chats.List
	} else {
		rawChats = []rawChat{root.rawChat}
	}
	result := make([]ChatExport, 0, len(rawChats))
	for _, rc := range rawChats {
		chatExport, err := convertChat(rc)
		if err != nil {
			return nil, err
		}
		result = append(result, chatExport)
	}
	return result, nil
}

func convertChat(rc rawChat) (ChatExport, error) {
	kind := chatKind(rc.Type)
	chat := models.Chat{Id: rc.Id, Kind: kind, Title: rc.Name}
	messages := make([]models.Message, 0, len(rc.Messages))
	for _, rm := range rc.Messages {
		if rm.Type != "message" {
			continue
		}
		m, err := convertMessage(rc.Id, rm)
		if err != nil {
			return ChatExport{}, fmt.Errorf("chat %d message %d: %w", rc.Id, rm.Id, err)
		}
		messages = append(messages, m)
		if m.Ts.After(chat.LatestMessageTs) {
			chat.LatestMessageTs = m.Ts
		}
	}
	chat.MessageCount = int64(len(messages))
	return ChatExport{Chat: chat, Messages: messages}, nil
}

func chatKind(exportType string) models.ChatKind {
	switch {
	case strings.Contains(exportType, "channel"):
		return models.ChatKindChannel
	case strings.Contains(exportType, "group"):
		return models.ChatKindGroup
	default:
		return models.ChatKindPrivate
	}
}

func convertMessage(chatId int64, rm rawMessage) (models.Message, error) {
	ts, err := parseTs(rm.DateUnixtime, rm.Date)
	if err != nil {
		return models.Message{}, err
	}
	var editTs time.Time
	if rm.Edited != "" {
		editTs, err = parseTs("", rm.Edited)
		if err != nil {
			return models.Message{}, err
		}
	}
	text, err := parseText(rm.Text)
	if err != nil {
		return models.Message{}, err
	}
	return models.Message{
		ChatId:           chatId,
		Id:               rm.Id,
		SenderUserId:     parseSenderId(rm.FromId),
		SenderName:       rm.From,
		Ts:               ts,
		EditTs:           editTs,
		ReplyToMessageId: rm.ReplyToMessageId,
		Text:             text,
	}, nil
}

func parseTs(unixtime string, date string) (time.Time, error) {
	if unixtime != "" {
		sec, err := strconv.ParseInt(unixtime, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse date_unixtime: %w", err)
		}
		return time.Unix(sec, 0).UTC(), nil
	}
	ts, err := time.ParseInLocation("2006-01-02T15:04:05", date, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date: %w", err)
	}
	return ts.UTC(), nil
}

func parseSenderId(fromId string) int64 {
	digits := strings.TrimLeftFunc(fromId, func(r rune) bool {
		return r < '0' || r > '9'
	})
	id, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func parseText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var plain string
	err := json.Unmarshal(raw, &plain)
	if err == nil {
		return plain, nil
	}
	var parts []json.RawMessage
	err = json.Unmarshal(raw, &parts)
	if err != nil {
		return "", fmt.Errorf("parse text: %w", err)
	}
	var sb strings.Builder
	for _, part := range parts {
		var s string
		err = json.Unmarshal(part, &s)
		if err == nil {
			sb.WriteString(s)
			continue
		}
		var entity struct {
			Text string `json:"text"`
		}
		err = json.Unmarshal(part, &entity)
		if err != nil {
			return "", fmt.Errorf("parse text entity: %w", err)
		}
		sb.WriteString(entity.Text)
	}
	return sb.String(), nil
}
