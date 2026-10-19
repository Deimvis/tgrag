package ask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/Deimvis/tgrag/src/configs"
	"github.com/Deimvis/tgrag/src/tools"
)

const systemPrompt = `You answer questions about the user's personal Telegram chat history using the provided tools.
Search iteratively:
* the search tool has modes: fts (keyword BM25, exact words and prefixes like квартир*), semantic (embedding similarity, finds paraphrases and cross-language matches from a natural-language query) and hybrid (both); the default auto mode picks hybrid when embeddings are enabled; if semantic/hybrid returns an "embeddings are disabled" error, use fts;
* start broad, then try several phrasings, synonyms and morphological variants in both Russian and English (use prefix queries like квартир* to cover word forms in fts mode, plain sentences in semantic mode);
* narrow by chat (list_chats), participant (sender) and date range when the question implies them;
* pull get_context or get_thread around promising hits before answering, never answer from snippets alone;
* run several searches when in doubt, do not stop after the first empty result.
In the answer cite chat title, sender, date and the message link for every claim. Say explicitly when nothing relevant was found.
Today is %s.`

type Agent struct {
	cfg      configs.AskConfig
	client   anthropic.Client
	registry *tools.Registry
	log      io.Writer
}

func New(cfg configs.Config, registry *tools.Registry) *Agent {
	return &Agent{cfg: cfg.Ask, client: anthropic.NewClient(), registry: registry, log: os.Stderr}
}

func (a *Agent) Run(ctx context.Context, question string) (string, error) {
	toolParams := make([]anthropic.ToolUnionParam, 0, len(tools.Definitions()))
	for _, d := range tools.Definitions() {
		toolParams = append(toolParams, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        d.Name,
			Description: anthropic.String(d.Description),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: d.InputSchema["properties"], Required: requiredFields(d.InputSchema)},
		}})
	}
	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(question))}
	system := []anthropic.TextBlockParam{{Text: fmt.Sprintf(systemPrompt, time.Now().Format("2006-01-02"))}}

	for iteration := 0; iteration < a.cfg.MaxIterations; iteration++ {
		resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(a.cfg.Model),
			MaxTokens: 16000,
			System:    system,
			Messages:  messages,
			Tools:     toolParams,
		})
		if err != nil {
			return "", fmt.Errorf("claude request: %w", err)
		}
		messages = append(messages, resp.ToParam())
		if resp.StopReason == anthropic.StopReasonRefusal {
			return "", fmt.Errorf("request refused: %s", resp.StopDetails.Explanation)
		}

		var toolResults []anthropic.ContentBlockParamUnion
		var finalText strings.Builder
		for _, block := range resp.Content {
			switch variant := block.AsAny().(type) {
			case anthropic.TextBlock:
				finalText.WriteString(variant.Text)
			case anthropic.ToolUseBlock:
				rawInput := []byte(variant.JSON.Input.Raw())
				fmt.Fprintf(a.log, "→ %s %s\n", variant.Name, string(rawInput))
				result, err := a.registry.Call(ctx, variant.Name, rawInput)
				if err != nil {
					toolResults = append(toolResults, anthropic.NewToolResultBlock(variant.ID, err.Error(), true))
					continue
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					return "", fmt.Errorf("encode tool result: %w", err)
				}
				toolResults = append(toolResults, anthropic.NewToolResultBlock(variant.ID, string(encoded), false))
			}
		}
		if resp.StopReason != anthropic.StopReasonToolUse {
			return finalText.String(), nil
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
	return "", errors.New("max iterations reached without final answer")
}

func requiredFields(schema map[string]any) []string {
	required, ok := schema["required"].([]string)
	if !ok {
		return nil
	}
	return required
}
