# go-ai standards

The judgement calls the standards-reviewer applies to go-ai, beyond what `mise run check`
enforces: each one line, with the architecture page that applies. They come from go-ai's design
review, `context/design.md`.

- `architecture/principles/context-architecture.md`: the README and each package's `doc.go` hold
  go-ai's documentation; a change that alters documented behavior updates them with it.
- `architecture/principles/downward-dependencies.md`: no README, `doc.go`, comment, or test names a
  caller of go-ai; the README names what go-ai depends on.
- `architecture/standards/go-elemental/README.md`, "Member repositories": the README states go-ai's
  tier and the principles it enhances, its dependency line, and its API by example.
- `architecture/standards/go-elemental/principles/dependencies.md`, "Sourcing": a requirement of
  `tools/infer` beyond go-ai, go-cli-sdk, and go-core enters only as a library sourced under the
  page's markers, and the README states it beside its link to the standard.
- `architecture/standards/go-elemental/principles/dependencies.md`, "A provider is selected by
  construction": a wire format is chosen by its package's typed constructor at the caller's
  composition root, never from a registry or by name.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: the library sets no
  timeout, retry, size limit, or concurrency bound unless the caller does; retry is opt-in, a
  policy the caller sets in `Options`, and every option's zero value sets none.
- `architecture/principles/service-tiers.md`, "Providers are adapters behind one interface": each
  format is an adapter beneath `format.Format`, which knows `protocol` and never the client.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `protocol` is Open
  Responses in its stateless subset, under the spec's names and no wider, and a client extension
  carries go-ai's slug, `go-ai`.
- `architecture/principles/service-tiers.md`, "Native": a native engine parameter goes through the
  request's raw-JSON `Native` field, documented as native tier, never as a typed field.
- `architecture/standards/go-elemental/principles/construction-and-execution.md`: `ai.New`'s
  required inputs are positional and its optional ones `Options`, as `responses.New`'s are; every
  new constructor does the same.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`: a nil HTTP client or
  format panics by name, and a base URL, retry policy, or header that can't be used is an error
  ("The wiring rule"); a call takes the caller's plain context, which ends it and closes its stream.
- `architecture/standards/go-elemental/principles/timeouts.md`: a started stream is bounded by
  `Options.StreamIdleTimeout`, which bounds each read and is off at its zero value.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: a retried POST may run
  twice on the endpoint, `RetryPolicy`'s documentation says so, and no idempotency key is sent.
- `architecture/principles/validation-first.md`: a request the protocol or its format can't
  express fails before the token source is called or anything is sent; a hint a format has no
  form for is dropped, and its package comment says so.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the root package is
  `ai` and its errors are prefixed `ai: `; the base module has no provider sub-module; the root is
  tagged `v<semver>` and `infer` `tools/infer/v<semver>`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`, "doc.go and godoc": every
  package's `doc.go` inventories its exported identifiers, and a format's package comment states
  what it sends, refuses, drops, and normalizes, naming no router build.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the test packages share
  `internal/aitest` as their helper; testdata recorded from an endpoint has a README saying how;
  an integration check that can't fail is deleted.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency,
  upgrade, and integration tasks run over `GO_MODULES`; each module has its own changelog;
  `go.work`'s use-list matches `GO_MODULES`; no CI job runs the integration tier.
- `architecture/principles/tool-beside-library.md`: `infer` is a thin consumer of the base module's
  surface and never the home of logic an integrator calls, in a module of its own, `tools/infer`,
  so its requirements never enter the library's.
- `architecture/standards/go-elemental/principles/cli-applications.md`: `infer` is laid out from
  go-cli-sdk-template, and `infrastructure.go` is the only file of its composition root that names
  a format.
