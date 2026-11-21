package messages

import "encoding/json/jsontext"

type request struct {
	Model        string         `json:"model"`
	MaxTokens    int            `json:"max_tokens"`
	System       any            `json:"system,omitzero"`
	Messages     []message      `json:"messages"`
	Tools        []tool         `json:"tools,omitzero"`
	ToolChoice   *toolChoice    `json:"tool_choice,omitzero"`
	Thinking     *thinking      `json:"thinking,omitzero"`
	OutputConfig *outputConfig  `json:"output_config,omitzero"`
	Stream       bool           `json:"stream"`
	Native       jsontext.Value `json:",embed"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	InputSchema jsontext.Value `json:"input_schema"`
	Strict      bool           `json:"strict,omitzero"`
}

type toolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitzero"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitzero"`
}

type thinking struct {
	Type    string `json:"type"`
	Display string `json:"display,omitzero"`
}

type outputConfig struct {
	Effort string        `json:"effort,omitzero"`
	Format *outputFormat `json:"format,omitzero"`
}

type outputFormat struct {
	Type   string         `json:"type"`
	Schema jsontext.Value `json:"schema"`
}

type message struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type thinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

type redactedThinkingBlock struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type toolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input jsontext.Value `json:"input"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"`
}

type imageBlock struct {
	Type   string      `json:"type"`
	Source imageSource `json:"source"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitzero"`
	Data      string `json:"data,omitzero"`
	URL       string `json:"url,omitzero"`
}

type response struct {
	ID         string  `json:"id"`
	Model      string  `json:"model"`
	Content    []block `json:"content"`
	StopReason string  `json:"stop_reason"`
	Usage      usage   `json:"usage"`
}

type block struct {
	Type      string         `json:"type"`
	Text      string         `json:"text"`
	Thinking  string         `json:"thinking"`
	Signature string         `json:"signature"`
	Data      string         `json:"data"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     jsontext.Value `json:"input"`
}

// See docs/design.md, "Implementation notes".
type usage struct {
	InputTokens              *int                `json:"input_tokens"`
	CacheReadInputTokens     *int                `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int                `json:"cache_creation_input_tokens"`
	OutputTokens             *int                `json:"output_tokens"`
	OutputTokensDetails      outputTokensDetails `json:"output_tokens_details"`
}

type outputTokensDetails struct {
	ThinkingTokens *int `json:"thinking_tokens"`
}

func (u *usage) update(next usage) {
	for _, c := range []struct{ to, from **int }{
		{&u.InputTokens, &next.InputTokens},
		{&u.CacheReadInputTokens, &next.CacheReadInputTokens},
		{&u.CacheCreationInputTokens, &next.CacheCreationInputTokens},
		{&u.OutputTokens, &next.OutputTokens},
		{&u.OutputTokensDetails.ThinkingTokens, &next.OutputTokensDetails.ThinkingTokens},
	} {
		if *c.from != nil {
			*c.to = *c.from
		}
	}
}

const envelopePrefix = "go-ai:messages/v1:"

// See docs/design.md, "Implementation notes".
type envelope struct {
	Thinking  *string `json:"thinking,omitzero"`
	Signature *string `json:"signature,omitzero"`
	Data      *string `json:"data,omitzero"`
}

type streamEvent struct {
	Type         string         `json:"type"`
	Message      jsontext.Value `json:"message"`
	Index        int            `json:"index"`
	ContentBlock block          `json:"content_block"`
	Delta        streamDelta    `json:"delta"`
	Usage        usage          `json:"usage"`
}

type streamMessage struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage usage  `json:"usage"`
}

type streamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}
