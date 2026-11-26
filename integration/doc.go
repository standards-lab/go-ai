// Package integration is go-ai's integration tier: tests behind the integration build tag that
// drive the exported API black-box against live endpoints. `mise run integration` runs them; the
// hermetic check builds, vets, and lints them under the tag but never runs them, and no CI job
// runs them, as the models are on the organization's own machines.
//
// Each role is an endpoint named by an environment variable, its API root including /v1, given
// to ai.New as it is, with no default:
//
//   - AI_AGENT_BASE_URL: a reasoning model with function calling and structured output. The
//     chat, stream, tools, structured, and reasoning scenarios run on it in every format.
//   - AI_VISION_BASE_URL: a model that reads images. The image scenario runs on it in every
//     format, and a streamed text reply through Chat Completions and Open Responses.
//   - AI_AUDIO_BASE_URL: a model that reads audio in a chat request. Transcribe runs on it in
//     chat mode, through Chat Completions, and Open Responses' refusal of audio is asserted.
//   - AI_EMBEDDING_BASE_URL: an embedding model. The embed and input-type scenarios run on it,
//     and the tokenize scenario embeds on it.
//   - AI_TRANSCRIPTION_BASE_URL: an endpoint that serves the transcription route,
//     audio/transcriptions. Transcribe runs on it in route mode, its default.
//
// The input-type and tokenize scenarios give the embedding client a fixed embedding profile, not
// one read from the environment: document inputs take the prefix "passage: ", multilingual-e5's.
// The input-type scenario wants a document embedding of a text within 1e-3, element-wise, of the
// bare embedding of the prefixed text, and the bare embedding of the text itself outside it, so a
// model that ignores the prefix fails it. The tokenize scenario wants Tokenize's count of a text
// to equal the prompt tokens Embed reports for it, bare and as a document, and the document's
// count to exceed the bare one.
//
// Tokenize goes to the route vLLM serves at the server's root, not below /v1, so the tokenize
// scenario takes its own variable, AI_TOKENIZE_BASE_URL: the root the embedding endpoint serves
// tokenize at, such as http://localhost:8000 beside AI_EMBEDDING_BASE_URL's
// http://localhost:8000/v1, or a gateway's API root that forwards it. Unset, the scenario skips,
// naming it. It sends to the embedding role's model, and takes its credentials from
// AI_TOKENIZE_TOKEN and AI_TOKENIZE_HEADER as a role does. It is no role: the models scenario
// doesn't list it, as a server's root serves no model list.
//
// Each role may also name its credentials, for an endpoint that requires them, such as a
// gateway or a cloud endpoint: AI_<ROLE>_TOKEN, such as AI_AGENT_TOKEN, sent as the bearer token
// through Options.Token, and AI_<ROLE>_HEADER, one or more "Name: value" lines separated by
// newlines, each trimmed and a blank one skipped, sent through Options.Header, such as
// AI_AGENT_HEADER='api-key: …'. With neither set, the role's requests carry no credential. A
// header line with no colon or no name fails the scenario naming the variable and the line, and
// a header ai.New refuses fails it naming the role's base URL; no failure prints a token or a
// header value. The reading of them is untagged, so the unit tier tests it.
//
// `mise run integration` fills an unset AI_<ROLE>_TOKEN from the committed fnox.toml, as the
// README's "Secrets" says.
//
// A scenario whose role's variable is unset skips, naming the variable, so the tier with none
// set skips whole. A scenario sends to the one model its endpoint's model list names, and fails
// when the list names none or more than one. A role may instead name its model in
// AI_<ROLE>_MODEL, such as AI_AGENT_MODEL='gpt-oss-120b', for an endpoint that lists more, such
// as a cloud account's whole catalog: its scenarios then send to that model without listing. The
// model is no secret, and is read apart from the credentials. Every role runs the models
// scenario, which wants exactly one model listed, or at least one when AI_<ROLE>_MODEL is set.
//
// Each role may also give its deployment's profile in AI_<ROLE>_PROFILE, such as
// AI_AGENT_PROFILE: a JSON object keyed by format name, completions, messages, or responses, each
// member that format's Options under their json names, the shape infer's --profile and
// INFER_PROFILE take. Each scenario builds its format with the role's member, or the format's
// zero Options, its spec's form, when the member is absent or the variable unset, so unset is
// the spec everywhere. It is decoded strictly, with encoding/json/v2 and
// json.RejectUnknownMembers: an unknown format name or field, a Go field name, or a value a field
// refuses fails each scenario of the role naming the variable. A request the profile refuses
// before sending, a *format.UnsupportedError of Declared true, skips its scenario, naming the
// variable, the format, and the gap, its Feature and Value: the deployment can't serve it, so
// there is nothing to assert. A refusal of the spec's own, Declared false, still fails. The skip
// is the one rule of skipDeclared, which wraps every scenario's format. The reading of the
// profile and the skip are untagged, so the unit tier tests them.
//
// The agent row's vLLM 0.34.0, serving gpt-oss-120b, is described by this profile, for example:
//
//	AI_AGENT_PROFILE='{"completions":{"refused_efforts":["none","minimal","xhigh","max"],"no_allowed_tools":true},"responses":{"refused_efforts":["none","xhigh"],"no_allowed_tools":true,"resend_reasoning_text":true},"messages":{"refused_efforts":["none","xhigh","max"]}}'
//
// Each field is backed by a recording of that deployment, in format/<fmt>/testdata/vllm-0.34.0,
// whose README tells each:
//
//   - completions refused_efforts: effort_none.json, effort_minimal.json, effort_xhigh.json, and
//     effort_max.json, each a 400;
//   - completions no_allowed_tools: allowed_tools.json, a 400 of tool_choice.function;
//   - responses refused_efforts: effort_none.json and effort_xhigh.json, each a 400;
//   - responses no_allowed_tools: allowed_tools.json, a 200 calling a tool outside the allowed
//     set;
//   - responses resend_reasoning_text: reasoning_text.json, a reasoning item of text and null
//     encrypted_content;
//   - messages refused_efforts: effort_none.json, a 200 that reasons under thinking disabled,
//     and effort_xhigh.json and effort_max.json, each a 400.
//
// On that row, the Open Responses round trips, TestFunctionCallRoundTrip/responses,
// TestReasoningRoundTrip/responses, and TestStreamReasoningRoundTrip/responses, pass under this
// profile and under the zero profile alike: they don't depend on the reasoning sent back. Nothing
// reads this example, and go-ai ships no preset: the profile is the caller's to give.
package integration
