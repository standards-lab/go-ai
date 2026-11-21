# A later minor release

These changes are noted for a later go-ai minor release; none is scheduled yet.

- Errors from `ai.New` that name the refused field. infer calls `ai.New` again with each setting
  on its own to find which one was refused.
- The retry policy's refusal, `base delay is above its most delay`, should say `max delay`.
- The idle-timeout error, `unexpected EOF: no bytes within the stream idle timeout (…): i/o
  timeout`, reads as a closed connection rather than a timeout.
- A `Native` clash is checked only on the exact wire name, so `{"max_tokens":64}` passes beside
  completions' `max_completion_tokens`.
- `messages.Options.Version`, the `anthropic-version` a deployment needs, once a deployment here
  needs it. Bedrock's examples send 2023-06-01, and Claude models before 4.7 keep an older
  version.
- Layout: one assistant-run grouping across the three encoders; one internal envelope seal and
  open; one copy of the duplicated helpers (`withStatus`, `itemID`, `emit`); no stream folds that
  mirror `protocol.Accumulator`; and a Responses stream whose terminal response wins over its
  done-item equality checks.
- Bedrock Converse. It needs a binary event-stream reader (`application/vnd.amazon.eventstream`)
  built on the standard library alone (`encoding/binary`, `hash/crc32`), after the adapter in tau
  (`standards-lab/context/ai-strategy.md`, "External harnesses in place of tau"), behind
  `format.EventStream`. It also needs a route derived from the request, since Converse puts the
  model in the path.
- The spec features go-ai doesn't model: Message.phase, file input, a tool result's is_error and
  richer tool results, refusal stop_details, reasoning-summary stream events, annotations,
  logprobs, stop sequences, image detail `original`, and an error body's request_id.
- Risks seen in the specs but not met here: Messages effort none sends thinking disabled, which
  Opus 5.5 refuses; the newest Claude models refuse a forced tool_choice; Foundry Model
  Inference's flat error body decodes to an empty message; Mistral takes `max_tokens`.
