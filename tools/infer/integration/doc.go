// Package integration is infer's integration tier: the harness that runs
// the program as the built binary, one shot at a time, and, under the
// integration build tag, the suite that asserts its behavior through the
// interfaces a terminal uses: its arguments, its two output streams, and
// its exit code. The harness is built on go-core's processtest; nothing in
// the runtime exists for the tests' sake.
//
//   - [Main] is the suite's TestMain: it clears the INFER_ environment and
//     builds cmd/infer once per run.
//   - [Run] runs the built program once with its arguments and returns its
//     processtest.Result.
//   - [RunEnv] runs it once as Run does, with environment overrides, such
//     as INFER_BASE_URL.
//   - [BaseURL] returns the endpoint an AI_<ROLE>_BASE_URL variable names,
//     skipping the test when it is unset.
//   - [Endpoint] returns the INFER_BASE_URL override of a role's endpoint,
//     as BaseURL gives it, and the INFER_MODEL override of the model its
//     AI_<ROLE>_MODEL names, when set, and maps the role's AI_<ROLE>_TOKEN and
//     AI_<ROLE>_HEADER, when set, onto INFER_TOKEN and INFER_HEADER in the
//     test's environment, which the program inherits, so no run's logged
//     shell line, and so no failure, shows a token or header value.
//   - [Model] returns the model a role's AI_<ROLE>_MODEL names, or "".
//   - [Profile] returns the profile a role's AI_<ROLE>_PROFILE holds, which
//     Endpoint maps onto INFER_PROFILE, once it decodes strictly, or "".
//   - [SkipDeclared] skips a test whose run infer refused for a gap the
//     role's profile declares.
//   - [Media] returns the path of a medium the suite sends: an image or an
//     audio clip of go-ai's own integration testdata.
//
// Beside the baseline command tree, the suite runs infer's commands against
// live endpoints, each named by an AI_<ROLE>_BASE_URL variable, as go-ai's
// own integration tier names them: AI_AGENT_BASE_URL, AI_VISION_BASE_URL,
// AI_AUDIO_BASE_URL, AI_EMBEDDING_BASE_URL, and AI_TRANSCRIPTION_BASE_URL,
// each an API root including /v1. A scenario maps its role's variable onto
// INFER_BASE_URL, its credentials, AI_<ROLE>_TOKEN and AI_<ROLE>_HEADER,
// when set, onto INFER_TOKEN and INFER_HEADER, for an endpoint that
// requires them, such as a gateway, its model, AI_<ROLE>_MODEL, when set,
// onto INFER_MODEL, for an endpoint that lists more than one, and its
// format onto INFER_FORMAT; with no model named it sets none, so infer
// sends to the one model the endpoint lists. It skips when the variable is
// unset; with none set, only the baseline and the loopback scenarios run.
//
// A role may also give its deployment's profile in AI_<ROLE>_PROFILE, such
// as AI_AGENT_PROFILE, in the shape infer's --profile takes: a JSON object
// keyed by format name, completions, messages, or responses, each member
// that go-ai format's Options under their json names. A scenario maps it
// onto INFER_PROFILE, so infer builds the format in use with its member;
// unset, it sets none, so infer speaks each format's spec. It is decoded
// strictly first, as infer and go-ai's tier decode it, with
// encoding/json/v2 and json.RejectUnknownMembers: an unknown format name or
// field, a Go field name, or a value a field refuses fails each scenario of
// the role naming the variable. A run infer refuses for a gap the profile
// declares, before sending, exit 2 with an error line ending in "; declared
// by --profile (INFER_PROFILE)", skips its scenario, naming the variable
// and quoting the line, which names the gap's feature and value: the
// deployment can't serve it, so there is nothing to assert. A refusal of
// the spec's own carries no such suffix and still fails. The skip is the
// one rule of SkipDeclared, which every scenario on an endpoint runs
// through.
//
// The agent row's vLLM 0.34.0, serving gpt-oss-120b, is described by this
// profile, for example:
//
//	AI_AGENT_PROFILE='{"completions":{"refused_efforts":["none","minimal","xhigh","max"],"no_allowed_tools":true},"responses":{"refused_efforts":["none","xhigh"],"no_allowed_tools":true,"resend_reasoning_text":true},"messages":{"refused_efforts":["none","xhigh","max"]}}'
//
// Each field is backed by a recording of that deployment in go-ai's
// format/<fmt>/testdata/vllm-0.34.0, whose README tells each:
//
//   - completions refused_efforts: effort_none.json, effort_minimal.json,
//     effort_xhigh.json, and effort_max.json, each a 400;
//   - completions no_allowed_tools: allowed_tools.json, a 400 of
//     tool_choice.function;
//   - responses refused_efforts: effort_none.json and effort_xhigh.json,
//     each a 400;
//   - responses no_allowed_tools: allowed_tools.json, a 200 calling a tool
//     outside the allowed set;
//   - responses resend_reasoning_text: reasoning_text.json, a reasoning
//     item of text and null encrypted_content;
//   - messages refused_efforts: effort_none.json, a 200 that reasons under
//     thinking disabled, and effort_xhigh.json and effort_max.json, each a
//     400.
//
// On that row, go-ai's Open Responses round trips, TestFunctionCallRoundTrip,
// TestReasoningRoundTrip, and TestStreamReasoningRoundTrip, each through
// responses, pass under this profile and under the zero profile alike.
// Nothing reads this example, and go-ai ships no preset: the profile is the
// caller's to give.
//
// The scenarios are:
//
//   - credentials, against a loopback endpoint the suite serves, so it
//     never skips: a role's token and header reach the endpoint through
//     the built binary, and the run's log shows neither value;
//   - declared skip, against a loopback endpoint too: a role whose profile
//     refuses an effort, at chat --reasoning of it, skips the scenario,
//     and the endpoint gets no request;
//   - models on every role, wanting exactly one model ID, or at least one
//     where AI_<ROLE>_MODEL names the model;
//   - chat on the agent, in each format, streamed and unstreamed, wanting
//     a completed answer, and an incomplete one at --max-tokens 16, exit 1
//     with reason max_output_tokens;
//   - chat on vision at --reasoning none and low, in each format, streamed
//     and unstreamed, wanting no reasoning item at none and, where none
//     ran rather than skipped, some at low, and an --image chat in each
//     format naming the image's shapes;
//   - transcribe --mode chat on audio through Chat Completions, and route
//     mode on the transcription endpoint, each wanting the clip's words;
//   - embed on the embedding endpoint, wanting one vector per input, the
//     alike inputs closer than the unrelated one.
//
// `mise run integration` runs the suite; no CI job does, as the endpoints
// are the organization's own machines.
//
// The harness is untagged, so the unit tier type-checks it on every pull
// request; the suite files carry the integration tag.
package integration
