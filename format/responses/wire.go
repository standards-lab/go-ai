package responses

import "encoding/json/jsontext"

var include = []string{"reasoning.encrypted_content"}

// See docs/design.md, "Implementation notes".
type request struct {
	Model             string          `json:"model"`
	Instructions      string          `json:"instructions,omitzero"`
	Input             []any           `json:"input"`
	Tools             []functionTool  `json:"tools,omitzero"`
	ToolChoice        any             `json:"tool_choice,omitzero"`
	Reasoning         *reasoningParam `json:"reasoning,omitzero"`
	Text              *textParam      `json:"text,omitzero"`
	MaxOutputTokens   int             `json:"max_output_tokens,omitzero"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitzero"`
	Stream            bool            `json:"stream"`
	Store             bool            `json:"store"`
	Include           []string        `json:"include"`
	Native            jsontext.Value  `json:",embed"`
}

type textParam struct {
	Format    *textFormat `json:"format,omitzero"`
	Verbosity string      `json:"verbosity,omitzero"`
}

type textFormat struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitzero"`
	Description string         `json:"description,omitzero"`
	Schema      jsontext.Value `json:"schema,omitzero"`
	Strict      bool           `json:"strict,omitzero"`
}

type functionTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitzero"`
	Parameters  jsontext.Value `json:"parameters,omitzero"`
	Strict      bool           `json:"strict"`
}

type specificFunction struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type allowedTools struct {
	Type  string             `json:"type"`
	Tools []specificFunction `json:"tools"`
	Mode  string             `json:"mode"`
}

type reasoningParam struct {
	Effort  string `json:"effort,omitzero"`
	Summary string `json:"summary,omitzero"`
}

type functionCallItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitzero"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCallOutputItem struct {
	Type   string `json:"type"`
	ID     string `json:"id,omitzero"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

type reasoningItem struct {
	Type             string     `json:"type"`
	ID               string     `json:"id,omitzero"`
	Summary          []textPart `json:"summary"`
	Content          []textPart `json:"content,omitzero"`
	EncryptedContent string     `json:"encrypted_content,omitzero"`
}

type messageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitzero"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imagePart struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url"`
	Detail   string `json:"detail,omitzero"`
}

type refusalPart struct {
	Type    string `json:"type"`
	Refusal string `json:"refusal"`
}

const audioType = "go-ai:input_audio"

type audioPart struct {
	Type   string `json:"type"`
	Data   string `json:"data"`
	Format string `json:"format"`
}

type response struct {
	ID                string             `json:"id"`
	Model             string             `json:"model"`
	Status            string             `json:"status"`
	Output            []outputItem       `json:"output"`
	IncompleteDetails *incompleteDetails `json:"incomplete_details"`
	Error             *responseError     `json:"error"`
	Usage             usage              `json:"usage"`
}

type outputItem struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	Status           string `json:"status"`
	Role             string `json:"role"`
	Content          []part `json:"content"`
	Summary          []part `json:"summary"`
	EncryptedContent string `json:"encrypted_content"`
	CallID           string `json:"call_id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
}

type part struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

type incompleteDetails struct {
	Reason string `json:"reason"`
}

type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type usage struct {
	InputTokens         int                 `json:"input_tokens"`
	OutputTokens        int                 `json:"output_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	InputTokensDetails  inputTokensDetails  `json:"input_tokens_details"`
	OutputTokensDetails outputTokensDetails `json:"output_tokens_details"`
}

type inputTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type outputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

const envelopePrefix = "go-ai:responses/v1:"

// See docs/design.md, "Implementation notes".
type envelope struct {
	EncryptedContent *string   `json:"encrypted_content,omitzero"`
	Content          *[]string `json:"content,omitzero"`
}
