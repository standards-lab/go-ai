# go-ai standards

The judgement calls the standards-reviewer applies to go-ai, beyond what `mise run check`
enforces: each one line, with the architecture page that applies. They come from go-ai's design
review, `context/design.md`.

- `architecture/principles/context-architecture.md`: the README and each `doc.go` are the homes; a
  change that alters documented behavior updates the README and the affected `doc.go` with it.
- `architecture/principles/downward-dependencies.md`: no README, `doc.go`, comment, or test names a
  caller of go-ai; the README states go-ai's tier, its dependency line, and its API by example.
- `architecture/standards/go-elemental/principles/dependencies.md`, "No provider in a base": a
  format pins no vendor library and stays a package of the base module, and a requirement beyond
  go-ai, go-cli-sdk, and go-core enters `tools/infer` only under "Sourcing".
- `architecture/standards/go-elemental/principles/dependencies.md`, "A provider is selected by
  construction": a wire format is chosen by its package's typed constructor at the caller's
  composition root, never from a registry or by name.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: the library sets no
  timeout, retry, size limit, or concurrency bound unless the caller does; retry is opt-in, a
  policy the caller sets in `Options`, and every option's zero value sets none.
- `architecture/standards/go-elemental/principles/baseline-standards.md`, "A type is introduced
  only when it is needed": the stream events stay Go values with no wire form, and an encoder or
  other type waits for a caller that needs it.
- `architecture/principles/service-tiers.md`, "Providers are adapters behind one interface": each
  format is an adapter beneath `format.Format`, which knows `protocol` and never the client.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `protocol` is Open
  Responses in its stateless subset, under the spec's names and no wider, and a client extension
  carries go-ai's slug, `go-ai`.
- `architecture/principles/service-tiers.md`, "Native use is wrapped beneath standard use": a
  native engine parameter goes through the request's raw-JSON `Native` field, documented as native
  tier, never as a typed field.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: `ai.New`'s
  required inputs are positional and its optional ones `Options`, as `responses.New`'s are; every
  new constructor does the same, and an options struct is `Options`, never `Config`.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`, "The wiring rule": a
  nil HTTP client or format panics by name; a base URL, retry policy, or header that can't be used
  is an error; a call takes the caller's context, which ends it and closes its stream.
- `architecture/standards/go-elemental/principles/timeouts.md`: the retry set is 408, 429, 502,
  503, 504, and a connection failure before any reply, never a started stream, a header timeout,
  or net/http's idle-connection error; `Options.StreamIdleTimeout` bounds each read.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: a retried POST may run
  twice on the endpoint, `RetryPolicy`'s documentation says so, and no idempotency key is sent.
- `architecture/principles/validation-first.md`: a request the protocol or its format can't
  express fails before the token source is called or anything is sent; a hint a format has no
  form for is dropped, and its package comment says so.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the root package `ai`,
  its errors prefixed `ai: `; the base module with no provider sub-module; `tools/infer` nested,
  with the tags `v<semver>` and `tools/infer/v<semver>`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`, "doc.go and godoc": the
  doc.go inventory of every package, and a format's package comment states what it sends,
  refuses, drops, and normalizes, naming no router build.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: `internal/aitest` as the
  helper the test packages share; testdata recorded from an endpoint with a README saying how;
  an integration check that can't fail is deleted.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency,
  upgrade, and integration tasks over `GO_MODULES`; a changelog per module; `go.work`'s use-list
  the same as `GO_MODULES`; no CI job runs the integration tier.
- `architecture/standards/go-elemental/principles/cli-applications.md`: `infer` laid out from
  go-cli-sdk-template, its subcommands through the base module's surface alone, and
  `infrastructure.go` the only file of its composition root that names a format.
