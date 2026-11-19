package aitest

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ChatRequest is a request to the Chat Completions route, decoded, beside its body as it
// arrived.
type ChatRequest struct {
	// Arrival is the request's place among the route's requests, from 1.
	Arrival int
	// Body is the request's body, as it arrived.
	Body []byte
	// Model is the request's model.
	Model string
	// Stream is the request's stream flag.
	Stream bool
	// MaxTokens is the request's max_completion_tokens, or its max_tokens when it has none;
	// zero when it has neither.
	MaxTokens int
	// ReasoningEffort is the request's reasoning_effort, empty when it has none.
	ReasoningEffort string
	// SchemaName is the name of the request's response_format json_schema, empty when it asks
	// for no JSON schema.
	SchemaName string
	// Messages are the request's messages, in order.
	Messages []ChatMessage
}

// ChatMessage is one message of a [ChatRequest]: its role and the content its parts carry, each
// kind in the order of its parts. A refusal part, which only a past assistant message has, is
// read and set aside.
type ChatMessage struct {
	// Role is the message's role, such as system, user, assistant, or tool.
	Role string
	// Text is the message's text: its content when that is a string, or its text parts joined
	// in order with nothing between them.
	Text string
	// Images are the message's image_url parts.
	Images []ChatImage
	// Audio are the message's input_audio parts.
	Audio []ChatAudio
}

// ChatImage is an image_url part of a [ChatMessage].
type ChatImage struct {
	// MediaType is a data URL's media type, such as image/png, its parameters kept and its
	// ;base64 dropped; empty for a remote URL.
	MediaType string
	// Data is a data URL's bytes, decoded from base64 or from percent-encoding; nil for a remote
	// URL.
	Data []byte
	// URL is the image's URL when it isn't a data URL, empty for one.
	URL string
}

// ChatAudio is an input_audio part of a [ChatMessage].
type ChatAudio struct {
	// Format is the clip's format, such as wav or mp3.
	Format string
	// Data is the clip's bytes, decoded from base64.
	Data []byte
}

// chatWire is the part of a Chat Completions request body ChatRequest decodes.
type chatWire struct {
	Model               string `json:"model"`
	Stream              bool   `json:"stream"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
	MaxTokens           int    `json:"max_tokens"`
	ReasoningEffort     string `json:"reasoning_effort"`
	StreamOptions       struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	ResponseFormat struct {
		JSONSchema struct {
			Name string `json:"name"`
		} `json:"json_schema"`
	} `json:"response_format"`
	Messages []struct {
		Role    string         `json:"role"`
		Content jsontext.Value `json:"content"`
	} `json:"messages"`
}

// chatPartWire is one part of a message's content array.
type chatPartWire struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
	InputAudio struct {
		Data   string `json:"data"`
		Format string `json:"format"`
	} `json:"input_audio"`
}

// decodeChatRequest decodes a Chat Completions request body, and what it asks of its reply's
// shape. A content part of a type it doesn't know, or an image or clip whose data doesn't decode,
// is an error, as is a body that isn't the request's JSON.
func decodeChatRequest(body []byte) (ChatRequest, asked, error) {
	var wire chatWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return ChatRequest{}, asked{}, fmt.Errorf("decode chat request: %w", err)
	}
	req := ChatRequest{
		Body:            body,
		Model:           wire.Model,
		Stream:          wire.Stream,
		MaxTokens:       wire.MaxCompletionTokens,
		ReasoningEffort: wire.ReasoningEffort,
		SchemaName:      wire.ResponseFormat.JSONSchema.Name,
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = wire.MaxTokens
	}
	for i, m := range wire.Messages {
		msg, err := decodeChatContent(m.Content)
		if err != nil {
			return ChatRequest{}, asked{}, fmt.Errorf("decode chat request's message %d: %w", i, err)
		}
		msg.Role = m.Role
		req.Messages = append(req.Messages, msg)
	}
	a := asked{model: wire.Model, stream: wire.Stream, streamUsage: wire.StreamOptions.IncludeUsage}
	return req, a, nil
}

// decodeChatContent decodes a message's content: absent or null, a string, or an array of parts.
func decodeChatContent(content jsontext.Value) (ChatMessage, error) {
	var msg ChatMessage
	switch content.Kind() {
	case 0, 'n':
		return msg, nil
	case '"':
		err := json.Unmarshal(content, &msg.Text)
		return msg, err
	}
	var parts []chatPartWire
	if err := json.Unmarshal(content, &parts); err != nil {
		return msg, err
	}
	var text strings.Builder
	for i, p := range parts {
		switch p.Type {
		case "text":
			text.WriteString(p.Text)
		case "image_url":
			img, err := decodeImageURL(p.ImageURL.URL)
			if err != nil {
				return msg, fmt.Errorf("part %d: %w", i, err)
			}
			msg.Images = append(msg.Images, img)
		case "input_audio":
			data, err := base64.StdEncoding.DecodeString(p.InputAudio.Data)
			if err != nil {
				return msg, fmt.Errorf("part %d: audio data: %w", i, err)
			}
			msg.Audio = append(msg.Audio, ChatAudio{Format: p.InputAudio.Format, Data: data})
		case "refusal":
		default:
			return msg, fmt.Errorf("part %d has unknown type %q", i, p.Type)
		}
	}
	msg.Text = text.String()
	return msg, nil
}

// decodeImageURL decodes an image_url part's URL: a data URL, data:[<media type>][;base64],<data>,
// to its media type and bytes, or any other URL as given.
func decodeImageURL(u string) (ChatImage, error) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return ChatImage{URL: u}, nil
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok {
		return ChatImage{}, errors.New("image data URL has no comma")
	}
	if mediaType, ok := strings.CutSuffix(meta, ";base64"); ok {
		b, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return ChatImage{}, fmt.Errorf("image data URL: %w", err)
		}
		return ChatImage{MediaType: mediaType, Data: b}, nil
	}
	b, err := url.PathUnescape(data)
	if err != nil {
		return ChatImage{}, fmt.Errorf("image data URL: %w", err)
	}
	return ChatImage{MediaType: meta, Data: []byte(b)}, nil
}

// ChatReply is the Chat Completions route's reply: an assistant message of text or a refusal,
// its finish reason, and its usage. It is unstreamed, a chat.completion, unless the request asked
// to stream; then it is a stream of chat.completion.chunk events: a chunk of the assistant's
// role, a chunk for each piece of the text, one of the refusal when there is one, one of the
// finish reason, one of the usage, of no choices, when the request's stream_options asked for it
// with include_usage, as go-ai's Chat Completions client always does, and data [DONE]. Its id is
// chatcmpl-aitest- and the request's arrival number, and its model the request's.
type ChatReply struct {
	// Text is the message's text, used when Chunks is nil; empty, the message has no content.
	Text string
	// Chunks, when not nil, is the message's text in pieces, each a chunk of a stream and the
	// whole joined in order, Text unused.
	Chunks []string
	// Refusal, when not empty, is the message's refusal.
	Refusal string
	// Finish is the reply's finish_reason, such as stop, length, or content_filter, written as
	// given; empty is stop.
	Finish string
	// PromptTokens is the reply's usage.prompt_tokens.
	PromptTokens int
	// CompletionTokens is the reply's usage.completion_tokens.
	CompletionTokens int
	// TotalTokens is the reply's usage.total_tokens.
	TotalTokens int
	// BreakAfter, when positive, breaks a stream after its first BreakAfter pieces of text: the
	// reply's body ends there, with no refusal, finish reason, usage, or [DONE]. An unstreamed
	// reply ignores it.
	BreakAfter int
}

// chatUsage is a chat reply's usage.
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (r ChatReply) write(w http.ResponseWriter, a asked) {
	if a.stream {
		r.stream(w, a)
		return
	}
	type message struct {
		Role    string  `json:"role"`
		Content *string `json:"content"`
		Refusal *string `json:"refusal"`
	}
	type choice struct {
		Index        int     `json:"index"`
		Message      message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	}
	msg := message{Role: "assistant"}
	if text := r.text(); text != "" {
		msg.Content = &text
	}
	if r.Refusal != "" {
		msg.Refusal = &r.Refusal
	}
	writeJSON(w, http.StatusOK, struct {
		ID      string    `json:"id"`
		Object  string    `json:"object"`
		Created int       `json:"created"`
		Model   string    `json:"model"`
		Choices []choice  `json:"choices"`
		Usage   chatUsage `json:"usage"`
	}{r.id(a), "chat.completion", 0, a.model, []choice{{Message: msg, FinishReason: r.finish()}}, r.usage()})
}

// stream writes r as the stream ChatReply describes, flushing each event on its own.
func (r ChatReply) stream(w http.ResponseWriter, a asked) {
	type delta struct {
		Role    string `json:"role,omitzero"`
		Content string `json:"content,omitzero"`
		Refusal string `json:"refusal,omitzero"`
	}
	type choice struct {
		Index        int     `json:"index"`
		Delta        delta   `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	}
	type chunk struct {
		ID      string     `json:"id"`
		Object  string     `json:"object"`
		Created int        `json:"created"`
		Model   string     `json:"model"`
		Choices []choice   `json:"choices"`
		Usage   *chatUsage `json:"usage,omitzero"`
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	// send writes one event and flushes it, reporting false once the client is gone.
	send := func(data string) bool {
		if _, err := io.WriteString(w, Frame{Data: data}.encode()); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	sendChunk := func(choices []choice, usage *chatUsage) bool {
		b, err := json.Marshal(chunk{r.id(a), "chat.completion.chunk", 0, a.model, choices, usage})
		if err != nil {
			panic(err) // a chunk is strings, integers, and their structs
		}
		return send(string(b))
	}
	if !sendChunk([]choice{{Delta: delta{Role: "assistant"}}}, nil) {
		return
	}
	pieces := r.Chunks
	if pieces == nil && r.Text != "" {
		pieces = []string{r.Text}
	}
	for i, p := range pieces {
		if r.BreakAfter > 0 && i == r.BreakAfter {
			return
		}
		if !sendChunk([]choice{{Delta: delta{Content: p}}}, nil) {
			return
		}
	}
	if r.BreakAfter > 0 && len(pieces) >= r.BreakAfter {
		return
	}
	if r.Refusal != "" && !sendChunk([]choice{{Delta: delta{Refusal: r.Refusal}}}, nil) {
		return
	}
	finish := r.finish()
	if !sendChunk([]choice{{FinishReason: &finish}}, nil) {
		return
	}
	if a.streamUsage {
		u := r.usage()
		if !sendChunk([]choice{}, &u) {
			return
		}
	}
	send("[DONE]")
}

// text is the reply's whole text: Chunks joined, or Text when Chunks is nil.
func (r ChatReply) text() string {
	if r.Chunks != nil {
		return strings.Join(r.Chunks, "")
	}
	return r.Text
}

// finish is the reply's finish_reason, stop when Finish is empty.
func (r ChatReply) finish() string {
	if r.Finish == "" {
		return "stop"
	}
	return r.Finish
}

// usage is the reply's usage.
func (r ChatReply) usage() chatUsage {
	return chatUsage{r.PromptTokens, r.CompletionTokens, r.TotalTokens}
}

// id is the reply's id, made from the request's arrival number.
func (ChatReply) id(a asked) string {
	return "chatcmpl-aitest-" + strconv.Itoa(a.arrival)
}
