package messages

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/internal/native"
	"github.com/standards-lab/go-ai/protocol"
)

// EncodeRequest encodes req, which the client has validated, as a Messages request body, and
// sets the anthropic-version header to the version the format speaks, 2026-10-21. MaxOutputTokens
// goes out as max_tokens, which Messages requires, so a request with no MaxOutputTokens is
// refused; any positive MaxOutputTokens goes out as it is.
//
// Messages takes system content only in the top-level system, never as a message. Instructions
// alone go out as the system string. The system and developer messages that lead the input,
// before any other item, are hoisted into it, Messages having no developer role: system then goes
// out as an array of text blocks, a text block of the instructions first if there are any, then
// one of each hoisted message's input_text parts, in order. A system or developer message after
// the conversation has begun has no place in a Messages request, and is refused.
//
// The other input items go out as messages:
//
//	user message          a user message of text and image blocks
//	assistant run         one assistant message: thinking and redacted_thinking blocks from the
//	                      reasoning items that open the run, text blocks from its assistant
//	                      message, an output_text or a refusal each, and tool_use blocks from
//	                      the function_calls after it
//	function_call_output  a tool_result block of a user message
//
// Every message's content is an array of blocks, even of one text block.
//
// A user turn is a run of adjacent user messages and function_call_outputs, and goes out as one
// user message: every tool_result block first, in order, then the messages' blocks, in order, as
// Messages requires a user message's tool_result blocks to lead it. A tool_result's tool_use_id
// is the output's call_id, and its content the output string, or the output's parts as text
// blocks.
//
// An assistant run opens at a reasoning item, at an assistant message no reasoning item opened,
// or at a function_call no assistant message or reasoning item opened, and takes the
// function_calls that follow it, so its blocks go out thinking first, then text, then tool_use.
// A reasoning item always opens a new run, joined by the reasoning items right after it. A
// tool_use block's id is the call's call_id, its name the call's, and its input the call's
// arguments as JSON, with empty arguments, a call of none, sent as {}.
//
// A reasoning item goes out through its encrypted_content alone: the envelope DecodeResponse
// wrote there, which holds the reply's thinking block, its text and signature, or its
// redacted_thinking block, its data, each sent back as it came. Its content, which the spec's
// input reasoning item lacks, is never sent. A reasoning item whose encrypted_content isn't this
// format's envelope, being empty, another format's, another version's, or malformed, is dropped:
// it is never sent, and it is never an error, so a conversation can carry another format's
// reasoning through this one. So is a run of reasoning that no assistant message or
// function_call follows, as Chat Completions drops it: llama.cpp's router makes an assistant
// message of it, which the reasoning alone leaves without content.
//
// An input_image whose URL is a base64 data URL, as protocol.DataURL builds, goes out as an image
// block of a base64 source, its media type the data URL's; one of any other URL goes out as an
// image block of a url source, which the endpoint fetches. A data URL that isn't base64, or has
// no media type, is refused, as a base64 source needs both. An input_audio part has no Messages
// form, and is refused.
//
// Tools go out as custom tools: name, description, input_schema from Parameters, and strict. A
// tool of no Parameters goes out with input_schema {"type":"object"}, a function of no arguments,
// as Messages requires input_schema. The tool choice goes out as tool_choice: auto as
// {"type":"auto"}, required as {"type":"any"}, none as {"type":"none"}, and a named function as
// {"type":"tool","name"}. Messages has no allowed-tools choice, so an allowed_tools choice sends
// only the tools it allows, in the request's order, with its mode mapped as a mode is.
//
// ParallelToolCalls false goes out as tool_choice's disable_parallel_tool_use true: on the tool
// choice's tool_choice, or on {"type":"auto"}, the spec's default choice, when the request sets
// none. True is the spec's default, so it sends nothing, as nil does. The spec defines
// disable_parallel_tool_use on auto, any, and tool alone, so a tool choice of none, under which
// no tool is called, sends none, and so does a request that sends no tools, as tool_choice is
// only for tools. A Native tool_choice beside a ParallelToolCalls is refused, clashing with the
// member ParallelToolCalls sets, whether or not this request sends it.
//
// Reasoning's effort none goes out as thinking {"type":"disabled"}, and low, medium, high, xhigh,
// and max as thinking {"type":"adaptive"} with output_config.effort of the same name, the
// spec's efforts. Messages has no minimal effort, so minimal is refused. A reasoning summary, of
// any detail, goes out as the adaptive thinking's display "summarized", Messages' one display of
// summarized thinking; a request that sends no adaptive thinking, of effort empty or none, has no
// thinking to display, and its summary is dropped. Text's JSON-schema format goes out as
// output_config.format, {"type":"json_schema","schema"}; plain text, the endpoint's default,
// sends nothing.
//
// The format's Options, the deployment's profile, adjust what goes out. An effort among
// RefusedEfforts is refused, a declared gap, Declared true, unless Messages lacks it, minimal,
// which stays the spec's refusal, listed or not. Either effort refusal comes ahead of the
// format's other refusals.
//
// The hints Messages has no form for are dropped, never refused: an image's detail; a reasoning
// summary's detail, and a summary of no adaptive thinking; ParallelToolCalls false where no tool
// can be called; text's verbosity; and a JSON-schema format's name, description, and strict flag.
//
// The request is unstreamed, sent with stream false; EncodeStreamRequest streams it. Native's
// members, the native tier, are merged into the top level, and a member named like one the typed
// fields set is refused, as is stream, which is always set. The header is set only once the body
// is encoded, so a refused request adds none.
func (f *Format) EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	return f.encode(req, false, header)
}

// EncodeStreamRequest encodes req as EncodeRequest does, as a streamed request: with stream
// true, which asks for the reply as server-sent events. It refuses what EncodeRequest refuses,
// a request with no MaxOutputTokens among them, and a Native stream member, which clashes with
// the stream member the body always sets. It sets the anthropic-version header as EncodeRequest
// does.
func (f *Format) EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	return f.encode(req, true, header)
}

func (f *Format) encode(req *protocol.Request, stream bool, header http.Header) ([]byte, error) {
	b, err := f.encodeRequest(req, stream)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	header.Set("anthropic-version", apiVersion)
	return b, nil
}

// See docs/design.md, "Implementation notes".
func (f *Format) encodeRequest(req *protocol.Request, stream bool) ([]byte, error) {
	switch e := req.Reasoning.Effort; {
	case e == protocol.ReasoningEffortMinimal:
		return nil, refuse("reasoning effort", string(e),
			fmt.Sprintf("reasoning effort %s has no Messages form", e))
	case e != "" && slices.Contains(f.opts.RefusedEfforts, e):
		return nil, declare("reasoning effort", string(e),
			fmt.Sprintf("reasoning effort %s is refused by the deployment, as its profile declares", e))
	}
	system, err := encodeSystem(req.Instructions, req.Input)
	if err != nil {
		return nil, err
	}
	messages, err := encodeInput(req.Input)
	if err != nil {
		return nil, err
	}
	if req.MaxOutputTokens == 0 {
		return nil, refuse("max output tokens", "", "request has no max output tokens, which Messages requires as max_tokens")
	}
	body := request{
		Model:     req.Model,
		MaxTokens: req.MaxOutputTokens,
		System:    system,
		Stream:    stream,
		Native:    req.Native,
		Messages:  messages,
	}
	if body.Tools, body.ToolChoice, err = encodeTools(req.Tools, req.ToolChoice); err != nil {
		return nil, err
	}
	if req.ParallelToolCalls != nil && native.Has(req.Native, "tool_choice") {
		clash := &native.ClashError{Member: "tool_choice"}
		return nil, refuse("native member", clash.Member, clash.Error())
	}
	if p := req.ParallelToolCalls; p != nil && !*p && len(body.Tools) > 0 {
		if body.ToolChoice == nil {
			body.ToolChoice = &toolChoice{Type: "auto"}
		}
		body.ToolChoice.DisableParallelToolUse = body.ToolChoice.Type != "none"
	}
	var config outputConfig
	switch effort := req.Reasoning.Effort; effort {
	case "":
	case protocol.ReasoningEffortNone:
		body.Thinking = &thinking{Type: "disabled"}
	default:
		body.Thinking = &thinking{Type: "adaptive"}
		if req.Reasoning.Summary != "" {
			body.Thinking.Display = "summarized"
		}
		config.Effort = string(effort)
	}
	switch f := req.Text.Format.(type) {
	case nil, protocol.PlainTextFormat:
	case protocol.JSONSchemaFormat:
		config.Format = &outputFormat{Type: "json_schema", Schema: f.Schema}
	default:
		return nil, refuse("text format", fmt.Sprintf("%T", f), fmt.Sprintf("text format %T has no Messages form", f))
	}
	if config != (outputConfig{}) {
		body.OutputConfig = &config
	}
	b, err := native.Marshal(body)
	if clash, ok := errors.AsType[*native.ClashError](err); ok {
		return nil, refuse("native member", clash.Member, clash.Error())
	}
	return b, err
}

func isSystem(it protocol.Item) bool {
	m, ok := it.(protocol.Message)
	return ok && (m.Role == protocol.RoleSystem || m.Role == protocol.RoleDeveloper)
}

func encodeSystem(instructions string, items []protocol.Item) (any, error) {
	n := 0
	for n < len(items) && isSystem(items[n]) {
		n++
	}
	if n == 0 {
		if instructions == "" {
			return nil, nil
		}
		return instructions, nil
	}
	var blocks []any
	if instructions != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: instructions})
	}
	for i, it := range items[:n] {
		b, err := encodeContent(it.(protocol.Message).Content)
		if err != nil {
			return nil, fmt.Errorf("input %d: %w", i, err)
		}
		blocks = append(blocks, b...)
	}
	return blocks, nil
}

func encodeInput(items []protocol.Item) ([]message, error) {
	var messages []message
	var results, content []any
	run, reasoning := -1, []any(nil)
	closeTurn := func() {
		if results == nil && content == nil {
			return
		}
		messages = append(messages, message{Role: string(protocol.RoleUser), Content: append(results, content...)})
		results, content = nil, nil
	}
	openRun := func() {
		closeTurn()
		messages = append(messages, message{Role: string(protocol.RoleAssistant), Content: reasoning})
		run, reasoning = len(messages)-1, nil
	}
	inLeadingSystem := true
	for i, it := range items {
		if isSystem(it) {
			if inLeadingSystem {
				continue
			}
			role := it.(protocol.Message).Role
			return nil, fmt.Errorf("input %d: %w", i, refuse("system message placement", string(role),
				fmt.Sprintf("%s message after the conversation has begun has no Messages form, as Messages takes system content only ahead of the messages", role)))
		}
		inLeadingSystem = false
		switch it := it.(type) {
		case protocol.Reasoning:
			b, ok := openEnvelope(it.EncryptedContent)
			if !ok {
				continue
			}
			closeTurn()
			run = -1
			reasoning = append(reasoning, b)
		case protocol.Message:
			blocks, err := encodeContent(it.Content)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			switch it.Role {
			case protocol.RoleUser:
				run, reasoning = -1, nil
				content = append(content, blocks...)
			default:
				openRun()
				messages[run].Content = append(messages[run].Content, blocks...)
			}
		case protocol.FunctionCall:
			if run < 0 {
				openRun()
			}
			args := it.Arguments
			if args == "" {
				args = "{}"
			}
			messages[run].Content = append(messages[run].Content, toolUseBlock{
				Type:  "tool_use",
				ID:    it.CallID,
				Name:  it.Name,
				Input: jsontext.Value(args),
			})
		case protocol.FunctionCallOutput:
			b, err := encodeOutput(it)
			if err != nil {
				return nil, fmt.Errorf("input %d: %w", i, err)
			}
			run, reasoning = -1, nil
			results = append(results, b)
		default:
			return nil, fmt.Errorf("input %d: %w", i, refuse("input item", fmt.Sprintf("%T", it),
				fmt.Sprintf("item %T has no Messages form", it)))
		}
	}
	closeTurn()
	return messages, nil
}

func openEnvelope(encryptedContent string) (any, bool) {
	rest, ok := strings.CutPrefix(encryptedContent, envelopePrefix)
	if !ok {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return nil, false
	}
	var e envelope
	if json.Unmarshal(b, &e) != nil {
		return nil, false
	}
	switch {
	case e.Thinking != nil && e.Signature != nil && e.Data == nil:
		return thinkingBlock{Type: "thinking", Thinking: *e.Thinking, Signature: *e.Signature}, true
	case e.Data != nil && e.Thinking == nil && e.Signature == nil:
		return redactedThinkingBlock{Type: "redacted_thinking", Data: *e.Data}, true
	}
	return nil, false
}

func encodeOutput(o protocol.FunctionCallOutput) (toolResultBlock, error) {
	b := toolResultBlock{Type: "tool_result", ToolUseID: o.CallID, Content: o.Output}
	if len(o.OutputParts) > 0 {
		parts, err := encodeContent(o.OutputParts)
		if err != nil {
			return toolResultBlock{}, fmt.Errorf("output %w", err)
		}
		b.Content = parts
	}
	return b, nil
}

func encodeTools(tools []protocol.FunctionTool, choice protocol.ToolChoice) ([]tool, *toolChoice, error) {
	var wire *toolChoice
	var allowed map[string]bool
	switch c := choice.(type) {
	case nil:
	case protocol.ToolChoiceMode:
		wire = &toolChoice{Type: toolChoiceType(c)}
	case protocol.FunctionToolChoice:
		wire = &toolChoice{Type: "tool", Name: c.Name}
	case protocol.AllowedToolChoice:
		wire = &toolChoice{Type: toolChoiceType(c.Mode)}
		allowed = make(map[string]bool, len(c.Tools))
		for _, f := range c.Tools {
			allowed[f.Name] = true
		}
	default:
		return nil, nil, refuse("tool choice", fmt.Sprintf("%T", c), fmt.Sprintf("tool choice %T has no Messages form", c))
	}
	var out []tool
	for _, t := range tools {
		if allowed != nil && !allowed[t.Name] {
			continue
		}
		schema := t.Parameters
		if len(schema) == 0 {
			schema = jsontext.Value(`{"type":"object"}`)
		}
		out = append(out, tool{Name: t.Name, Description: t.Description, InputSchema: schema, Strict: t.Strict})
	}
	return out, wire, nil
}

func refuse(feature, value, message string) error {
	return &format.UnsupportedError{Format: "messages", Feature: feature, Value: value, Message: message}
}

func declare(feature, value, message string) error {
	return &format.UnsupportedError{Format: "messages", Feature: feature, Value: value, Declared: true, Message: message}
}

func toolChoiceType(m protocol.ToolChoiceMode) string {
	if m == protocol.ToolChoiceRequired {
		return "any"
	}
	return string(m)
}

func encodeContent(parts []protocol.ContentPart) ([]any, error) {
	blocks := make([]any, 0, len(parts))
	for i, p := range parts {
		var b any
		switch p := p.(type) {
		case protocol.InputText:
			b = textBlock{Type: "text", Text: p.Text}
		case protocol.OutputText:
			b = textBlock{Type: "text", Text: p.Text}
		case protocol.Refusal:
			b = textBlock{Type: "text", Text: p.Refusal}
		case protocol.InputImage:
			source, err := encodeImageSource(p.ImageURL)
			if err != nil {
				return nil, fmt.Errorf("content %d: %w", i, err)
			}
			b = imageBlock{Type: "image", Source: source}
		default:
			return nil, fmt.Errorf("content %d: %w", i, refuse("content part", fmt.Sprintf("%T", p),
				fmt.Sprintf("part %T has no Messages form", p)))
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// See docs/design.md, "Implementation notes".
func encodeImageSource(url string) (imageSource, error) {
	rest, ok := strings.CutPrefix(url, "data:")
	if !ok {
		return imageSource{Type: "url", URL: url}, nil
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok {
		return imageSource{}, errors.New("image data URL has no data")
	}
	mediaType, ok := strings.CutSuffix(meta, ";base64")
	if !ok {
		return imageSource{}, refuse("image data URL encoding", "", "image data URL isn't base64, as a base64 source requires")
	}
	mediaType, _, _ = strings.Cut(mediaType, ";")
	if mediaType == "" {
		return imageSource{}, refuse("image data URL media type", "",
			"image data URL has no media type, which a base64 source requires")
	}
	return imageSource{Type: "base64", MediaType: mediaType, Data: data}, nil
}
