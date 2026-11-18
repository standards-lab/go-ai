# go-ai context

go-ai is the model client of Go Elemental: one client over the standard wire formats the model
engines serve natively (Chat Completions, Anthropic Messages, and Open Responses), so a caller
generates, streams, embeds, and transcribes without a vendor SDK. Its base module uses the
standard library only; its convenience CLI, `infer`, lives in the nested module `tools/infer`.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`). It is built from spike-ai-pipeline-client v0.4.0 to the design in
[`design.md`](design.md); the map from each name of docproc's `context/layers.md`, "go-ai's
surface", to one go-ai name is the coordinator's (`standards-lab/context/ai-strategy.md`, "The
client and processing layers").

## Capability map

- **Protocol**: the Open Responses request and response types every format maps to and from,
  with the reasoning efforts any implemented format's spec defines and the `go-ai:context_window`
  incomplete reason.
- **Formats**: Chat Completions, Anthropic Messages, and Open Responses on the wire. Each format
  declares the event stream its replies arrive in, `format.SSE` for all three, and refuses what
  its spec has no form for with a `*format.UnsupportedError` before anything is sent. Each takes
  its own `Options`, the profile of one deployment, whose zero value is the spec: the format
  refuses a declared gap before sending, as an `UnsupportedError` with `Declared` true, or narrows
  an `allowed_tools` choice to the allowed tools.
- **Client**: construction with `Options`, including fixed request headers; Generate,
  GenerateStream with its accumulator, Embed, Transcribe by the route or over chat, and the
  /models listing.
- **Resilience**: retry with Retry-After, per-call timeouts, cancellation by context, and an idle
  bound on a started stream.
- **infer**: the CLI in `tools/infer`, over the client's released surface: the commands models,
  embed, chat, and transcribe against one endpoint per run, set by flags over `INFER_` variables,
  with credentials from `INFER_TOKEN` and `INFER_HEADER` alone and a fallback to the one model the
  endpoint lists. `--profile`, over `INFER_PROFILE`, gives the deployment's profile as one JSON
  object keyed by format name. `tools/infer/docs/quick-start.md` tours each command and the common
  failures with real output.
- **Integration tiers**: go-ai's and infer's, run by `mise run integration` against one endpoint
  per role, named by `AI_<ROLE>_BASE_URL`, with the role's optional credentials in
  `AI_<ROLE>_TOKEN` and `AI_<ROLE>_HEADER`, its optional model in `AI_<ROLE>_MODEL`, for an
  endpoint that lists more than one, and its optional profile in `AI_<ROLE>_PROFILE`, in
  `--profile`'s shape. Unset, every format is its spec; a scenario the profile refuses before
  sending skips and names the gap.

## What Cerebro asks of go-ai

Cerebro, the capstone.data-intelligence service, is go-ai's first long-running service consumer
(the coordinator's `data-intelligence.md`, "What it asks of the goals beneath it"). It asks for:

- retry proven against the live gateway's 429, 503, and Retry-After. go-ai's unit tests prove
  retry, and spike-model-gateway proves it against the gateway ([`design.md`](design.md),
  finding 17);
- per-call timeouts and cancellation by context;
- a per-consumer key header;
- Embed batching;
- a streaming Generate whose events Cerebro can relay.

Concurrency bounds stay the caller's.

## Candidates for a later minor release

These changes are noted for a later go-ai minor release; none is planned yet.

From the infer task:

- A helper that gathers a Response's output text. infer implements its own.
- Errors from `ai.New` that name the refused field. infer calls `ai.New` again with each setting
  on its own to find which one was refused.
- The retry policy's refusal, `base delay is above its most delay`, should say `max delay`.
- The idle-timeout error, `unexpected EOF: no bytes within the stream idle timeout (…): i/o
  timeout`, reads as a closed connection rather than a timeout.
- `StreamIdleTimeout` also bounds the wait for a stream's first bytes, so a reasoning model needs
  a bound longer than its time to first token. Either document it, or bound the two waits
  separately.
- A `Native` clash is checked only on the exact wire name, so `{"max_tokens":64}` passes beside
  completions' `max_completion_tokens`.

From the format-profile plan:

- `messages.Options.Version`, the `anthropic-version` a deployment needs, once a deployment here
  needs it. Bedrock's examples send 2023-06-01, and Claude models before 4.7 keep an older
  version.
- Layout: one assistant-run grouping across the three encoders; one internal envelope seal and
  open; one copy of the duplicated helpers (`withStatus`, `itemID`, `emit`); no stream folds that
  mirror `protocol.Accumulator`; and a Responses stream whose terminal response wins over its
  done-item equality checks.
- Bedrock Converse. It needs a binary event-stream reader (`application/vnd.amazon.eventstream`)
  built on the standard library alone (`encoding/binary`, `hash/crc32`), after tau's adapter,
  behind `format.EventStream`. It also needs a route derived from the request, since Converse puts
  the model in the path.
- The spec features go-ai doesn't model: Message.phase, file input, a tool result's is_error and
  richer tool results, refusal stop_details, reasoning-summary stream events, annotations,
  logprobs, stop sequences, image detail `original`, and an error body's request_id.
- Risks seen in the specs but not met here: Messages effort none sends thinking disabled, which
  Opus 5.5 refuses; the newest Claude models refuse a forced tool_choice; Foundry Model
  Inference's flat error body decodes to an empty message; Mistral takes `max_tokens`.
