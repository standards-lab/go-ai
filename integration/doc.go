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
//     format.
//   - AI_AUDIO_BASE_URL: a model that reads audio in a chat request. Transcribe runs on it in
//     chat mode, through Chat Completions, and Open Responses' refusal of audio is asserted.
//   - AI_EMBEDDING_BASE_URL: an embedding model. The embed scenario runs on it.
//   - AI_TRANSCRIPTION_BASE_URL: an endpoint that serves the transcription route,
//     audio/transcriptions. Transcribe runs on it in route mode, its default.
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
// A scenario whose role's variable is unset skips, naming the variable, so the tier with none
// set skips whole. Each role's endpoint serves one model: a scenario sends to the one model the
// endpoint's model list names, and fails when the list names none or more than one. Every role
// runs the models scenario.
package integration
