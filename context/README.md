# go-ai context

go-ai is the model client of Go Elemental: one client over the standard wire formats the model
engines serve natively (Chat Completions, Anthropic Messages, and Open Responses), so a caller
generates, streams, embeds, and transcribes without a vendor SDK. Its base module uses the
standard library only; its convenience CLI, `infer`, lives in the nested module `tools/infer`.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.client). It is built from spike-ai-pipeline-client v0.4.0 to the design in
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
