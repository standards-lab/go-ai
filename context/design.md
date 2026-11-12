# design

go-ai's design, reviewed from spike-ai-pipeline-client v0.4.0: what the spike is measured against,
what go-ai keeps, changes, and drops from it, and the surface the library and infer tasks of
v1.ai.client build to. It is provisional until the library task builds it.

## Evaluation

Fable 5.2's evaluation of the spike at tag v0.4.0, one finding per issue or simplification. Each
names the spike's package or file, what it is measured against, and a verdict with its reason:
**kept** as the spike has it, **changed** in go-ai, or **dropped**. Where a finding changes a
signature, the surface section states the signature (planned).

The measures are cited in short. A bare page name, such as `dependencies.md`, is a Go Elemental
principle under the architecture repository's `standards/go-elemental/principles/`;
`principles/<page>` is an architecture principle; a quoted name is the page's section. "layers" is
docproc's `context/layers.md`, whose "go-ai's surface" lists what docinfer and Cerebro call.
"Cerebro" is standards-lab's `context/data-intelligence.md`, "What it asks of the goals beneath it".
"ai-strategy" is standards-lab's `context/ai-strategy.md`.

### Layout and names

1. **Four layers, importing one way** (`doc.go`; `protocol`, `format`, `format/completions`,
   `format/messages`, `format/responses`, and the root). Against `principles/service-tiers.md`,
   "Providers are adapters behind one interface", and `topology-and-naming.md`, "Module layout per
   tier". **kept**: each format is an adapter beneath `format.Format`, and the formats pin no vendor
   library, so they are packages of the base module, split by growth, not provider sub-modules.
2. **The root package is `client`** (`doc.go`). Against `topology-and-naming.md`, "Module layout per
   tier": the import path names the repository and the identifier stays the package's, as
   `database.New` does. Under go-ai's path every importer would alias the package, as the spike's
   own tests do. **changed**: the root package takes a name that reads with the repository's, and
   its error prefix follows; the surface section names it (planned).
3. **A format is chosen by typed constructor, with no registry** (`Config.Format`;
   `completions.New`, `messages.New`, `responses.New`). Against `dependencies.md`, "A provider is
   selected by construction". **kept**: it is the rule as written, and adding a format is one import
   at the caller's composition root.
4. **The implementor slug on the wire is the spike's** (`protocol/doc.go`, each format's `wire.go`):
   the audio extension's type, `spike-ai-pipeline:input_audio`, and the reasoning envelopes,
   `spike-ai-pipeline:<format>/v1:`. Against `baseline-standards.md`: the baseline is Open
   Responses, whose extension rule prefixes the implementor's slug. **changed**: the slug becomes
   go-ai's. An envelope the spike wrote is then another format's and is dropped on encode; neither
   caller holds one (layers, "What neither caller uses").
5. **Embeddings and transcription sit outside the Format, on the OpenAI-compatible routes**
   (`embed.go`, `transcribe.go`). Against `principles/service-tiers.md`, "Standard": where no formal
   standard exists, the common interface is the operations the target APIs share, kept minimal.
   **kept**: Open Responses defines neither operation, and the OpenAI-compatible shape is the one
   the model engines and the gateway serve.

### Construction

6. **`New(cfg Config)` takes one struct that mixes required and optional inputs** (`client.go`):
   `BaseURL`, `HTTP`, and `Format` are required; `Query`, `Token`, and `Retry` are not. Against
   `construction-and-execution.md`, "The rule": at most four required positional inputs, then one
   trailing options struct whose zero value is the default. **changed**: the three required inputs
   are positional and the optional ones an options struct. The spike predates the page.
7. **`Config.Retry` is a pointer, and a set policy of zero attempts is refused** (`client.go`,
   `retry.go`, `RetryPolicy.check`). Against `construction-and-execution.md`, "The rule": the
   options struct's zero value selects every default. **changed**: the policy is a value among the
   options, and its zero value retries nothing.
8. **`New` returns an error for a nil `HTTP` or `Format`** (`client.go`). Against
   `construction-and-execution.md`, "The rule" (a missing required field panics by name), and
   `lifecycle-and-context.md`, "The wiring rule": a wiring defect panics with the fix named, and a
   defect in configuration content returns an error. **changed**: a nil HTTP client or format panics
   by name; a base URL or retry policy that can't be used stays an error, since both are
   configuration content.
9. **The HTTP client is required and never defaulted, and the library sets no timeout, retry, or
   size limit** (`client.go`, `Config.HTTP`; `stream.go`; `internal/sse`, whose lines have no length
   limit; the spike's first client rule in `STANDARDS.md`). Against `baseline-standards.md` (a
   library ships no policy the application must own) and Cerebro (per-call timeouts and cancellation
   by context; concurrency bounds stay the caller's). **kept**: every bound is the context's or the
   caller's transport's, and a `Client` stays safe for concurrent use with no limit of its own.
10. **The base module takes nothing from go-core**: no configuration block, no lifecycle part
    (`go.mod`). Against `dependencies.md`, "The line per tier" and "Integration is structural where
    it can be", and `lifecycle-and-context.md` (leaf components take a plain `context.Context`).
    **kept**: an infrastructure library may take go-core, and go-ai tightens the line to the
    standard library, as `principles/minimal-footprint.md` allows. A client holds no resource to
    start, stop, or report ready, and its values come from the consumer's configuration.
11. **The client logs nothing and takes no logger, meter, or tracer** (the root package). Against
    `dependencies.md`, "Peers compose in the application". **kept**: instrumentation is a
    `RoundTripper` on the `*http.Client` the caller passes, so go-ai imports no observability
    library.
12. **`TokenSource` is the only credential, sent as a bearer token and called once per attempt**
    (`client.go`). Against Cerebro and layers, "The client": the consumer key is the bearer token,
    and a header of its own only if the gateway asks for one. **kept**. One gap stands beside it:
    Anthropic's API takes its key in `x-api-key`, so `format/messages` reaches that API only through
    a `RoundTripper` of the caller's. Whether the options gain fixed request headers, which would
    serve both, is the surface section's decision (planned).

### Retry and errors

13. **Retry is opt-in, on a 408, 429, or 503 reply or a connection failure before any reply, with
    full-jitter backoff; a Retry-After replaces the backoff, and a wait past `MaxDelay` or the
    context's deadline returns the last error at once** (`retry.go`). Against Cerebro (the gateway's
    429, 503, and Retry-After) and layers, "The client". **kept**: it is the behavior both callers
    are written to, and one loop serves every operation whose body can be sent again.
14. **`APIError` drops the reply's Retry-After** (`error.go`; `client.go`, `do`, returns the header
    beside the error and `send` discards it). Against layers, "Requests to go-ai". **changed**: the
    error carries it. `send` returns at once when the wait is past `MaxDelay` or the deadline, and
    that is when a caller that redelivers needs the delay.
15. **A transport's `ResponseHeaderTimeout` and net/http's error for a server closing an idle
    connection are not retried** (`retry.go`, `connectionFailure`; the spike's `context/README.md`).
    Against `timeouts.md`, "A store's per-try deadline bounds one operation". **kept**: after a
    header timeout the endpoint may still be running the request, and the idle-connection error is
    unexported, so matching it means matching its text. The proof against the live gateway shows
    whether either needs more.
16. **A retried POST may run twice on the endpoint** (`retry.go`, `RetryPolicy`'s comment). Against
    `baseline-standards.md`. **kept**: the caller who sets a policy accepts it, as the comment says,
    and an idempotency key would be one endpoint's native header, not the standard's.
17. **Retry is proven by unit tests alone** (`retry_test.go`, on `testing/synctest`; the spike's
    `context/README.md`). Against Cerebro (retry proven against the live gateway) and
    `tests-and-docs.md`, "Integration and acceptance suites, and where each runs". **changed**: the
    unit tests stay, and a proof against the live gateway's 429, 503, and Retry-After is added, in
    the acceptance task.
18. **In-band stream failures are events, and errors are for a stream that breaks or never begins**
    (`stream.go`; `format.StreamDecoder`). Against layers, "Generation: vision and extraction": any
    status but completed and incomplete is an error to docinfer. **kept**: the stream ends as the
    spec has it, with the error event and `response.failed`, and the folded `Response` holds the
    output made before the failure.

### Generation and streaming

19. **`Generate` validates, encodes, calls the token source, then sends** (`client.go`;
    `protocol/request.go`, `Validate`). Against `principles/validation-first.md`. **kept**: a
    request the model or the format can't express fails before any effect, in the caller's terms.
20. **`GenerateStream` is a lazy `iter.Seq2` read in the caller's loop, with no goroutine, and
    cancelling the context closes the connection** (`stream.go`). Against Cerebro (cancellation by
    context) and layers, "The client". **kept**: cancelling a job ends its requests, and the caller
    owns the loop's pace.
21. **Stream events are Go values in a closed set, each with its sequence number, and `protocol`
    gives them no wire form** (`protocol/event.go`). Against Cerebro (streaming Generate whose
    events Cerebro can relay) and `baseline-standards.md`, "A type is introduced only when it is
    needed". **kept**: a caller relays by mapping the typed events to its own, as docinfer yields
    `OutputTextDelta` as progress. Whether go-ai also encodes them as Open Responses' server-sent
    events is the surface section's decision (planned).
22. **A stream has no idle bound** (`stream.go`): after the reply's header a stalled body waits for
    the context, and the decoder reads past llama.cpp's pings. Against `timeouts.md`, "A store's
    per-try deadline bounds one operation", where go-storage bounds each read of a body. **kept**:
    an idle bound is policy, and a caller sets one in its own loop with a timer that cancels the
    context and resets on each event.
23. **`protocol.Accumulator` folds the events in the caller's loop and holds the partial output
    after a break** (`protocol/accumulator.go`). Against layers, "Generation: vision and
    extraction". **kept**: extraction streams through it.
24. **The SSE decoder implements the whole standard, including a reconnection time nothing reads**
    (`internal/sse/sse.go`, `Decoder.Retry`). Against `principles/minimal-footprint.md`.
    **dropped**: `Decoder.Retry`, since the client never reconnects. The rest of the decoder is
    kept.

### Protocol and formats

25. **The model is Open Responses, spec 2026-04-24, in its stateless subset, under the spec's names,
    with a native tier in `Native`** (`protocol`; the spike's third client rule). Against
    `principles/service-tiers.md`, "Standard" and "Native", and `baseline-standards.md`. **kept**:
    the standard tier is the external standard and no wider, and a native engine parameter stays raw
    JSON, never a typed field.
26. **The protocol carries more than either caller uses**: function tools, tool choice, the
    reasoning round trip, reasoning's summary, verbosity, image detail, audio input, and the refusal
    events (`protocol`; each format). Against layers, "What neither caller uses", and ai-strategy,
    "The four layers". **kept**: the layers note lists what two callers use, not the client's scope,
    which is chat with tools and reasoning; each passes against set A (ai-strategy, "The spike's
    answer", item 1).
27. **`Validate` refuses a `MaxOutputTokens` of 1 through 15 whatever the format**
    (`protocol/request.go`, `MinMaxOutputTokens`). Against `principles/service-tiers.md`,
    "Standard". **kept**: 16 is the spec's minimum, and the same request must be valid in every
    format.
28. **`format.Format` is the one contract, and `format` also holds the OpenAI-style error decoder
    the root uses** (`format/format.go`, `format/error.go`). Against `principles/service-tiers.md`,
    "Providers are adapters behind one interface". **kept**: the contract sits in the library,
    outside the root, so a format never imports the client.
29. **`responses.New(opts Options)` takes one options struct whose zero value is the spec's form**
    (`format/responses/format.go`). Against `construction-and-execution.md`, "The rule". **kept**:
    it already has the rule's shape.
30. **`format/messages` refuses a request with no `MaxOutputTokens` and one with audio input, and
    drops the hints it has no form for** (`format/messages/encode.go`, `doc.go`). Against
    `principles/validation-first.md`. **kept**: what would change the request's meaning is refused
    before anything is sent, and a hint is advice.
31. **The formats' package documentation and the README record what one build of llama.cpp's router
    drops and departs from** (`format/messages/doc.go`, `format/responses/doc.go`, `README.md`).
    Against `tests-and-docs.md`, "doc.go and godoc": the package comment describes the package's
    API. **changed**: a package comment states what its format sends, refuses, drops, and
    normalizes. The account of router build b11669 moves to the testdata READMEs that record it.

### Embeddings, transcription, and models

32. **`Embed` sends every input in one request and places each vector by its index, as base64 of
    float32** (`embed.go`). Against Cerebro (Embed batching; concurrency bounds stay the caller's)
    and `baseline-standards.md`. **kept**: `Input` is the batch, and how large a batch is, and how
    many run at once, are the caller's.
33. **`EmbeddingUsage` counts `PromptTokens` where `protocol.Usage` and `TranscriptionUsage` count
    `InputTokens`** (`embed.go`). Against layers, "docinfer's go-ai use", whose table maps both.
    **kept**: each usage type carries its endpoint's names, and docinfer maps them in one place.
34. **`Transcribe` reads a one-shot `File` and is never retried** (`transcribe.go`). Against layers,
    "Requests to go-ai", and Cerebro. **changed**: the request can reopen its clip, so `Transcribe`
    joins the one retry loop and honors Retry-After. Each attempt still streams the clip through a
    pipe and never holds it whole.
35. **`Transcribe` always asks for `response_format` json, and leaves the endpoint's other fields to
    `Native`** (`transcribe.go`). Against `principles/resolution.md`. **kept**: json is the one
    format llama.cpp answers, and the reply the client decodes has one shape.
36. **The client has no `/models` listing, and builds only POSTs** (`client.go`, `request`). Against
    ai-strategy, "Repositories and placement" (the client gains a `/models` listing). **changed**:
    the listing is added, as a GET through the same token and retry path.

### Tests

37. **The unit tier is black-box and hermetic** (every `_test.go`; `.golangci.yml`, `testpackage`;
    `export_test.go`, whose one hook replaces the backoff's jitter). Against `tests-and-docs.md`,
    "The unit tier: black-box and hermetic". **kept**: the hook is a probe the API can't reach
    deterministically, which the page allows.
38. **`internal/clienttest` is the loopback server the module's test packages share**
    (`internal/clienttest`). Against `tests-and-docs.md`, "The unit tier: black-box and hermetic": a
    shared helper is a `<pkg>test` package under `internal/`. **kept**, renamed with the root
    package. It stays internal: docinfer drives the client against its own `internal/endpointtest`
    (layers, "docinfer's API").
39. **The integration tier is a root-level `integration` package behind the build tag, run against a
    live router, and the check lints it without running it** (`integration`; `mise.toml`,
    `SPIKE_ROUTER_URL`, which defaults to one machine's name). Against `tests-and-docs.md`,
    "Integration and acceptance suites, and where each runs", and `release-and-ci.md`, "One check
    per repository". **kept**, with the variable renamed and its default removed, so the tier skips
    unless a caller names an endpoint. No CI job runs it, since the models are on the organization's
    own machines.
40. **Where the router drops a hint, the integration tier logs the outcome and passes either way**
    (`integration/router_test.go`, `hinted` and `record`). Against `tests-and-docs.md`: a test
    proves a behavior only if it can fail. **changed**: a scenario asserts what the format sent, or
    what the endpoint does with it, and a check that can't fail is deleted.
41. **Each format's testdata is recorded from the router, streamed and unstreamed for the same
    request, with a README that says how** (`format/*/testdata`). Against `tests-and-docs.md`, "The
    unit tier: black-box and hermetic". **kept**: the stream tests prove a folded stream equals the
    unstreamed reply against real replies.

### Check, CI, and release

42. **A strict depguard rule holds every Go file, tests included, to the standard library and the
    module itself** (`.golangci.yml`). Against `dependencies.md` (two repositories enforce the line
    with depguard) and `principles/minimal-footprint.md`. **kept**, with the allow list on go-ai's
    module path. The layout section states the rule for each module (planned).
43. **The check adds a lint under the integration tag and shellcheck over the scripts**
    (`mise.toml`). Against `release-and-ci.md`, "One check per repository". **kept**: the tagged
    lint, so the tier always compiles. **dropped**: shellcheck, which go-ai's check already omits
    and no page asks for.
44. **There is no `upgrade` task** (`mise.toml`). Against `release-and-ci.md`, "Currency and
    upgrade" and "Tasks". **changed**: go-ai gains `mise run upgrade`; its `mise.toml` has `check`
    and `currency` alone.
45. **The release workflow names one changelog and takes `v*` tags**
    (`.github/workflows/release.yml`). Against `release-and-ci.md`, "Releases", and
    `principles/independent-releases.md`. **changed**: go-ai has two artifacts, the root module and
    `tools/infer`, each with its own changelog and tag namespace, and the workflow derives both from
    the tag. The layout section states the tags (planned).
46. **The changelog is in Keep-a-Changelog form, with link references** (`CHANGELOG.md`). Against
    `release-and-ci.md`, "Releases". **kept** as a form. The spike's entries are not carried:
    go-ai's tags start fresh under its own module path.

### README and documentation

47. **Each package has a `doc.go` that inventories its exported identifiers** (every package's
    `doc.go`). Against `tests-and-docs.md`, "doc.go and godoc". **kept**.
48. **The README states the spike's role and names the repositories that build on it, and `doc.go`
    points to it** (`README.md`, `doc.go`). Against `principles/downward-dependencies.md` (a
    repository never names its dependents) and the Go Elemental README, "Member repositories" (a
    README states the repository's place in the standard and the principles it enhances).
    **changed**: go-ai's README states its tier, its dependency line beside its link to the
    standard, and its API by example, and names no caller.
49. **The spike's three client rules** (`STANDARDS.md`, "The client"): no timeout, retry, or limit
    unless the caller sets one; a format chosen by typed constructor; a native parameter through the
    raw-JSON field. Against findings 9, 3, and 25. **kept**, all three. Their wording in go-ai's
    `STANDARDS.md` is written with the other judgement calls (planned).
