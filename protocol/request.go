package protocol

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/standards-lab/go-ai/internal/native"
)

// Request is one request for a model's response: Open Responses' create-response body, in its
// stateless subset.
type Request struct {
	// Model names the model to respond. It is required.
	Model string
	// Instructions guide the model for this request, ahead of Input.
	Instructions string
	// Input is the conversation so far, oldest item first. It holds at least one item.
	Input []Item
	// Tools are the functions the model may call.
	Tools []FunctionTool
	// ToolChoice controls which of Tools the model calls. Nil leaves it to the endpoint.
	ToolChoice ToolChoice
	// Reasoning configures the model's reasoning. Its zero value leaves it to the endpoint.
	Reasoning ReasoningParam
	// Text configures the model's text output: its format, such as a JSON schema, and its
	// verbosity. Its zero value leaves both to the endpoint.
	Text TextParam
	// MaxOutputTokens bounds the tokens the model generates, reasoning included. Zero leaves
	// the bound to the endpoint, and a negative bound is refused. The least positive bound is
	// each format's: its spec may set a minimum, which the format refuses a bound below before
	// anything is sent.
	MaxOutputTokens int
	// ParallelToolCalls says whether the model may call several of Tools in one turn. Nil
	// leaves it to the host's default.
	ParallelToolCalls *bool
	// Native is the native tier: a JSON object whose members are merged into the top level of
	// the wire body as they are, for a feature of the endpoint the model doesn't cover, such
	// as a sampling parameter. It is outside the standard model, so its members mean what the
	// endpoint makes them mean. A member that clashes with one the typed fields set is
	// refused.
	Native jsontext.Value
}

// ReasoningParam configures a reasoning model's reasoning: the spec's reasoning parameter.
type ReasoningParam struct {
	// Effort is how much the model reasons. Empty leaves it to the endpoint.
	Effort ReasoningEffort
	// Summary asks for a summary of the model's reasoning, and how detailed. Empty asks for
	// none. A format whose API has no form for it drops it.
	Summary ReasoningSummary
}

// ReasoningEffort is how much a model reasons before it answers. More effort may take longer
// and use more output tokens.
type ReasoningEffort string

// The efforts a ReasoningParam takes, least first. An effort is the protocol's when an
// implemented format's spec defines it, and each names the specs that do: the Open Responses
// spec's reasoning.effort, OpenAI Chat Completions' reasoning_effort, and Anthropic Messages'
// output_config.effort. A format refuses an effort its own spec lacks, before anything is sent.
const (
	// ReasoningEffortNone asks the model not to reason. Open Responses' reasoning.effort and
	// Chat Completions' reasoning_effort define it.
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortMinimal asks for the least reasoning short of none. Chat Completions'
	// reasoning_effort defines it.
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	// ReasoningEffortLow asks for little reasoning. Open Responses' reasoning.effort, Chat
	// Completions' reasoning_effort, and Messages' effort define it.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium asks for a moderate amount of reasoning. Open Responses'
	// reasoning.effort, Chat Completions' reasoning_effort, and Messages' effort define it.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh asks for thorough reasoning. Open Responses' reasoning.effort, Chat
	// Completions' reasoning_effort, and Messages' effort define it.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortXHigh asks for more reasoning than high. Open Responses'
	// reasoning.effort, Chat Completions' reasoning_effort, and Messages' effort define it.
	ReasoningEffortXHigh ReasoningEffort = "xhigh"
	// ReasoningEffortMax asks for the most reasoning the model gives. Messages' effort
	// defines it.
	ReasoningEffortMax ReasoningEffort = "max"
)

var reasoningEfforts = []ReasoningEffort{
	ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow, ReasoningEffortMedium,
	ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax,
}

// UnmarshalText decodes an effort, accepting only the protocol's seven and refusing any other
// name, the empty one included, with an error that names it and the efforts accepted. It is
// strict so that a mistyped effort in a configuration, such as a format profile's list of the
// efforts its host refuses, fails to decode instead of silently matching nothing. No format
// decodes a host's reply into a ReasoningEffort, so a host's unknown effort never meets it.
func (e *ReasoningEffort) UnmarshalText(text []byte) error {
	if !slices.Contains(reasoningEfforts, ReasoningEffort(text)) {
		names := make([]string, len(reasoningEfforts))
		for i, known := range reasoningEfforts {
			names[i] = string(known)
		}
		return fmt.Errorf("protocol: unknown reasoning effort %q, want one of %s", text, strings.Join(names, ", "))
	}
	*e = ReasoningEffort(text)
	return nil
}

// ReasoningSummary is how detailed a summary of its reasoning a model gives, alongside the
// reasoning itself.
type ReasoningSummary string

// The summaries a ReasoningParam takes.
const (
	// ReasoningSummaryAuto leaves the summary's detail to the model.
	ReasoningSummaryAuto ReasoningSummary = "auto"
	// ReasoningSummaryConcise asks for a brief summary.
	ReasoningSummaryConcise ReasoningSummary = "concise"
	// ReasoningSummaryDetailed asks for a thorough summary.
	ReasoningSummaryDetailed ReasoningSummary = "detailed"
)

// Validate reports whether the request is one the model can describe, so a caller can check a
// request before sending it; a client validates every request before anything is sent. It
// refuses:
//
//   - nil, no model, no input, or a negative MaxOutputTokens
//   - an item, content part, tool choice, or text format that isn't one of this package's by
//     value (a *Message is refused)
//   - a message with an unknown role or no content, or a part its role doesn't take: the spec
//     gives the user input_text and input_image, the system and developer input_text alone, and
//     the assistant output_text and refusal, and the audio extension, an input part like
//     input_image, is the user's alone too
//   - an image with no URL or an unknown detail, or an audio clip with no data or an unknown
//     format
//   - a function_call with no call id or name, or with arguments that aren't JSON
//   - a function_call_output with no call id, with both output forms, or with a part other than
//     InputText
//   - a tool with no name, a name the spec doesn't allow, a name another tool has, or
//     parameters that aren't a JSON object
//   - a tool choice of an unknown mode, naming a function the request doesn't declare, or of
//     allowed_tools with no tools
//   - a reasoning effort none of the efforts above, or a reasoning summary or a verbosity the
//     spec doesn't define
//   - a json_schema format with no name, a name the spec doesn't allow, or a schema that isn't
//     a JSON object
//   - a Native that isn't a JSON object with unique member names
//
// A reasoning item's fields are all optional, and its Content, which the spec's input item lacks,
// is ignored on input rather than refused, so a response's Output goes back as it came.
//
// It doesn't check whether a Native member clashes with a member the typed fields set: only a
// format knows its wire names, so a clash is refused when the format encodes the request, which
// is still before anything is sent.
func (r *Request) Validate() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	return nil
}

func (r *Request) validate() error {
	switch {
	case r == nil:
		return errors.New("no request")
	case r.Model == "":
		return errors.New("request has no model")
	case len(r.Input) == 0:
		return errors.New("request has no input")
	case r.MaxOutputTokens < 0:
		return fmt.Errorf("request's max output tokens %d is negative", r.MaxOutputTokens)
	}
	if e := r.Reasoning.Effort; e != "" && !slices.Contains(reasoningEfforts, e) {
		return fmt.Errorf("request has unknown reasoning effort %q", e)
	}
	switch r.Reasoning.Summary {
	case "", ReasoningSummaryAuto, ReasoningSummaryConcise, ReasoningSummaryDetailed:
	default:
		return fmt.Errorf("request has unknown reasoning summary %q", r.Reasoning.Summary)
	}
	if err := validateText(r.Text); err != nil {
		return fmt.Errorf("text: %w", err)
	}
	for i, it := range r.Input {
		if err := validateItem(it); err != nil {
			return fmt.Errorf("input %d: %w", i, err)
		}
	}
	declared := make(map[string]bool, len(r.Tools))
	for i, t := range r.Tools {
		if err := validateTool(t, declared); err != nil {
			return fmt.Errorf("tool %d: %w", i, err)
		}
		declared[t.Name] = true
	}
	if r.ToolChoice != nil {
		if err := validateToolChoice(r.ToolChoice, declared); err != nil {
			return fmt.Errorf("tool choice: %w", err)
		}
	}
	if !native.Valid(r.Native) {
		return errors.New("request's native field isn't a JSON object with unique member names")
	}
	return nil
}

func validateItem(it Item) error {
	switch it := it.(type) {
	case Message:
		return validateMessage(it)
	case FunctionCall:
		return validateFunctionCall(it)
	case FunctionCallOutput:
		return validateFunctionCallOutput(it)
	case Reasoning:
		return nil
	default:
		return fmt.Errorf("unsupported item %T", it)
	}
}

func validateMessage(m Message) error {
	switch m.Role {
	case RoleUser, RoleSystem, RoleDeveloper, RoleAssistant:
	default:
		return fmt.Errorf("message has unknown role %q", m.Role)
	}
	if len(m.Content) == 0 {
		return fmt.Errorf("%s message has no content", m.Role)
	}
	for i, p := range m.Content {
		if err := validatePart(m.Role, p); err != nil {
			return fmt.Errorf("%s message content %d: %w", m.Role, i, err)
		}
	}
	return nil
}

func validatePart(role Role, p ContentPart) error {
	switch p := p.(type) {
	case InputText:
		if role != RoleAssistant {
			return nil
		}
	case InputImage:
		if role == RoleUser {
			return validateImage(p)
		}
	case InputAudio:
		if role == RoleUser {
			return validateAudio(p)
		}
	case OutputText, Refusal:
		if role == RoleAssistant {
			return nil
		}
	}
	return fmt.Errorf("unsupported part %T", p)
}

func validateImage(p InputImage) error {
	if p.ImageURL == "" {
		return errors.New("image has no URL")
	}
	switch p.Detail {
	case "", ImageDetailAuto, ImageDetailLow, ImageDetailHigh:
		return nil
	}
	return fmt.Errorf("image has unknown detail %q", p.Detail)
}

func validateAudio(p InputAudio) error {
	if len(p.Data) == 0 {
		return errors.New("audio has no data")
	}
	switch p.Format {
	case AudioFormatWAV, AudioFormatMP3:
		return nil
	}
	return fmt.Errorf("audio has unknown format %q", p.Format)
}

func validateFunctionCall(c FunctionCall) error {
	switch {
	case c.CallID == "":
		return errors.New("function_call has no call id")
	case c.Name == "":
		return fmt.Errorf("function_call %s has no name", c.CallID)
	case c.Arguments != "" && !jsontext.Value(c.Arguments).IsValid():
		return fmt.Errorf("function_call %s has arguments that aren't JSON", c.CallID)
	}
	return nil
}

func validateFunctionCallOutput(o FunctionCallOutput) error {
	switch {
	case o.CallID == "":
		return errors.New("function_call_output has no call id")
	case o.Output != "" && len(o.OutputParts) > 0:
		return fmt.Errorf("function_call_output %s has both an output string and output parts", o.CallID)
	}
	for i, p := range o.OutputParts {
		if _, ok := p.(InputText); !ok {
			return fmt.Errorf("function_call_output %s part %d: unsupported part %T", o.CallID, i, p)
		}
	}
	return nil
}

func validateTool(t FunctionTool, declared map[string]bool) error {
	switch {
	case t.Name == "":
		return errors.New("tool has no name")
	case !validName(t.Name):
		return fmt.Errorf("tool name %q isn't 1 to 64 letters, digits, underscores, and hyphens", t.Name)
	case declared[t.Name]:
		return fmt.Errorf("tool name %q is declared twice", t.Name)
	case len(t.Parameters) > 0 && (t.Parameters.Kind() != jsontext.KindBeginObject || !t.Parameters.IsValid()):
		return fmt.Errorf("tool %s's parameters aren't a JSON object with unique member names", t.Name)
	}
	return nil
}

func validateText(t TextParam) error {
	switch f := t.Format.(type) {
	case nil, PlainTextFormat:
	case JSONSchemaFormat:
		if err := validateJSONSchemaFormat(f); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format %T", f)
	}
	switch t.Verbosity {
	case "", VerbosityLow, VerbosityMedium, VerbosityHigh:
		return nil
	}
	return fmt.Errorf("unknown verbosity %q", t.Verbosity)
}

func validateJSONSchemaFormat(f JSONSchemaFormat) error {
	switch {
	case f.Name == "":
		return errors.New("json_schema format has no name")
	case !validName(f.Name):
		return fmt.Errorf("json_schema format name %q isn't 1 to 64 letters, digits, underscores, and hyphens", f.Name)
	case len(f.Schema) == 0:
		return fmt.Errorf("json_schema format %s has no schema", f.Name)
	case f.Schema.Kind() != jsontext.KindBeginObject || !f.Schema.IsValid():
		return fmt.Errorf("json_schema format %s's schema isn't a JSON object with unique member names", f.Name)
	}
	return nil
}

func validName(name string) bool {
	if len(name) > 64 {
		return false
	}
	for _, c := range []byte(name) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func validateToolChoice(tc ToolChoice, declared map[string]bool) error {
	switch tc := tc.(type) {
	case ToolChoiceMode:
		return validateToolChoiceMode(tc)
	case FunctionToolChoice:
		return validateFunctionToolChoice(tc, declared)
	case AllowedToolChoice:
		if len(tc.Tools) == 0 {
			return errors.New("allowed_tools has no tools")
		}
		for i, f := range tc.Tools {
			if err := validateFunctionToolChoice(f, declared); err != nil {
				return fmt.Errorf("allowed_tools tool %d: %w", i, err)
			}
		}
		if err := validateToolChoiceMode(tc.Mode); err != nil {
			return fmt.Errorf("allowed_tools: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported tool choice %T", tc)
	}
}

func validateToolChoiceMode(m ToolChoiceMode) error {
	switch m {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return nil
	}
	return fmt.Errorf("unknown mode %q", m)
}

func validateFunctionToolChoice(f FunctionToolChoice, declared map[string]bool) error {
	if !declared[f.Name] {
		return fmt.Errorf("function %q isn't one of the request's tools", f.Name)
	}
	return nil
}
