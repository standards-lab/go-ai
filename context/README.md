# go-ai context

go-ai is the model client of Go Elemental: one client over the standard wire formats the model
engines serve natively (Chat Completions, Anthropic Messages, and Open Responses), so a caller
generates, streams, embeds, and transcribes without a vendor SDK. Its base module uses the
standard library and go-core; its convenience CLI, `infer`, lives in the nested module
`tools/infer`.

Its goals live in the coordinator's roadmap (`standards-lab/roadmap/index.toml`). It is built
from spike-ai-pipeline-client v0.4.0 to the design review in [`docs/design.md`](../docs/design.md).

## Capability map

- **Protocol** (built): [protocol/doc.go](../protocol/doc.go).
- **Formats** (built): [format/doc.go](../format/doc.go).
- **Client** (built): [doc.go](../doc.go).
- **Resilience** (built): [retry.go](../retry.go) and [idle.go](../idle.go).
- **Test support** (built): [aitest/doc.go](../aitest/doc.go).
- **infer** (built): [tools/infer/README.md](../tools/infer/README.md).
- **Integration tiers** (built): [integration/doc.go](../integration/doc.go).
- **A later minor release** (planned): [next-minor.md](next-minor.md).
