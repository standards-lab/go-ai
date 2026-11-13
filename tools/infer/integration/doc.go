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
//   - [Media] returns the path of a medium the suite sends: an image or an
//     audio clip of go-ai's own integration testdata.
//
// Beside the baseline command tree, the suite runs infer's commands against
// live endpoints, each named by an AI_<ROLE>_BASE_URL variable, as go-ai's
// own integration tier names them: AI_AGENT_BASE_URL, AI_VISION_BASE_URL,
// AI_AUDIO_BASE_URL, AI_EMBEDDING_BASE_URL, and AI_TRANSCRIPTION_BASE_URL,
// each an API root including /v1. A scenario maps its role's variable onto
// INFER_BASE_URL, and its format onto INFER_FORMAT, sets no model, so infer
// sends to the one model the endpoint lists, and skips when the variable is
// unset; with none set, only the baseline runs. The scenarios are:
//
//   - models on every role, wanting exactly one model ID;
//   - chat on the agent, in each format, streamed and unstreamed, wanting
//     a completed answer, and an incomplete one at --max-tokens 16, exit 1
//     with reason max_output_tokens;
//   - chat on vision at --reasoning none and low, in each format, streamed
//     and unstreamed, wanting no reasoning item at none and some at low, and
//     an --image chat in each format naming the image's shapes;
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
