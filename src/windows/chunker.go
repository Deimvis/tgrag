package windows

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Deimvis/tgrag/src/models"
)

type Params struct {
	Gap      time.Duration
	MaxChars int
}

func Build(chatId int64, messages []models.Message, params Params) []models.Window {
	sorted := make([]models.Message, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Text) != "" {
			sorted = append(sorted, m)
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Id < sorted[j].Id
	})

	var result []models.Window
	var current []models.Message
	currentChars := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		result = append(result, newWindow(chatId, current))
		current = nil
		currentChars = 0
	}
	for _, m := range sorted {
		lineChars := len(formatLine(m))
		if len(current) > 0 {
			isOverLimit := currentChars+lineChars > params.MaxChars
			isGapExceeded := m.Ts.Sub(current[len(current)-1].Ts) > params.Gap
			isReplyIntoCurrent := m.ReplyToMessageId != 0 && containsMessage(current, m.ReplyToMessageId)
			if isOverLimit || (isGapExceeded && !isReplyIntoCurrent) {
				flush()
			}
		}
		current = append(current, m)
		currentChars += lineChars
	}
	flush()
	return result
}

func containsMessage(messages []models.Message, id int64) bool {
	for _, m := range messages {
		if m.Id == id {
			return true
		}
	}
	return false
}

func newWindow(chatId int64, messages []models.Message) models.Window {
	lines := make([]string, 0, len(messages))
	participantSet := map[string]struct{}{}
	for _, m := range messages {
		lines = append(lines, formatLine(m))
		if m.SenderName != "" {
			participantSet[m.SenderName] = struct{}{}
		}
	}
	participants := make([]string, 0, len(participantSet))
	for p := range participantSet {
		participants = append(participants, p)
	}
	sort.Strings(participants)
	return models.Window{
		ChatId:         chatId,
		FirstMessageId: messages[0].Id,
		LastMessageId:  messages[len(messages)-1].Id,
		FirstTs:        messages[0].Ts,
		LastTs:         messages[len(messages)-1].Ts,
		Participants:   participants,
		Text:           strings.Join(lines, "\n"),
	}
}

func formatLine(m models.Message) string {
	return fmt.Sprintf("%s: %s", m.SenderName, strings.TrimSpace(m.Text))
}
