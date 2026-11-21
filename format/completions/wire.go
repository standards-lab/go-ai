package completions

import "encoding/json/jsontext"

// See docs/design.md, "Implementation notes".
type request struct {
	Model               string          `json:"model"`
	Messages            []message       `json:"messages"`
	Tools               []tool          `json:"tools,omitzero"`
	ToolChoice          jsontext.Value  `json:"tool_choice,omitzero"`
	ReasoningEffort     string          `json:"reasoning_effort,omitzero"`
	ResponseFormat      *responseFormat `json:"response_format,omitzero"`
	Verbosity           string          `json:"verbosity,omitzero"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitzero"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitzero"`
	Stream              bool            `json:"stream"`
	StreamOptions       *streamOptions  `json:"stream_options,omitzero"`
	Native              jsontext.Value  `json:",embed"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type message struct {
	Role             string         `json:"role"`
	Content          jsontext.Value `json:"content,omitzero"`
	ReasoningContent string         `json:"reasoning_content,omitzero"`
	ToolCalls        []toolCall     `json:"tool_calls,omitzero"`
	ToolCallID       string         `json:"tool_call_id,omitzero"`
}

type tool struct {
	Type     string       `json:"type"`
	Function functionTool `json:"function"`
}

type functionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Parameters  jsontext.Value `json:"parameters,omitzero"`
	Strict      bool           `json:"strict,omitzero"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type namedToolChoice struct {
	Type     string       `json:"type"`
	Function functionName `json:"function"`
}

type functionName struct {
	Name string `json:"name"`
}

type allowedToolsChoice struct {
	Type         string       `json:"type"`
	AllowedTools allowedTools `json:"allowed_tools"`
}

type allowedTools struct {
	Mode  string            `json:"mode"`
	Tools []namedToolChoice `json:"tools"`
}

type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitzero"`
}

type jsonSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Schema      jsontext.Value `json:"schema"`
	Strict      bool           `json:"strict,omitzero"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imagePart struct {
	Type     string   `json:"type"`
	ImageURL imageURL `json:"image_url"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitzero"`
}

type audioPart struct {
	Type       string     `json:"type"`
	InputAudio inputAudio `json:"input_audio"`
}

type inputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

type response struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			Reasoning        string     `json:"reasoning"`
			Refusal          string     `json:"refusal"`
			ToolCalls        []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}

type usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	ReasoningTokens         *int `json:"reasoning_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type chunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			ReasoningContent string          `json:"reasoning_content"`
			Reasoning        string          `json:"reasoning"`
			Content          string          `json:"content"`
			Refusal          string          `json:"refusal"`
			ToolCalls        []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage         `json:"usage"`
	Error jsontext.Value `json:"error"`
}

func (c chunk) claimsNothing() bool {
	return c.ID == "" && len(c.Choices) == 0
}

type toolCallDelta struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

const envelopePrefix = "go-ai:completions/v1:"

// See docs/design.md, "Implementation notes".
type envelope struct {
	ReasoningContent string `json:"reasoning_content"`
}
