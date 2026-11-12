# design

go-ai's design, reviewed from spike-ai-pipeline-client v0.4.0: what the spike is measured against,
what go-ai keeps, changes, and drops from it, and the surface the library and infer tasks of
v1.ai.client build to. It is provisional until the library task builds it.

## Evaluation

Fable 5.2's evaluation of the spike at tag v0.4.0, one finding per issue or simplification. Each
names the spike's package or file, what it is measured against, and a verdict with its reason:
**kept** as the spike has it, **changed** in go-ai, or **dropped**. Where a finding changes a
signature, the surface section states the signature.

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
   own tests do. **changed**: the root package is `ai`, which reads with the repository's name, and
   its error prefix follows (the surface section, decision 1).
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
   by context; concurrency bounds stay the caller's). **kept**: every bound is the context's, the
   caller's transport's, or an option the caller sets, whose zero value sets none, and a `Client`
   stays safe for concurrent use with no limit of its own.
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
    and a header of its own only if the gateway asks for one. **kept**. One gap stood beside it:
    Anthropic's API takes its key in `x-api-key`, so `format/messages` reached that API only through
    a `RoundTripper` of the caller's. The options gain fixed request headers, `Options.Header`,
    which serve both (the surface section, decision 3).

### Retry and errors

13. **Retry is opt-in, on a 408, 429, or 503 reply or a connection failure before any reply, with
    full-jitter backoff; a Retry-After replaces the backoff, and a wait past `MaxDelay` or the
    context's deadline returns the last error at once** (`retry.go`). Against Cerebro (the gateway's
    429, 503, and Retry-After) and layers, "The client". **changed**: the set adds 502 and 504,
    which a gateway answers for an upstream that failed or stalled (the surface section, decision
    8). The rest is the behavior both callers are written to, and one loop serves every operation
    whose body can be sent again.
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
    `OutputTextDelta` as progress. go-ai ships no encoder of them as Open Responses' server-sent
    events (the surface section, decision 4).
22. **A stream has no idle bound** (`stream.go`): after the reply's header a stalled body waits for
    the context, and the decoder reads past llama.cpp's pings. Against `timeouts.md`, "A store's
    per-try deadline bounds one operation", where go-storage bounds each read of a body.
    **changed**: the options gain `StreamIdleTimeout`, zero by default, which bounds each read of a
    started stream and resets on any bytes, a ping included. A caller's timer sees only events, so
    it can't tell a stalled endpoint from one still pinging (the surface section, decision 7).
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

## Surface

go-ai's base module as signatures, from the spike at v0.4.0 with the evaluation's changes
applied. The library task builds it (planned). The module path is `github.com/standards-lab/go-ai`;
its packages are the root, `protocol`, `format`, and the three formats below `format`. Each block
lists declarations only; an unexported field or helper the spike has stays as it is unless a
finding changes it.

### What this section settles

The nine items the evaluation left open, each decided with its reason:

1. **The root package is `ai`** (finding 2). It reads with the repository, as `storage` and
   `database` do, so `ai.New`, `ai.Client`, and `ai.APIError` need no alias, and the error prefix
   is `ai: `.
2. **`New` takes its three required inputs positionally, then one `Options` struct**
   (findings 6 to 8). Three is under `construction-and-execution.md`'s four, and a client is a
   value the caller keeps, not a configured thing run once, so the struct-with-a-method form
   doesn't apply. The struct is `Options`, not `Config`: in Go Elemental a `Config` is a go-core
   configuration block, as go-storage's and go-database's are, which go-ai takes none of
   (finding 10), and `responses.Options` already names the format's. A nil HTTP client or format
   panics by name (`lifecycle-and-context.md`, "The wiring rule"); a base URL, policy, or header
   that can't be used is an error.
3. **`Options.Header` carries fixed request headers.** It serves Anthropic's `x-api-key` and a
   gateway's consumer-key header, should the gateway ask for one, without a `RoundTripper` of the
   caller's. `Token` stays the bearer token, called once per attempt. A header the format sets,
   such as `anthropic-version`, replaces one of the same name; one naming `Content-Type`,
   `Accept`, or `Authorization`, which the client sets, is refused by `New`.
4. **go-ai ships no Open Responses SSE encoder; Cerebro maps the events itself** (finding 21).
   Cerebro relays progress as its own event shapes, not as Open Responses' wire, and Athena's chat
   stream takes go-web-sdk's SSE helper. The events are a closed set of Go values, each with its
   sequence number, so a relay is one type switch; an encoder would be a type before its need.
5. **`APIError.RetryAfter` is a `time.Duration`** (finding 14): the wait the reply's Retry-After
   asked for, measured from the reply's arrival, zero for none. A duration needs no clock and no
   zone, so `utc-times.md` has nothing to hold it to, and it is what a redelivery's delay takes.
6. **`Client.Models` lists the endpoint's models** (finding 36): a GET of the OpenAI-compatible
   `models` route whatever the Format, as `Embed` is, through the same token, header, query, and
   retry path. A GET is safe to repeat, so its retry runs nothing twice.
7. **`Options.StreamIdleTimeout` is an idle bound the caller sets; zero sets none** (finding 22).
   It bounds each read of a started stream's body, the clock running only while a read waits,
   and any bytes reset it, llama.cpp's ping included. A caller's own timer sees only events, so it
   can't tell an endpoint that stalls from one still pinging, and Cerebro is long-running; this is
   go-storage's `ReadIdleTimeout` (`timeouts.md`), without its default, since go-ai sets no policy
   (finding 9). An idle stream ends with an error matching `io.ErrUnexpectedEOF` and
   `os.ErrDeadlineExceeded`.
8. **The retry set is 408, 429, 502, 503, and 504, and a connection failure before any reply**
   (findings 13 and 15). A gateway answers 502 and 504 for an upstream that failed or stalled,
   which a later attempt can mend, and docinfer's mapping already treats both as transient
   (layers, "docinfer's go-ai use"). A run twice on the endpoint is finding 16's, which the caller
   accepts. A header timeout and net/http's idle-connection error stay unretried, as finding 15
   has it.
9. **`Embed`, `Transcribe`, and `Models` on a client of any format are documented, not refused**
   (finding 5). Each goes to the base URL's OpenAI-compatible route whatever the Format; llama.cpp's
   router serves all three beside `/messages`, which `cmd/docinfer`'s endpoints rely on, and an
   endpoint without the route, such as Anthropic's API for embeddings, answers with an `*APIError`.

### The root package

```go
// Package ai is go-ai's model client: one transport over the protocol, in the wire format its
// constructor is given, with embeddings, transcription, and the model listing beside it.
package ai // import "github.com/standards-lab/go-ai"

// TokenSource returns the bearer token for one attempt. An empty token sends none.
type TokenSource func(ctx context.Context) (string, error)

// Options are New's optional inputs. The zero value selects every default: no query, no token,
// no header, no retry, and no idle bound. New copies Query, Header, and Retry.
type Options struct {
    Query             url.Values    // added to every request's URL
    Token             TokenSource   // nil sends no Authorization header
    Header            http.Header   // fixed headers on every request, such as x-api-key
    Retry             RetryPolicy   // the zero value retries nothing
    StreamIdleTimeout time.Duration // bounds each read of a started stream; 0 sets none
}

// RetryPolicy resends a request on a 408, 429, 502, 503, or 504 reply, or a connection failure
// before any reply, never a started stream. It waits a full-jitter exponential backoff, which a
// Retry-After replaces; a wait past MaxDelay or ctx's deadline returns the last error at once.
type RetryPolicy struct {
    MaxAttempts int           // the most requests for a call, the first included; 0 sends one
    BaseDelay   time.Duration // the cap on the first wait, doubled for each after it
    MaxDelay    time.Duration // the cap on any wait, and the longest Retry-After honored
}

// Client makes requests of one endpoint in one wire format. It is safe for concurrent use when
// its TokenSource and format are, and sets no limit of its own.
type Client struct{ /* unexported */ }

// New returns a Client and makes no request. A nil httpClient or wireFormat panics by name.
// A baseURL that isn't an absolute http or https URL without query or fragment, a negative
// attempt count or delay, a base delay above the most, or a Header naming Content-Type, Accept,
// or Authorization is an error.
func New(baseURL string, httpClient *http.Client, wireFormat format.Format,
    opts Options) (*Client, error)

// APIError is a non-2xx reply, returned wrapped and reached with errors.As.
type APIError struct {
    Status     int           // the HTTP status code
    Message    string        // the endpoint's error message, when the body gave one
    Type       string        // the endpoint's error type, such as "invalid_request_error"
    Code       string        // the endpoint's error code; a numeric one as its decimal text
    Body       []byte        // the reply's body
    RetryAfter time.Duration // the reply's Retry-After, as a wait from its arrival; 0 for none
}
func (e *APIError) Error() string

// Generate validates and encodes req, then sends it through the retry loop.
func (c *Client) Generate(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

// GenerateStream is lazy: each range sends the request. Each pair is an event or an error; the
// events run from ResponseCreated to one terminal event (completed, incomplete, failed).
func (c *Client) GenerateStream(ctx context.Context,
    req *protocol.Request) iter.Seq2[protocol.Event, error]

// EmbeddingRequest goes to the OpenAI-compatible embeddings route whatever the format.
type EmbeddingRequest struct {
    Model  string
    Input  []string       // the batch, sent as one request; its size is the caller's
    Native jsontext.Value // members merged into the body's top level
}
type Embeddings struct {
    Vectors [][]float32 // Vectors[i] is Input[i]'s
    Usage   EmbeddingUsage
}
type EmbeddingUsage struct{ PromptTokens, TotalTokens int }
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*Embeddings, error)

// TranscriptionRequest goes to the OpenAI-compatible transcription route whatever the format.
type TranscriptionRequest struct {
    Model string
    // Open opens the clip once per attempt; the attempt streams it as it uploads, then closes it.
    Open     func(ctx context.Context) (io.ReadCloser, error)
    FileName string     // names the clip in the upload, such as "clip.wav"
    Language string     // empty sends none
    Prompt   string     // empty sends none
    Native   url.Values // form fields beside the typed ones
}
type Transcription struct {
    Text  string
    Usage TranscriptionUsage
}
type TranscriptionUsage struct{ InputTokens, OutputTokens, TotalTokens int }
// Transcribe sends through the retry loop, opening the clip again for each attempt.
func (c *Client) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error)

// Model is one entry of the endpoint's model list.
type Model struct{ ID string }
// Models GETs the OpenAI-compatible models route whatever the format, through the retry loop.
func (c *Client) Models(ctx context.Context) ([]Model, error)
```

### protocol

Open Responses, spec 2026-04-24, in its stateless subset, under the spec's names (finding 25). A
client extension's wire type carries go-ai's slug, `go-ai` (finding 4).

```go
package protocol // import "github.com/standards-lab/go-ai/protocol"

type Request struct {
    Model           string
    Instructions    string
    Input           []Item
    Tools           []FunctionTool
    ToolChoice      ToolChoice     // ToolChoiceMode, FunctionToolChoice, or AllowedToolChoice
    Reasoning       ReasoningParam
    Text            TextParam
    MaxOutputTokens int            // 0, or at least MinMaxOutputTokens
    Native          jsontext.Value // the native tier, merged into the body's top level
}
const MinMaxOutputTokens = 16
func (r *Request) Validate() error

type ReasoningParam struct {
    Effort  ReasoningEffort
    Summary ReasoningSummary
}
type ReasoningEffort string  // ReasoningEffortNone, Low, Medium, High, XHigh
type ReasoningSummary string // ReasoningSummaryAuto, Concise, Detailed
type TextParam struct {
    Format    TextFormat // PlainTextFormat or JSONSchemaFormat
    Verbosity Verbosity
}
type TextFormat interface{ textFormat() }
type PlainTextFormat struct{}
type JSONSchemaFormat struct {
    Name, Description string
    Schema            jsontext.Value
    Strict            bool
}
type Verbosity string // VerbosityLow, Medium, High

type Item interface{ item() } // Message, FunctionCall, FunctionCallOutput, Reasoning
type Message struct {
    ID      string
    Status  Status
    Role    Role
    Content []ContentPart
}
type Role string // RoleUser, RoleSystem, RoleDeveloper, RoleAssistant
type ContentPart interface{ contentPart() } // the parts below
type InputText struct{ Text string }
type InputImage struct {
    ImageURL string // an http(s) URL, or DataURL's
    Detail   ImageDetail
}
type ImageDetail string // ImageDetailAuto, Low, High
func DataURL(mediaType string, data []byte) string
type InputAudio struct { // the go-ai:input_audio extension
    Data   []byte
    Format AudioFormat
}
type AudioFormat string // AudioFormatWAV, AudioFormatMP3
type OutputText struct{ Text string }
type Refusal struct{ Refusal string }
type ReasoningText struct{ Text string }
type SummaryText struct{ Text string }
type FunctionCall struct {
    ID                     string
    Status                 Status
    CallID, Name, Arguments string
}
type FunctionCallOutput struct {
    ID             string
    Status         Status
    CallID, Output string
    OutputParts    []ContentPart
}
type Reasoning struct {
    ID               string
    Status           Status
    Content          []ReasoningText
    Summary          []SummaryText
    EncryptedContent string // the endpoint's, or a format's go-ai:<format>/v1: envelope
}
type Status string // StatusInProgress, StatusCompleted, StatusIncomplete, StatusFailed

type FunctionTool struct {
    Name, Description string
    Parameters        jsontext.Value
    Strict            bool
}
type ToolChoice interface{ toolChoice() }
type ToolChoiceMode string // ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired
type FunctionToolChoice struct{ Name string }
type AllowedToolChoice struct {
    Tools []FunctionToolChoice
    Mode  ToolChoiceMode
}

type Response struct {
    Output            []Item
    Status            Status
    IncompleteDetails *IncompleteDetails
    Error             *ResponseError
    Usage             Usage
}
type IncompleteDetails struct{ Reason IncompleteReason }
type IncompleteReason string // ReasonMaxOutputTokens, ReasonContentFilter
type ResponseError struct{ Code, Message string }
type Usage struct {
    InputTokens, OutputTokens, TotalTokens int
    OutputTokensDetails                    OutputTokensDetails
}
type OutputTokensDetails struct{ ReasoningTokens int }

// Event is the closed set of streaming events, each carrying its SequenceNumber.
type Event interface{ event() }
type ResponseCreated struct {
    SequenceNumber int
    Response       *Response
}
// ResponseCompleted, ResponseIncomplete, and ResponseFailed, the terminal events, are shaped as
// ResponseCreated.
type ErrorEvent struct {
    SequenceNumber int
    Error          ErrorPayload
}
type ErrorPayload struct{ Type, Code, Message, Param string }
type OutputItemAdded struct {
    SequenceNumber, OutputIndex int
    Item                        Item
}
// OutputItemDone is shaped as OutputItemAdded.
type ContentPartAdded struct {
    SequenceNumber            int
    ItemID                    string
    OutputIndex, ContentIndex int
    Part                      ContentPart
}
// ContentPartDone is shaped as ContentPartAdded.
type OutputTextDelta struct {
    SequenceNumber            int
    ItemID                    string
    OutputIndex, ContentIndex int
    Delta                     string
}
// OutputTextDone (Text), RefusalDelta (Delta), RefusalDone (Refusal), ReasoningDelta (Delta),
// and ReasoningDone (Text) are shaped as OutputTextDelta, with the field named.
type FunctionCallArgumentsDelta struct {
    SequenceNumber int
    ItemID         string
    OutputIndex    int
    Delta          string
}
// FunctionCallArgumentsDone is shaped as FunctionCallArgumentsDelta, with Arguments for Delta.

// Accumulator folds a stream's events, in the caller's loop, into the Response Generate returns.
type Accumulator struct{ /* unexported */ }
func (a *Accumulator) Add(e Event) error
// Response is the folded Response: after a break, the partial one, in StatusInProgress.
func (a *Accumulator) Response() *Response
```

### format and the three formats

```go
package format // import "github.com/standards-lab/go-ai/format"

// Format is a wire format, chosen by its package's constructor at the composition root.
type Format interface {
    Route() string
    EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error)
    DecodeResponse(body []byte) (*protocol.Response, error)
    EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error)
    NewStreamDecoder() StreamDecoder
    DecodeError(body []byte) (message, typ, code string)
}
// StreamDecoder turns one stream's event data into protocol events; it serves one stream.
type StreamDecoder interface {
    Decode(data []byte) ([]protocol.Event, error)
    End() ([]protocol.Event, error)
}
// DecodeOpenAIError decodes {"error": {"message", "type", "code"}}, for the OpenAI routes.
func DecodeOpenAIError(body []byte) (message, typ, code string)

package completions // import "github.com/standards-lab/go-ai/format/completions"
type Format struct{}
func New() *Format // Chat Completions, on "chat/completions"

package messages // import "github.com/standards-lab/go-ai/format/messages"
type Format struct{}
// New is Anthropic Messages, on "messages", sending its anthropic-version header. It refuses a
// request with no MaxOutputTokens or with audio input, and drops the hints it has no form for.
func New() *Format

package responses // import "github.com/standards-lab/go-ai/format/responses"
// Options adjusts the format to an endpoint that departs from the spec; the zero value is the
// spec's form.
type Options struct {
    ResendReasoningText bool // send a reasoning item's text back, for llama.cpp's router
}
type Format struct{ /* unexported */ }
func New(opts Options) *Format // Open Responses, on "responses"
```

### The name map

Every name layers' "go-ai's surface" writes as code maps to exactly one go-ai name. The section's
names are the spike's; a note gives the reason where the go-ai name differs, or where its type or
behavior changes under the same name. A field written bare in a layers code block is qualified by
its type here.

| layers | go-ai | note |
| --- | --- | --- |
| `client` (the package) | `ai` | decision 1; finding 2 |
| `TokenSource` | `ai.TokenSource` | |
| `Config` | `ai.Options` | decision 2: optional inputs only; findings 6 and 10 |
| `Config.BaseURL` | `ai.New`'s `baseURL` | decision 2: a required input, positional |
| `Config.Query` | `ai.Options.Query` | |
| `Config.Token` | `ai.Options.Token` | |
| `Config.HTTP` | `ai.New`'s `httpClient` | decision 2: positional; nil panics (finding 8) |
| `Config.Format` | `ai.New`'s `wireFormat` | decision 2: positional; nil panics (finding 8) |
| `Config.Retry` | `ai.Options.Retry` | a value, its zero retrying nothing (finding 7) |
| `RetryPolicy` | `ai.RetryPolicy` | the set adds 502 and 504 (decision 8) |
| `RetryPolicy.MaxAttempts` | `ai.RetryPolicy.MaxAttempts` | 0 sends one, as 1 does (finding 7) |
| `RetryPolicy.BaseDelay` | `ai.RetryPolicy.BaseDelay` | |
| `RetryPolicy.MaxDelay` | `ai.RetryPolicy.MaxDelay` | |
| `New` | `ai.New` | positional inputs, then `Options` (decision 2) |
| `Client` (`client.Client`) | `ai.Client` | |
| `APIError` | `ai.APIError` | gains `RetryAfter` (decision 5; finding 14) |
| `APIError.Status` | `ai.APIError.Status` | |
| `APIError.Message` | `ai.APIError.Message` | |
| `APIError.Type` | `ai.APIError.Type` | |
| `APIError.Code` | `ai.APIError.Code` | |
| `APIError.Body` | `ai.APIError.Body` | |
| `Generate` | `ai.Client.Generate` | |
| `GenerateStream` | `ai.Client.GenerateStream` | an idle bound if set (decision 7) |
| `protocol.Request` | `protocol.Request` | |
| `Request.Model` | `protocol.Request.Model` | |
| `Request.Instructions` | `protocol.Request.Instructions` | |
| `Request.Input` | `protocol.Request.Input` | |
| `Request.Reasoning` | `protocol.Request.Reasoning` | |
| `Request.Text` | `protocol.Request.Text` | |
| `Request.MaxOutputTokens` | `protocol.Request.MaxOutputTokens` | |
| `MinMaxOutputTokens` | `protocol.MinMaxOutputTokens` | |
| `Item` | `protocol.Item` | |
| `Message` | `protocol.Message` | |
| `Message.Role` | `protocol.Message.Role` | |
| `RoleUser` | `protocol.RoleUser` | |
| `Message.Content` | `protocol.Message.Content` | |
| `InputImage` | `protocol.InputImage` | |
| `InputImage.ImageURL` | `protocol.InputImage.ImageURL` | |
| `DataURL` | `protocol.DataURL` | |
| `InputText` | `protocol.InputText` | |
| `ReasoningParam` | `protocol.ReasoningParam` | |
| `ReasoningParam.Effort` | `protocol.ReasoningParam.Effort` | |
| `protocol.ReasoningEffort` | `protocol.ReasoningEffort` | |
| `TextParam` | `protocol.TextParam` | |
| `TextParam.Format` | `protocol.TextParam.Format` | |
| `JSONSchemaFormat` | `protocol.JSONSchemaFormat` | |
| `JSONSchemaFormat.Name` | `protocol.JSONSchemaFormat.Name` | |
| `JSONSchemaFormat.Schema` | `protocol.JSONSchemaFormat.Schema` | |
| `JSONSchemaFormat.Strict` | `protocol.JSONSchemaFormat.Strict` | |
| `Response` | `protocol.Response` | |
| `Response.Output` | `protocol.Response.Output` | |
| `Response.Status` | `protocol.Response.Status` | |
| `StatusIncomplete` | `protocol.StatusIncomplete` | |
| `Response.IncompleteDetails` | `protocol.Response.IncompleteDetails` | |
| `IncompleteDetails` (the type) | `protocol.IncompleteDetails` | |
| `IncompleteDetails.Reason` | `protocol.IncompleteDetails.Reason` | |
| `max_output_tokens` | `protocol.ReasonMaxOutputTokens` | the reason's wire value |
| `Response.Error` | `protocol.Response.Error` | |
| `ResponseError` | `protocol.ResponseError` | |
| `Response.Usage` | `protocol.Response.Usage` | |
| `protocol.Usage` | `protocol.Usage` | |
| `Usage.InputTokens` | `protocol.Usage.InputTokens` | |
| `Usage.OutputTokens` | `protocol.Usage.OutputTokens` | |
| `Accumulator` | `protocol.Accumulator` | |
| `Accumulator.Add` | `protocol.Accumulator.Add` | |
| `Accumulator.Response` | `protocol.Accumulator.Response` | |
| `protocol.Event` | `protocol.Event` | |
| `ResponseCreated` | `protocol.ResponseCreated` | |
| `OutputTextDelta` | `protocol.OutputTextDelta` | |
| `OutputTextDelta.Delta` | `protocol.OutputTextDelta.Delta` | |
| `ResponseCompleted` | `protocol.ResponseCompleted` | |
| `ResponseIncomplete` | `protocol.ResponseIncomplete` | |
| `ResponseFailed` | `protocol.ResponseFailed` | |
| `OutputText` | `protocol.OutputText` | |
| `EmbeddingRequest` | `ai.EmbeddingRequest` | |
| `EmbeddingRequest.Model` | `ai.EmbeddingRequest.Model` | |
| `EmbeddingRequest.Input` | `ai.EmbeddingRequest.Input` | |
| `Embeddings` | `ai.Embeddings` | |
| `Embeddings.Vectors` | `ai.Embeddings.Vectors` | |
| `Embeddings.Usage` | `ai.Embeddings.Usage` | |
| `EmbeddingUsage` | `ai.EmbeddingUsage` | |
| `EmbeddingUsage.PromptTokens` | `ai.EmbeddingUsage.PromptTokens` | finding 33 |
| `EmbeddingUsage.TotalTokens` | `ai.EmbeddingUsage.TotalTokens` | |
| `Embed` | `ai.Client.Embed` | |
| `TranscriptionRequest` | `ai.TranscriptionRequest` | |
| `TranscriptionRequest.Model` | `ai.TranscriptionRequest.Model` | |
| `TranscriptionRequest.File` | `ai.TranscriptionRequest.Open` | reopened per attempt (finding 34) |
| `TranscriptionRequest.FileName` | `ai.TranscriptionRequest.FileName` | |
| `TranscriptionRequest.Language` | `ai.TranscriptionRequest.Language` | |
| `TranscriptionRequest.Prompt` | `ai.TranscriptionRequest.Prompt` | |
| `Transcription` | `ai.Transcription` | |
| `Transcription.Text` | `ai.Transcription.Text` | |
| `Transcription.Usage` | `ai.Transcription.Usage` | |
| `TranscriptionUsage` | `ai.TranscriptionUsage` | |
| `TranscriptionUsage.InputTokens` | `ai.TranscriptionUsage.InputTokens` | |
| `TranscriptionUsage.OutputTokens` | `ai.TranscriptionUsage.OutputTokens` | |
| `TranscriptionUsage.TotalTokens` | `ai.TranscriptionUsage.TotalTokens` | |
| `Transcribe` | `ai.Client.Transcribe` | in the one retry loop, honoring Retry-After (finding 34) |
| `format.Format` | `format.Format` | |
| `format/completions` | package `completions` | |
| `completions.New` | `completions.New` | |
| `format/messages` | package `messages` | |
| `format/responses` | package `responses` | |
| `FunctionCall` | `protocol.FunctionCall` | |
| `FunctionCallOutput` | `protocol.FunctionCallOutput` | |
| `Reasoning` (the items) | `protocol.Reasoning` | |
| `encrypted_content` | `protocol.Reasoning.EncryptedContent` | go-ai's slug (finding 4) |
| `ReasoningParam.Summary` | `protocol.ReasoningParam.Summary` | |
| `InputAudio` | `protocol.InputAudio` | wire type `go-ai:input_audio` (finding 4) |
| `Native` (the tiers) | `Native` | each request's native-tier field (note below) |
| `/models` | `ai.Client.Models` | finding 36; decision 6 |
| `tools/infer` | `tools/infer` | the nested module; the layout section states it (planned) |

The section's prose also names kinds rather than identifiers, each one of the protocol's: function
tools are `protocol.FunctionTool`, tool choice `protocol.ToolChoice`, verbosity
`protocol.TextParam.Verbosity`, image detail `protocol.InputImage.Detail`, and the refusal events
`protocol.RefusalDelta` and `protocol.RefusalDone`. "The `Native` tiers" is one name, the field
`Native`, which `protocol.Request`, `ai.EmbeddingRequest`, and `ai.TranscriptionRequest` each
carry. `TranscriptionRequest.Open` takes a context, which the layers note's sketch of it doesn't:
a clip opened from a store is a read that the call's context bounds. The harness-session surface
is outside this review. The names docinfer owns, such as `docinfer.Pipeline`, `Bounds`,
`Options.Final`, a chunk's `Error`, and the outcomes `incomplete` and `failed`, the spike's file
names, and the standard library's, such as `http.Client`, map to nothing here.

## Requests

What docinfer and Cerebro ask of go-ai, and what the coordinator's notes ask beside them, each with
its decision and the v1.ai.client task that owns it. "data-intelligence" is standards-lab's
`context/data-intelligence.md`; the other sources are cited as the evaluation cites them. The
decisions are stated in full above; a cell names its finding or decision. Concurrency bounds stay
the caller's throughout.

| request | who asks | decision | owner |
| --- | --- | --- | --- |
| A transcription clip the retry loop can reopen | docinfer (layers, "Requests to go-ai"); Cerebro | `TranscriptionRequest.Open`, once per attempt; `Transcribe` joins the retry loop (finding 34) | library |
| Retry-After on the API error | docinfer (layers, "Embeddings and transcription") | `APIError.RetryAfter`, a duration, beside the reopenable clip (finding 14; decision 5) | library |
| Per-call timeouts and cancellation by context | Cerebro; layers, "The client" | the caller's context ends a call or closes its stream; no library timeout; an idle bound when set (findings 9, 20; decision 7) | library |
| The consumer key, in a header of its own if the gateway asks | Cerebro; layers, "Requests to go-ai" | the bearer token from `Options.Token`; `Options.Header` should the gateway ask (finding 12; decision 3) | library |
| Embed batching | Cerebro | `EmbeddingRequest.Input` is the batch, one request; its size and fan-out are the caller's (finding 32) | library |
| Stream events Cerebro can relay | Cerebro | the typed event set with sequence numbers, mapped by Cerebro; no SSE encoder (finding 21; decision 4) | library |
| Retry proven against the live gateway's 429, 503, and Retry-After | Cerebro; layers, "Requests to go-ai" | proven against the live gateway once spike-model-gateway runs; the release doesn't wait (finding 17) | acceptance |
| The surface docinfer calls kept | docinfer (layers, "Requests to go-ai"); data-intelligence, "The layers" | kept, under the name map | library |
| The gateway's capacity replies retried as go-ai expects | data-intelligence, "What it asks of the goals beneath it" (v1.ai.hosting) | 408, 429, 502, 503, 504, and a connection failure before any reply (decision 8) | library |
| A `/models` listing | ai-strategy, "Repositories and placement" | `Client.Models`, a GET through the same token and retry path (finding 36; decision 6) | library |
| infer's `models` subcommand | ai-strategy, "Repositories and placement" | a listing over `Client.Models`; its flags and output are infer's | infer |
| infer rerun against set A | data-intelligence, "Gates" | run through the formats docinfer's endpoints use | acceptance |
