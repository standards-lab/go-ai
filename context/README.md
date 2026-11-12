# go-ai context

go-ai is the model client of Go Elemental: one client over the standard wire formats the model
engines serve natively (Chat Completions, Anthropic Messages, and Open Responses), so docinfer,
Cerebro, and any other caller generate, stream, embed, and transcribe without a vendor SDK. Its
base module uses the standard library only; its convenience CLI, `infer`, lives in the nested
module `tools/infer`.

Its goals live in the coordinator's roadmap (`standards-lab/context/roadmap.toml`,
v1.ai.client). It is built from spike-ai-pipeline-client v0.4.0, and the surface its callers use
is settled by docproc's `context/layers.md`, "go-ai's surface".

## Capability map

- **Protocol**: the Open Responses request and response types every format maps to and from.
- **Formats**: Chat Completions, Anthropic Messages, and Open Responses on the wire.
- **Client**: construction and Config, Generate, GenerateStream with its accumulator, Embed,
  Transcribe, and the /models listing.
- **Resilience**: retry with Retry-After, per-call timeouts, and cancellation by context.
- **infer**: the CLI over the client: chat, embed, transcribe, and models.
