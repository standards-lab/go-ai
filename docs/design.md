# design

go-ai's design: the rules it holds beyond its package documentation, the surface of its base
module, its modules, the reasoning behind both modules' unexported code and tests, and the
alternatives it rules out. The judgement calls it settles are go-ai's `STANDARDS.md`. go-ai
began as spike-ai-pipeline-client v0.4.0, "the spike" below.

The measures are cited in short. A bare page name, such as `dependencies.md`, is a Go Elemental
principle under the architecture repository's `standards/go-elemental/principles/`;
`principles/<page>` is an architecture principle; a quoted name is the page's section.

## Rules

What go-ai holds that its package documentation, its `STANDARDS.md`, and the rejected
alternatives leave unsaid, each rule with its reason.

1. **Embeddings and transcription sit outside the Format, on the OpenAI-compatible routes**
   (`embed.go`, `transcribe.go`). Open Responses defines neither operation, and where no formal
   standard exists, the common interface is the operations the target APIs share, kept minimal
   (`principles/service-tiers.md`, "Standard"): the OpenAI-compatible shape is the one the model
   engines and the gateway serve.
2. **Retry is proven by the unit tier alone** (`retry_test.go`, on `testing/synctest`), and the
   integration tiers carry no retry or gateway scenario (`tests-and-docs.md`, "Integration and
   acceptance suites, and where each runs"). A live gateway's 429, 503, and Retry-After are proven
   where the gateway is, by the repository that owns all testing through it.
3. **Each format's testdata is recorded from real endpoints, streamed and unstreamed for the same
   request, and each profile field is backed by one recorded reply** (`format/*/testdata`;
   `tests-and-docs.md`, "The unit tier: black-box and hermetic"). The stream tests then prove a
   folded stream equals the unstreamed reply against real replies. A profile field declares a gap
   seen on one deployment, so its recorded reply, a 400 or a 200 whose reply shows the departure,
   is what the unit tier serves on loopback through `Client.Generate` to prove the field: under
   the zero profile, a 400 reaches the caller as an `*APIError` and a 200 decodes as recorded;
   with the gap declared, a refusing gap is refused before the token source is called, an
   `allowed_tools` choice is narrowed, and reasoning text is resent. A gap that didn't reproduce
   declares no field, and its set's README says so.

## Surface

go-ai's base module. The module path is `github.com/standards-lab/go-ai`; its packages are the
root, `protocol`, `format`, and the three formats below `format`. `protocol` is Open Responses,
spec 2026-04-24, in its stateless subset, under the spec's names. A client extension's wire type
carries go-ai's slug, `go-ai`. Each package's `doc.go` and godoc state its declarations.

### What this section settles

Nine decisions, each with its reason:

1. **The root package is `ai`.** It reads with the repository, as `storage` and `database` do, so
   `ai.New`, `ai.Client`, and `ai.APIError` need no alias, and the error prefix is `ai: `.
2. **`New` takes its three required inputs positionally, then one `Options` struct.** Three is under
   `construction-and-execution.md`'s four, and a client is a value the caller keeps, not a
   configured thing run once, so the struct-with-a-method form doesn't apply. The struct is
   `Options`, not `Config`: in Go Elemental a `Config` is a go-core configuration block, as
   go-storage's and go-database's are, which go-ai takes none of, and each format's `Options` names
   its profile. A nil HTTP client or format panics by name (`lifecycle-and-context.md`, "The wiring
   rule"); a base URL, policy, or header that can't be used is an error.
3. **`Options.Header` carries fixed request headers.** It serves Anthropic's `x-api-key` and a
   gateway's consumer-key header, should the gateway ask for one, without a `RoundTripper` of the
   caller's. `Token` stays the bearer token, called once per attempt. Names that differ only in
   case are one header. A header the format sets, such as `anthropic-version`, replaces one of
   the same name. `New` refuses one naming `Content-Type`, `Accept`, or `Authorization`, which
   the client sets, and one net/http can't send: a name that isn't a header name, or a value
   with a control character.
4. **go-ai ships no Open Responses SSE encoder; a caller maps the events itself.** A service relays
   progress as its own event shapes, not as Open Responses' wire, and a chat stream's server-sent
   events are go-web-sdk's SSE helper's. The events are a closed set of Go values, each with its
   sequence number, so a relay is one type switch; an encoder would be a type before its need.
5. **`APIError.RetryAfter` is a `time.Duration`:** the wait the reply's Retry-After asked for,
   measured from the reply's arrival, zero for none. The retry loop returns at once when the wait
   is past `MaxDelay` or the context's deadline, and that is when a caller that redelivers needs
   the delay. A duration needs no clock and no zone, so `utc-times.md` has nothing to hold it to,
   and it is what a redelivery's delay takes.
6. **`Client.Models` lists the endpoint's models:** a GET of the OpenAI-compatible `models` route
   whatever the Format, as `Embed` is, through the same token, header, query, and retry path. A GET
   is safe to repeat, so its retry runs nothing twice.
7. **`Options.StreamIdleTimeout` is an idle bound the caller sets; zero sets none.** It bounds each
   read of a started stream's body, the clock running only while a read waits, and any bytes reset
   it, llama.cpp's ping included. A caller's own timer sees only events, so it can't tell an
   endpoint that stalls from one still pinging, and a service's streams can be long-running; this is
   go-storage's `ReadIdleTimeout` (`timeouts.md`), without its default, since go-ai sets no policy.
   An idle stream ends with an error matching `io.ErrUnexpectedEOF` and `os.ErrDeadlineExceeded`.
8. **The retry set is 408, 429, 502, 503, and 504, and a connection failure before any reply.**
   A gateway answers 502 and 504 for an upstream that failed or stalled, which a later attempt
   can mend. A run twice on the endpoint is the caller's to accept, as `RetryPolicy`'s
   documentation says. A header timeout and net/http's idle-connection error stay unretried, as
   the rejected alternatives have it.
9. **`Embed`, `Models`, and `Transcribe` through the route on a client of any format are documented,
   not refused, and `Options.TranscriptionMode` adds a chat mode.** Each goes to the base URL's
   OpenAI-compatible route whatever the Format; llama.cpp's router serves all three beside
   `/messages`, vLLM serves embeddings and models beside every format's route, and an endpoint
   without the route, such as Anthropic's API for embeddings, answers with an `*APIError`.
   Transcription has two modes. `TranscribeRoute`, the zero value, uploads the clip to
   `audio/transcriptions`. `TranscribeChat` is for an endpoint whose audio model has no
   transcription route, as vLLM serving Gemma 4 has none: it sends one request in the Client's own
   format, to its route, with `Model` and one user message of the clip as a `protocol.InputAudio`
   part, its format from `FileName`'s extension (.wav or .mp3), then the instruction as an
   `InputText` part: `Prompt`, or else `TranscriptionInstruction(Language)`, modeled on Gemma 4's
   model card, naming the language when set. `Text` joins the reply's output text, and `Usage` is
   the reply's usage. Chat mode returns a whole transcript or an error, as the route does: a reply
   whose status isn't completed, most often one incomplete because the model stopped at the engine's
   output token bound, which a chat-mode request can't raise, returns an
   `*IncompleteTranscriptError` naming the status, the reason, its cause, and what to do, and no
   `Transcription`. It is not retried, as the same clip meets the same bound again. Chat mode needs
   a format that carries audio, Chat Completions today. Before the clip is opened or anything is
   sent, it encodes a request of the same shape with a placeholder clip, so a format that refuses
   audio, such as Messages, refuses it there; any `Native` form field and an extension other than
   .wav or .mp3 are refused earlier still. `New` refuses any other mode. The mode is the caller's,
   set once per client: probing the route and falling back to chat would cost a request per call and
   hide a misconfigured endpoint, and pinning Chat Completions in the root package would make the
   transport name a format.

## Modules

go-ai's module layout, its dependency line and how the check holds it, `infer`'s nested module,
and the tags each module is released under.

### What this section settles

1. **Two modules: the base module at the root and `tools/infer` nested under it.** go-ai is an
   infrastructure library with no provider, so it has no provider sub-modules; the one nested
   module is the tool, as go-web-service's `tools/slab` is (`topology-and-naming.md`,
   "Module layout per tier"; `principles/tool-beside-library.md`), so the tool's requirements
   never enter the library's module. A nested module's directory is outside its parent's module
   zip, so a caller that imports go-ai never fetches what `infer` requires.
2. **Each `go.mod` says `go 1.27`**, the current minor, as go-core's, go-cli-sdk's, and
   go-storage's do; `mise.toml` pins the toolchain, Go 1.27.4, which CI and a developer's machine
   both read; `mise run upgrade` moves the minor.
3. **The base module requires nothing, and the check holds it there in two steps.** A strict
   depguard rule allows the standard library and the module's own packages in every Go file
   outside `tools/infer`, tests and the integration tier included, so an import outside the line
   fails at the line that makes it; `go mod tidy -diff` then holds `go.mod` to what the files
   import. Beside them, the check asserts that the base `go.mod` has no `require`,
   reading it through `go mod edit -json` and jq, which `mise.toml` already pins. depguard alone
   can be widened by an edit to its allow list; the assertion states the line itself, so moving it
   means changing the check, in a diff a reviewer reads as a change of line. The line is
   mechanical, so it is the check's, not a judgement call in `STANDARDS.md`.
4. **One `.golangci.yml` at the root serves both modules, with a depguard rule per module.**
   golangci-lint reads the nearest configuration above the directory it runs in, so `infer`'s run
   reads the root's. The base rule's files exclude `tools/infer`; a second rule, over
   `tools/infer`'s files, tests and the integration tier included, allows the standard library,
   go-ai's public packages, go-cli-sdk, go-core, and infer's own packages, and denies
   `go-ai/internal` by name, as go-cli-sdk's rule holds it to the standard library and
   go-core (`dependencies.md`).
   Both rules sit in one file, so the line of each module is read in one place.
5. **`go.work` is committed, and `infer` requires a released go-ai.** `go.work` uses `.` and
   `./tools/infer`, and keeps its use-list the same as `GO_MODULES`, as go-storage does. The
   workspace builds a change spanning both modules together before anything is tagged; the check
   sets `GOWORK=off`, so the gate is the committed pins. `tools/infer`'s `go.mod` requires the root
   at a released tag, never by a committed `replace`. Where `infer` needs base changes not yet
   released, a `replace` of the root is a transient bridge, removed once the root is tagged
   (`release-and-ci.md`, "Coordinating a change across modules").
6. **The tags are `v<semver>` for the root and `tools/infer/v<semver>` for `infer`, and each
   module's first tag is `v0.1.0`.** The prefix is the nested module's path, which the module
   proxy resolves (`topology-and-naming.md`, "Release tags"). go-ai starts fresh: the spike's
   `v0.1.0` to `v0.4.0` name `github.com/JaimeStill/spike-ai-pipeline-client`, another module
   path, so no version of go-ai's own precedes them, and its changelog carries none of the spike's
   entries. Both modules stay below 1.0: the surface is open to change until its callers build
   on it, and every library of the standard beneath it, go-core included, is still at v0.
7. **The release workflow takes both tag namespaces.** The release ripples bottom-up: `infer`
   pins the root release it was validated against, so the root is tagged first, and `infer`'s
   versions move on their own. The workflow is go-storage's: the trigger takes `v*` and `**/v*`,
   and the job derives the path prefix and the changelog, `CHANGELOG.md` or
   `tools/infer/CHANGELOG.md`, from the tag, so a module's tag needs no change to the workflow,
   only its changelog.

### The base module

The base module, `github.com/standards-lab/go-ai`, holds the packages of the surface section,
their testdata, the integration tier, and `aitest`, the fake endpoint for tests, beside its
internal packages: `internal/aitest`, go-ai's format-test kit, `internal/sse`, and
`internal/native`.

Each format's testdata is recorded from llama.cpp b11669's router, from vLLM 0.33.0, and, for the
profiles' gaps, from vLLM 0.34.0, and Chat Completions' and Open Responses' also from Azure AI
Foundry, each set with a README that names its engine and build, says how it was recorded, and
records what the engine departs from (rule 3). The integration tier is a root-level package of
the base module: the check builds, vets, and lints it under its build tag, and
`mise run integration` runs it against the endpoints its `AI_<ROLE>_BASE_URL` variables name. No
CI job runs it.

### infer's module

`infer` is the convenience CLI over the base module's surface, a Go Elemental CLI laid out from
go-cli-sdk-template (`cli-applications.md`): `cmd/infer` derives go-core's signal context and
calls the composition root, and `internal/app` holds `app.go`, `infrastructure.go`, `domain.go`,
and `version.go`, with its graph fixtures in `internal/apptest`. The clients are nodes of
`infrastructure.go`, built from configuration under the tool's environment prefix, `INFER_`,
only when a command declares them; that file is the only one in the composition root that names
a format, as it is the only one that names a provider.

Its module requires go-ai, go-cli-sdk, and go-core, each at a pinned release, and nothing else:
go-ai at `v0.4.0`, the release that adds `aitest`, `Response.OutputText`, and the transcription
mode's text form, go-cli-sdk at `v0.4.0`, and go-core at `v0.12.0`. A further requirement enters
only under `dependencies.md`, "Sourcing", and the README states it.

**Each run talks to one endpoint.** The root flags `--base-url`, `--format`, and `--model`,
accepted at any depth, each override a variable, `INFER_BASE_URL`, `INFER_FORMAT`, and
`INFER_MODEL`, when given. `--format` is `completions`, `messages`, or `responses`, and
`completions` when neither is given; the format package's typed constructor is chosen in
`infrastructure.go` alone, the caller's composition root. The bearer token is
`INFER_TOKEN`'s alone, never a flag's, so it never enters a command line, and an empty one sends
none. `INFER_HEADER`, also never a flag's, holds a credential sent in a header of its own, such
as a gateway's or a cloud endpoint's `api-key`: one or more `Name: value` lines separated by
newlines, each trimmed and a blank one skipped, which all three client nodes send through
`Options.Header`. `infer` refuses a line with no colon or no name. `ai.New` refuses a name the
client sets or a name or value net/http can't send, and `infer` attributes that refusal to
`INFER_HEADER` by putting the headers to `ai.New` alone, as it does each other setting. No
refusal echoes a value. The root help names both variables under `Environment:`, a go-cli-sdk
`Footer`, since no flag section lists them. A missing base URL, an unknown format, or a value
`ai.New` refuses is a usage error, exit 2, naming the flag and its variable, before any request.
An endpoint and model per capability is a long-running program's need, not a one-shot client's:
a run that wants another endpoint is another run.

**A deployment's profile is one JSON setting.** The root flag `--profile`, over `INFER_PROFILE`,
holds a JSON object keyed by format name, `completions`, `messages`, or `responses`, each member
that format's `Options` under its snake_case names. `infrastructure.go` decodes the whole object
with `encoding/json/v2` and `json.RejectUnknownMembers` and builds the format in use with its
member, or with the zero `Options`, the spec, when the member is absent or the profile unset. A
malformed profile is a usage error, exit 2, naming the flag and its variable. Only `chat` and
`transcribe` read it, since every `Options` field concerns a chat request: `models` and `embed`
share a client in the format's spec form.

**The one-model fallback.** With no model set, a command that sends to a model lists the
endpoint's models through `Client.Models` first and sends to the one listed. A purpose base URL
lists one model, such as a gateway's per-purpose `/<alias>/v1` or one vLLM instance, so it needs
no `--model`, and the gateway needs no code of its own in `infer`. An endpoint that lists none,
or several, exits 1, naming each in the endpoint's order, as in `several models: agent, vision;
pass --model`: the command line is sound, and the endpoint's answer leaves the model open. A set
model lists nothing first.

Its four commands that send, bare words at the root, each go through the base module's surface
alone:

- **`models`** writes each model ID `Client.Models` returns, one per line, in the endpoint's
  order.
- **`embed`** sends its inputs, each argument or else each line of stdin, as one
  `EmbeddingRequest` batch through `Client.Embed`, and writes one JSON array of numbers per input,
  one per line, in input order, with the usage on stderr, as in `usage: 22 input tokens, 22 total
  tokens`.
- **`chat`** sends one prompt, its arguments or else stdin, as a `protocol.Request`, with
  `--instructions`, `--image` (repeatable: a file inlined through `protocol.DataURL`, or an
  http(s) URL as given), `--reasoning`, `--max-tokens`, and `--native`, raw JSON merged into the
  request's top level through `Native`, the native tier. It checks the request with
  `protocol.Request.Validate` and the format's own encoding before anything is sent, so a refusal
  is a usage error, exit 2. A format's refusal is a `format.UnsupportedError`, whose `Feature`
  names the flag to change, as in `--max-tokens: responses: request's max output tokens 15 is
  below the minimum, 16`, which only `--format responses` refuses, or `--format messages needs
  --max-tokens: ` when no `--max-tokens` was given. A refusal the profile declares (`Declared`)
  ends with `; declared by --profile (INFER_PROFILE)`. A refusal that carries no feature, such as
  `protocol.Request.Validate`'s, is attributed as `ai.New`'s refusals are: `infer` checks the
  request again once per flag, with that flag's field alone put back to a value every format
  accepts. When exactly one of these requests is accepted, its flag precedes go-ai's reason, as
  in `--reasoning: protocol: request has unknown reasoning effort "extreme"`. When none is
  accepted, as with two flags each refused, the flag is the one whose request is refused for a
  different reason, the errors compared whole. Otherwise, as with two flags refused only
  together, go-ai's reason stands alone. It streams through `Client.GenerateStream` by default,
  writing each `OutputTextDelta` as it arrives and folding the events with
  `protocol.Accumulator`; `--stream=false` sends one request through `Client.Generate`. Either way
  it writes the reply's status, its usage, and its count of reasoning items to stderr, the
  reasoning tokens shown as part of the output, as in `usage: 78 input tokens, 38 output tokens
  (27 reasoning), 116 total tokens`. go-ai decodes an absent reasoning count as zero, so when the
  count is zero and the reply holds a reasoning item, as through `messages`, whose usage has none,
  the line says `(reasoning not reported)`; with neither, it has no parenthesis. A reply that
  isn't completed keeps the text written and exits 1 naming its status and reason, as in
  `incomplete: max_output_tokens`.
- **`transcribe`** sends one audio file through `Client.Transcribe`, its `Open` opening the file
  again for each attempt, and writes the transcript, with the usage on stderr. `--mode`, over
  `INFER_TRANSCRIPTION_MODE`, sets `Options.TranscriptionMode`: `route`, the default, or `chat`,
  for an endpoint whose audio model has no transcription route (the surface section, decision 9).
  Chat mode through a format that can't carry audio is go-ai's refusal, exit 1, before the file
  is opened, after `--mode chat needs a format that carries audio; --format messages doesn't: `.
  `infer` reads that refusal from go-ai's `format.UnsupportedError` of Feature `content part` and
  Value `protocol.InputAudio`; any other chat-mode refusal, such as of a file name with no audio
  extension, is go-ai's error alone. An `*ai.IncompleteTranscriptError` exits 1 naming its status
  and reason. A route-mode 404, an `*ai.APIError` of status 404, exits 1 with the error and, on a
  second line, `(the endpoint serves no transcription route; try --mode chat)`; `infer` neither
  probes chat mode nor falls back to it.

The template's version command stands beside them. Help and a usage error exit 2, any other
failure exits 1, reported once on stderr; help and version read no configuration and send
nothing.

**Retry and the idle bound are flags over variables too.** The root flags `--retries`,
`--retry-base-delay`, and `--retry-max-delay`, over `INFER_RETRIES`, `INFER_RETRY_BASE_DELAY`,
and `INFER_RETRY_MAX_DELAY`, set `ai.RetryPolicy` for every command. Unlike go-ai's zero value,
which retries nothing, infer retries by default: each setting unset or empty is 3
attempts, a 500ms base delay, and a 30s max delay, and each given overrides its default alone;
`--retries 1` sends once. The default base delay is capped at a max delay given below it, so
`--retry-max-delay` alone is a working policy. `chat`'s `--idle-timeout`, over `INFER_IDLE_TIMEOUT`,
sets `Options.StreamIdleTimeout` (the surface section, decision 7). A negative, inverted, or
unparsable value is a usage error, exit 2, naming the flag and its variable; `ai.New` is the
authority on what it refuses. A base delay above zero with the max delay zero, which
`ai.New` refuses as "above its most delay", gets a message that says what to change:
`--retry-base-delay (INFER_RETRY_BASE_DELAY) needs --retry-max-delay (INFER_RETRY_MAX_DELAY) at
least as long`. Each of these four flags is a string flag that its node parses, and its usage
backquotes the type it is parsed as, which `flag.UnquoteUsage`, and so go-cli-sdk's help, shows
in place of `string`: `--retries int`, and `duration` for the delays and the timeout. The
transcription mode and the idle bound each belong to one command, so each reaches a client node
of its own, the transcriber and the chat client, built by the same constructor as the client the
other commands share: no other command reads their variables, and an unknown mode never refuses
a chat. A Retry-After above `--retry-max-delay` ends the retries at once, as `MaxDelay` caps
every wait.

**Two test tiers.** The unit tier runs the App in-process against go-ai's public `aitest`
endpoint and `infer`'s own graph fixtures in `internal/apptest`, never go-ai's internal helpers,
and asserts what each command sends and writes. The integration tier, `tools/infer/integration`
behind the integration build tag, builds `cmd/infer` and runs it one shot at a time on go-core's
`processtest`, mapping each `AI_<ROLE>_BASE_URL` the base module's tier reads onto
`INFER_BASE_URL` after clearing every `INFER_` variable. It maps a role's optional
`AI_<ROLE>_MODEL` onto `INFER_MODEL`; with none, it sets no model, so the role's endpoint must
list one. A scenario whose variable is unset skips. Both tiers read a role's optional
credentials, `AI_<ROLE>_TOKEN` and `AI_<ROLE>_HEADER`. The base module's tier puts them in
`Options.Token` and `Options.Header`; `infer`'s maps them onto `INFER_TOKEN` and
`INFER_HEADER` in the test's environment rather than in a run's logged overrides, so no failing
scenario prints a value. Both tiers also read a role's optional `AI_<ROLE>_PROFILE`, in
`--profile`'s shape and decoded as strictly. The base module's tier builds each format with the
role's member; `infer`'s maps the profile onto `INFER_PROFILE`. Unset, every format is its spec.
A scenario whose request the profile refuses before sending, a declared gap, skips and names the
gap; a refusal of the spec's own fails. `mise run integration` runs it after the base module's
tier; no CI job runs either.

### The lint configuration

`.golangci.yml` carries out the modules section's decisions 3 and 4. Its linters are the standard
set, plus depguard, testpackage, and nolintlint. Tests are linted with the code they test, so the
depguard rules hold them too, and the check runs golangci-lint a second time under the
`integration` build tag, so each integration tier is linted as well.

- **testpackage** keeps tests black-box. Its skip regexp is narrowed to `export_test.go`, the one
  white-box file allowed, whose one hook replaces the backoff's jitter: a probe the API can't
  reach deterministically, which `tests-and-docs.md`, "The unit tier: black-box and hermetic",
  allows. Its `allow-packages` names only the empty package name, which no package
  has, in place of the default, `main`, so a command's tests are black-box too, in `main_test`. An
  empty list would leave the default in force.
- **depguard's allow entries match by prefix** unless they end in `$`, so go-ai's entries are
  anchored: the module as itself, or a package under it, never a sibling such as `go-ai-extra`.
  The infer rule's allow entry for go-ai would admit its internal packages too, so it denies
  `go-ai/internal` by name; depguard takes the longer of an allow and a deny match, and the deny
  is the longer.
- **nolintlint** requires every suppression to name its linter and give its reason
  (`architecture/principles/comments.md`).

## Implementation notes

This section holds the reasoning that the names and exported docs of the base module, its
integration tier, and infer leave unsaid about their unexported code and tests. It lives here
rather than in comments (`architecture/principles/comments.md`).

### The transport

- **New fixes what every request would send.** It encodes the query once, so a caller changing
  its `url.Values` after New can't change the requests. It copies the fixed headers under
  canonical names: names that differ only in case are one header, their values in the order of
  the names' spellings, so the copy doesn't depend on the map's order. It refuses a name that
  isn't an RFC 9110 token and a value with a control character other than the horizontal tab,
  which net/http won't send, and the headers the Client sets itself, Content-Type, Accept, and
  Authorization.
- **A route's URL** is the base path and the route joined by exactly one slash, with the query.
- **A body is made per attempt.** Its function is called once per attempt, after the token
  source, and may add headers, such as a format's version header. It returns the reader and an
  end function the attempt calls once it is done with the body: on its failure, or as its 2xx
  reply's body is closed. End reports the body's own failure, such as a clip whose read failed
  part way, which is the attempt's cause whatever the transport made of it. A 2xx reply's body
  closes the request's body with it, and its Close returns the request body's failure alone: the
  reply's own close failure, after its read, reports nothing the caller acts on.
- **An attempt is built, then sent.** Building calls the token source first, before the body is
  made, so a source error opens nothing and sends nothing. The header is the fixed headers, then
  the body's, replacing any of the same name, then the bearer token, each value slice cloned so a
  transport that changes the request's header changes neither. A body of unknown length, such as
  a pipe, goes chunked. Building is apart from sending because its errors are every way an
  attempt fails before anything is sent: they return at once, never classified for a retry, so a
  token source that fails on its own network call isn't mistaken for the endpoint's connection
  failing. Sending reads a non-2xx reply's body whole, a failed read keeping the part read as
  still the best account of the error there is, decodes it with the format, and reads its
  Retry-After as the reply arrives, reporting whether it could be read: a Retry-After of 0 waits
  none, where one that can't be read leaves the wait to the backoff.
- **There is one retry loop**, which Generate, GenerateStream (whose stream starts only once the
  loop returns), Embed, Transcribe, and Models all send through. An attempt that fails before
  anything is sent returns at once whatever its error wraps, as does one whose request body
  failed on its own, such as a clip's read, which a later attempt doesn't mend. An attempt is made
  again after a 408, 429, 502, 503, or 504 reply, or a connection failure before any reply while
  the caller's context is live; a transport error once the context is done is the context's
  doing. A connection failure is a network operation's error, such as a refused dial or a reset,
  or the connection closed before the reply's header, which the transport reports as `io.EOF` or
  `io.ErrUnexpectedEOF`. A host name that doesn't resolve isn't one, unless the resolver calls its
  failure temporary. Nor is anything else, such as the transport's ResponseHeaderTimeout,
  net/http's error for a server closing an idle connection, a TLS certificate the transport
  refuses, or an error of the caller's own RoundTripper: the list is short so that only what a
  later attempt can mend is retried.
- **The backoff** caps the wait before attempt n+1, after n attempts, at BaseDelay doubled n-1
  times, at most MaxDelay; the doubling stops at MaxDelay, so it can't overflow. The wait is full
  jitter, a random duration from 0 up to the cap, the cap included; `export_test.go` replaces the
  source so a test can time it. A Retry-After is whole seconds, or an HTTP date whose wait is the
  time until it, none for a date passed; seconds too many to hold are the longest wait there is,
  which exceeds any policy's MaxDelay.
- **An APIError's text** bounds how much of a body without a message it prints, not what is read:
  Body holds the whole reply.

### Streaming and the idle bound

- **A stream's request runs under a context of its own**, below the caller's, so the idle bound
  can end the request, which unblocks a read waiting on it, without ending the caller's context.
  The caller's context alone is checked for its cancel, since the request's being done may be the
  bound's doing; once it is done, nothing more is yielded and its error ends the stream.
- **A failed read's error.** Once the caller's context is done the transport has closed the
  connection, so the read failed because of the context, and the error is the context's.
  Otherwise the stream ended before its terminal event: the error matches `io.ErrUnexpectedEOF`,
  which the transport returns for a connection dropped mid-body and the idle bound for a stalled
  one, and also the read's own error, such as a connection reset.
- **The idle bound times each read.** Its timer, `time.AfterFunc`'s so the bound starts no
  goroutine of its own, runs only while a read waits, so the time the caller spends between events
  isn't counted, and any bytes a read returns, a comment's included, end that read and so reset
  the bound. When it fires, it cancels the stream's request, which unblocks the transport's read.
  A read it cut off, or any read after, reports the stall whatever the transport returned: the
  transport's error is the cancellation's, which the caller must not mistake for its own
  context's. A read that reached the body's end as the timer fired is the end, since the stream
  was whole. The stall matches `io.ErrUnexpectedEOF`, as the stream ended before its terminal
  event, and `os.ErrDeadlineExceeded`, as a read past a `net.Conn`'s deadline does. Closing the
  body stops the timer and cancels the request first, so a transport whose body resumes after a
  failed read sees the request is done.

### Embeddings, models, and transcription

- **Embeddings travel as base64**, standard base64 of little-endian float32s: about a quarter of
  the bytes of the float array, and exact. Each vector is placed by its item's index, and a
  decoded vector is never nil, even when empty, so a placed index can be told from an open one.
- **A model list entry is kept whole**, as the endpoint sent it, to become the Model's Native
  beside the id decoded from it: its other members, such as `owned_by` and llama.cpp's router's
  `status`, are the endpoint's. Decoding into a `jsontext.Value` copies the entry's bytes, so a
  Model doesn't alias the reply buffer.
- **The transcription route's form** has one boundary per call, so every attempt sends the same
  form. Its fields go ahead of the file in order, the typed fields then Native's in sorted key
  order, so the form is the same whatever the map's order; the file part's name, `file`, is
  always set, so a Native key of that name clashes too. The file part's Content-Type is
  `application/octet-stream`: a type guessed from the file name would depend on the host's MIME
  tables, and llama.cpp reads the clip's format from its bytes. llama.cpp's reply also carries
  type `transcript.text.done`, a usage type of `tokens`, and
  `usage.input_tokens_details.cached_tokens`, which the client doesn't keep.
- **Each attempt writes the form into a pipe** from a goroutine of its own, whose error, or EOF,
  ends the request body's read. When the attempt ends, closing the pipe's read side fails a write
  still blocked on it, so the goroutine ends; after a whole read, that is a no-op. The clip is
  read, so a failure to close it changes nothing sent. The clip's reader keeps its own read error,
  other than `io.EOF`: an upload that failed reading the clip has that as its cause, whatever the
  transport made of the body's failed read. Any other failure of the goroutine's is a write into
  the pipe the request closed, which the attempt's error accounts for.
- **Chat mode probes the format first.** It asks the format to encode a request of the same
  shape, its clip a placeholder, before the clip is opened or the token source called, so a
  format that refuses audio refuses it there, and what it refuses doesn't depend on the clip's
  bytes. A reply the model didn't finish holds part of a transcript at most, which reads as a
  whole one, so it is refused, after the retry loop rather than inside it: sending the same clip
  again meets the same bound or filter.

### The aitest package

- **An Endpoint ends with its test.** Its context ends when the test ends, and with it every
  request's context; its cleanup ends that context before closing the server, which waits on the
  handlers using it.
- **A transcription upload** is parsed from the recorded copy of its body, read whole to record
  it, with up to 32 MiB of the form in memory and the rest in temporary files the request's end
  removes. Its file name is read from the part's Content-Disposition, since the parsed form's
  file name keeps a path's base name alone, which would hide a client that sends the path it read
  the clip from.
- **What a request asks of its reply's shape** is decoded on the Chat Completions route alone and
  zero elsewhere, so a ChatReply another route returns is unstreamed. A chat reply's id is made
  from the request's arrival number, and its model is the request's. Everything a stream wrote is
  flushed before it is held, so the client has read it.
- **A Gate** keeps each parked request's channel in the order they parked, closing one to let its
  request go; a count of the releases no parked request has taken yet, each letting a later
  request through at once; and a channel closed and replaced when a request parks, which wakes
  Await. Await's bound fails a test whose requests never arrive, naming the count, rather than
  leaving it to hang until the test binary's timeout.

### The completions format

- **Item ids derive from the reply's id**, as `<reply id>/<output index>`, such as
  `chatcmpl-abc/1`, alike for every kind of item and alike streamed and unstreamed: every chunk
  carries the reply's id, so a stream's items get the ids DecodeResponse gives the same reply. An
  id is unique within the reply, as its output index is, and across replies, as the reply's id is.
  A reply with no id gives its items none: an id made of the index alone would repeat across
  replies, where an empty id claims no uniqueness.
- **Reasoning is read from reasoning_content first.** vLLM sends `reasoning` in its place; a
  server that sends both sends one text under the two names, so reasoning_content alone is read,
  never the two joined.
- **The usage's reasoning tokens** are pointers on the wire, nil when the member is absent or
  null, so a count of 0 under completion_tokens_details still counts as given and wins over the
  top-level reasoning_tokens some servers send. A count is taken as given, never clamped, though
  a server's may exceed completion_tokens, as Foundry's gpt-oss-120b's does.
- **The request's members.** max_completion_tokens carries MaxOutputTokens: llama.cpp reads it
  and max_tokens alike, as aliases of n_predict, and it is the name OpenAI keeps, having
  deprecated max_tokens. stream is always sent, so a Native stream member clashes in both
  encodings; stream_options is sent with a streamed request alone, as OpenAI refuses it
  otherwise, and a Native stream_options is refused in both, as it is the stream's.
  parallel_tool_calls is omitted when the request leaves it to the endpoint, so a Native member
  of that name clashes only when it is set.
- **The envelope** is what the format writes into a Reasoning item's encrypted_content and reads
  back: the prefix `go-ai:completions/v1:`, naming the implementor, the format, and the version,
  then `{"reasoning_content"}` as JSON in standard, padded base64. A change to the layout or the
  members' meaning is a new version, and a new prefix; encrypted_content without this prefix was
  written by another format, or another version, and isn't this format's to read. The tests pin
  the envelopes as literals, so a layout change, which would orphan the envelopes callers hold,
  fails there.
- **A chunk that isn't JSON** is reported with its JSON error formatted, not wrapped: a chunk cut
  short wraps `io.ErrUnexpectedEOF`, which in a stream means the stream ended before its terminal
  event, and the chunk arrived whole.
- **An error frame's param** is read by the format itself, as DecodeOpenAIError reads none; an
  error of another shape, or a param that isn't a string, gives none.
- **The stream decoder** records whether any event arrived, so End opens a stream of nothing but
  chunks of no id and no choices before reporting it incomplete, and makes nothing for an empty
  one. A call's output index follows from its tool-call index, the first call's index fixing the
  base, so the output is the tool_calls array's order whatever order the calls begin in; calls
  whose indexes skip one are refused at the finish_reason, before any closes.
- **The recorded streams' cached prompt tokens** are pinned from the stream: llama.cpp's cache
  held another share of the prompt for the stream than for the unstreamed send. Foundry's two
  replies to one request differ even at temperature 0 and seed 7, so their reasoning and token
  counts are pinned from the stream too.

### The messages format

- **Item ids derive from the reply's id**, as the completions format's do: `<reply id>/<output
  index>`, such as `msg_abc/1`, and none for a reply with no id.
- **Refusals come first in a fixed order.** The effort is checked first, so a request of an
  effort Messages lacks, or one the profile declares refused, is refused for that effort whatever
  else it holds; the input is encoded next, so a request whose content Messages has no form for
  is refused for that content whatever else it lacks; max_tokens comes after. The
  anthropic-version header is set only once the body is encoded, so a refused request adds none.
  A Native tool_choice beside ParallelToolCalls is refused as the clash Marshal would report were
  the request to send the member ParallelToolCalls sets.
- **A refusal part goes back as a text block**, as Messages has no refusal block to send back.
- **An image data URL's media type** is the type alone: its parameters, such as a charset, precede
  `;base64` and are dropped. A data URL of no data is malformed, an error but not a refusal.
- **The envelope** is the prefix `go-ai:messages/v1:`, then base64 of one of two shapes,
  `{"thinking","signature"}` for a thinking block, both members always present, or `{"data"}` for
  a redacted_thinking block. Its members are pointers so the shape shows in which are present: a
  thinking block's signature is present even when empty, as llama.cpp's always is. A
  well-formed envelope is of exactly one shape. A change to the layout or the members' meaning is
  a new version, and a new prefix.
- **The usage's counts** are pointers on the wire, nil when the reply omits them or gives null, so
  a message_delta, whose counts are cumulative, keeps the counts it omits at what they were.
  message_start's output counts are placeholders, such as Anthropic's output_tokens of 1, so they
  are cleared there and message_delta gives the reply's.
- **An error frame** is recognized from its data: type `error`, Anthropic's, whose error member
  holds the error; or a body of a type no Messages event has and a string message, as llama.cpp's
  is, which holds the error object at its top level and is read as an OpenAI-style body wrapping
  it. message_start's message is an object, so it is never taken for one.
- **The stream decoder** keeps each item's blocks, as a message is one block per part, and closes
  an item once its blocks have stopped, it has ended, and its status is known: completed when an
  item follows it, the stop_reason's once message_stop has made it final, as a later
  message_delta may give another stop_reason. A tool_use block of no input_json_delta has the
  input its start gave, `{}` from Anthropic or none from an endpoint that omits it, which becomes
  `{}`, the arguments DecodeResponse gives the same call. An event that isn't JSON is reported with
  its JSON error formatted, not wrapped, as the completions format's chunk is.
- **The recorded replies** were each sent twice, streamed and not, and the endpoint makes new ids
  for each, vLLM a new signature, and the router's prompt cache another split of the same input,
  so the stream tests take those from the stream.

### The responses format

- **The request's members.** include, `["reasoning.encrypted_content"]`, store false, and stream
  are always sent, so a Native member of any of those names clashes; parallel_tool_calls is
  omitted when the request leaves it to the endpoint, so a Native member of that name clashes only
  when it is set. The input is always an array of items, never the string form. A tool's strict
  flag is always sent, so false stays false at llama.cpp's router, whose default is true. The audio
  extension's part type carries the implementor slug, `go-ai:input_audio`, under the spec's
  extension rule.
- **The envelope** is the prefix `go-ai:responses/v1:`, then base64 of
  `{"encrypted_content","content"}`, both members always present. Its members are pointers so an
  envelope missing one reads as malformed. A change to the layout or the members' meaning is a
  new version, and a new prefix.
- **The stream decoder decodes a terminal event's response** as DecodeResponse decodes a reply,
  through the same function without the package's prefix on its error.
- **A skipped item holds its output index**, and the items after it are placed as if it weren't
  there, so the events' output indexes leave no gap. Once an item after it is added, its place
  can't be taken out, and a skipped item added there is an error. A skipped part holds its content
  index in the same way.
- **An event's output_index and content_index** are pointers, nil when the event gives none, as
  the router's give none; such an event is placed by arrival and by its item_id.
- **A reasoning item is added with neither encrypted_content nor summary**, though the spec lets
  an in-progress item carry each, the encrypted_content possibly incomplete; its output_item.done
  gives the whole of each, its encrypted_content going into the envelope. Foundry adds
  gpt-5-mini's reasoning item with an encrypted_content of its own, unlike its output_item.done's,
  which is unlike the terminal response's again, so each envelope holds the encrypted_content of
  the item it was made from.
- **An error frame's param** that isn't a string, such as null, leaves the payload's param empty;
  the rest is still the error. An event that isn't JSON is reported with its JSON error
  formatted, not wrapped, as the other formats' are.

### The integration tier

- **What a scenario proves is what the exported API shows.** That a reasoning round trip put the
  reasoning on the wire, as the assistant message's reasoning, a thinking block, or a reasoning
  item's content, is the unit tier's to prove: the exported API doesn't show the body. Each
  scenario runs as one piece of test code per format, the format the only difference, and a
  scenario an endpoint honors in some formats only runs through those alone.
- **Every request carries a bound.** Messages requires max_tokens, and a reasoning model reasons
  ahead of its answer or its call within the same bound, so a request to the agent, and the
  vision stream's, leaves room for both. One request is bounded in time too: the endpoints have
  their models loaded, so the bound covers generation alone, tens of seconds on a reasoning model.
- **The model list is a check of its own.** TestModels wants each role's endpoint to list exactly
  one model, so a misnamed or crowded endpoint fails there by name before any scenario of it runs;
  a role whose `AI_<ROLE>_MODEL` names its model wants the list to name at least one.
  A route the formats don't shape, such as embeddings', is sent in Chat Completions as the
  client's format.
- **A declared gap skips from inside the client.** A format refuses a request as it encodes it,
  before anything is sent, on the goroutine that called Generate, GenerateStream's iterator, or
  Transcribe, which is the test's own, so skipDeclared may skip the test there. Every scenario's
  format is wrapped in one, so the skip is the tier's one rule.
- **No failure prints a secret.** A header line that doesn't parse is reported by its number,
  never its text, and a refused client by its variable and base URL; the model is read apart from
  the credentials, so a scenario that prints it prints neither.
- **The allowed_tools choice runs on Messages alone**, whose format sends it as the one tool it
  allows under tool_choice any. Chat Completions and Open Responses send their own forms, as the
  unit tier proves, and vLLM 0.33.0 honors neither: it refuses Chat Completions' with a 400 and
  answers Open Responses' as auto, with no call.
- **The embed scenario reads the vectors' decoding from their geometry.** It wants two alike
  inputs closer to each other than either is to an unrelated one, which fails vectors decoded
  with the wrong byte order or width, whose components are noise. It wants the third input,
  embedded alone, to match the batch's third vector and not its first, which fails vectors placed
  in arrival order or swapped. Their lengths are not
  asserted: vLLM 0.33.0 gives granite-embedding-english-r2's vectors unnormalized, where llama.cpp
  normalizes them.
- **Media answers are read loosely.** Each shape of `shapes.png` must be named as a word of its
  own, singular or plural, in any phrasing. A transcript of `phrase.wav` is compared with
  everything but letters and digits stripped, so the access code passes as digits or words,
  spaced or not, after "accesscode". The transcription route scenario skips unless
  `AI_TRANSCRIPTION_BASE_URL` is set, as an audio endpoint may serve no route.
- **A cancelled stream** yields no event after the cancel, the context's error as its one error,
  and leaves the accumulated Response in progress, its message holding the text so far.

### infer

- **cmd/infer's run is apart from main** so its deferred stop releases the signal registration
  before os.Exit, which skips deferred calls. The version is the build info's main module version,
  `(devel)` when the binary carries none, as under go test.
- **Nothing is read until a Build reaches it.** defineInfrastructure constructs nothing: each
  setting is read in its node's constructor, so configuration is read only for a command that
  declares a node, never in New. The dispatcher adds the lifecycle configuration node to every
  Build. A setting is its default, replaced by its variable when that is set and non-empty, and
  in turn by its flag when the dispatch set the flag, even to an empty value: the root's PreRun,
  and each command flag's Validate, record which flags the dispatch set, as a flag variable can't
  tell a flag given its default value, which still overrides the environment. A command's own
  flag over a variable is that command's alone, so no other command accepts it or reads its
  variable; `--mode`'s variable is named for the setting, `INFER_TRANSCRIPTION_MODE`, so it reads
  as transcription's beside the other `INFER_` variables.
- **The profile is decoded whole**, every member checked, not only the format in use's.
- **The client sets no timeout**: a long stream is bounded by the run's context and the idle
  timeout alone. The token and the headers are read once, as the client is built, and the client
  makes no request. A header line that doesn't parse is reported by its number, never its text,
  as the value is a secret, as the token is, which is why neither has a flag. The root help's
  footer naming their variables renders in memory, so its write can't fail.
- **ai.New's refusals are attributed by probing.** go-ai's errors carry no field, so only when
  New refuses does infer put each setting to it again alone, beside values it never refuses: the
  base URL with no options, the headers alone, the attempt count alone, the max delay alone, and
  the base delay under the longest max delay. A base delay above the max delay is refused only
  with both, so when New refuses the policy but no single field, the message names both delays;
  with the max delay zero, the message says the base delay needs a max delay at least as long,
  which says what to change. A max delay left unset is the default, never zero.
- **infer retries by default** because it is run by hand and by scripts against endpoints that
  shed load, where a transient 503 shouldn't end the run; go-ai's zero policy, which retries
  nothing, suits a library. The default base delay is capped at a max delay given below it, but
  a negative max delay is left for ai.New to refuse, naming it alone. The default max delay isn't
  raised to meet a base delay given above it, so ai.New refuses that base delay, naming both.
  go-ai sets no bound for a negative StreamIdleTimeout rather than refusing one, so infer refuses
  it: a negative bound is a mistake, not a request for none. infer refuses an unknown
  transcription mode in its own words rather than go-ai's.
- **chat checks its request before it resolves the model**, so a refusal is found before the
  models are listed; an unresolved request is checked under a stand-in name, as the checks refuse
  only a request with none. A refusal with no feature is attributed by clearing one flag's field
  at a time rather than setting it on a request of none: Messages refuses a request with no max
  output tokens, so no format accepts a request with every field cleared. A max-tokens refusal with
  no `--max-tokens` given is Messages'. `--instructions` and `--image` are never attributed: go-ai
  refuses neither as chat sets them. An image's media type is its extension's, or else its
  content's sniffed type, without parameters such as a charset, which a data URL doesn't take.
- **chat's text reaches the terminal as it arrives.** Its writer flushes after each delta when
  stdout buffers, and remembers the last byte, so the text ends with exactly one newline and an
  empty text is an empty line.
- **embed reads stdin line for line.** A final newline ends the last line rather than starting an
  empty one, and a blank line is an empty input, which keeps the vectors line for line with the
  input. A line may be as long as a model's context, which the scanner's default 64 KiB would
  refuse, so a line may run to 64 MiB. models writes an entry's root only when it is a string: a
  missing, null, or other root, or an entry that doesn't decode, is no root rather than a failure
  on a member only some endpoints send.
- **transcribe leaves the file to go-ai**, which opens it once per attempt, so a retry sends the
  whole clip; infer neither reads nor checks it. go-ai is the authority on a format that can't
  carry audio: infer reads its typed refusal only to word it, exit 1 rather than a usage error, as
  it is the format and the mode together that can't transcribe. A chat-mode reply that isn't
  completed writes no transcript, as it holds part of one at most. A route-mode 404's hint is a
  line of its own, in parentheses, so the error is reported once, whole on its first line;
  infer neither probes chat mode nor falls back to it, as a 404 can mean a wrong base URL too.
- **The unit tier's graph test** takes the leaves from the App's own tree, not a copied list, so a
  command mounted later is checked too, and proves its layer guard fails a fixture leaf that
  declares a node ordered after one its path doesn't. New is shown to build nothing by a Replace
  succeeding after it, as the graph freezes on its first Build. The live-stream test writes
  through a buffering writer, as a terminal's may be, so a delta left unflushed never arrives
  while the stream is held.
- **The integration tier** builds the program once per run in TestMain; without the build tag the
  harness only type-checks and needs no binary, so the unit tier runs its untagged tests of the
  harness's mapping of a role's variables and of SkipDeclared. Every scenario on an endpoint runs
  through runOn, so each gets the declared-gap skip. It reads go-ai's own integration testdata in
  place, so the repository holds one copy of the media and one account of where it came from; the
  tier runs from a checkout of go-ai, where that directory is always beside `tools/infer`.
  TestChatReasoningEffort runs on the vision endpoint, not the agent's: gpt-oss-120b speaks
  Harmony, which always reasons, and vLLM 0.33.0 refuses its effort none with a 400 through Chat
  Completions and Open Responses and reasons anyway under Messages' thinking disabled, where
  Gemma 4 honors the effort in all three. The reasoning at low gives none's zero its meaning, so
  it is wanted only where none ran: Foundry's gpt-5-mini reasons at low through Open Responses but
  reports none through Chat Completions, whose spec has no reasoning output.

### Scripts

- **currency captures each scan's output before reading it.** Each scan's command runs into a
  variable first, so under `set -e` a scan command that fails, such as a `go list` missing a
  `go.sum` entry, fails currency instead of reporting nothing.

## Rejected alternatives

What the design rules out, one line each with its reason, citing the section that settles it
where one does: "decision" alone is the surface section's, and "modules" the modules section's.

- **A vendor SDK, openai-go or anthropic-sdk-go, as the client.** None covers all three wire
  formats; openai-go failed minimal-footprint (spike-harness-driver's `findings.md`); and a vendor
  SDK enters a base module only through a sub-module (`dependencies.md`, the "No provider in a
  base" item of its opening list, and "The line per tier").
- **A provider sub-module per format.** A format pins no vendor library, so each is a package of
  the base module, split by growth (modules, decision 1).
- **`infer` in the base module.** Its requirements would enter the library's module (modules,
  decision 1).
- **A format registry, or a format chosen by name.** A provider is selected by construction, and a
  new format is one import at the caller's composition root.
- **One capability struct shared by the formats and read by the root package.** Each format's gaps
  differ, so each format holds its own `Options`.
- **A profile loader, host presets, or a profile keyed by model.** Where a program keeps its
  profiles, and which deployments it meets, are the program's; a profile binds one deployment, so
  a program builds one client per deployment.
- **`client` as the root package's name.** Under go-ai's path every importer would alias it; `ai`
  reads with the repository (decision 1).
- **The spike's slug on the wire.** Open Responses prefixes an extension with its implementor's
  slug, which is go-ai's; an envelope the spike wrote is another format's and is dropped on
  encode.
- **`New(cfg Config)`, one struct of required and optional inputs.** The required inputs are
  positional, then an options struct whose zero value is the default (decision 2).
- **The struct-with-a-method form of construction.** A client is a value the caller keeps, not a
  configured thing run once (decision 2).
- **`Config` as the options struct's name.** In Go Elemental a `Config` is a go-core configuration
  block, which go-ai takes none of (decision 2).
- **A pointer retry policy, with a set policy of zero attempts refused.** The options' zero value
  selects every default, so the zero policy retries nothing.
- **An error for a nil HTTP client or format.** A wiring defect panics with the fix named.
- **A default HTTP client, timeout, retry, size limit, or concurrency bound.** A library ships no
  policy the application must own.
- **A go-core configuration block or lifecycle part.** A client holds no resource to start, stop,
  or report ready, and the base module stays on the standard library.
- **A logger, meter, or tracer in the client.** Instrumentation is a `RoundTripper` on the
  caller's `*http.Client`.
- **A caller's `RoundTripper` as the only way to send `x-api-key` or a consumer-key header.**
  `Options.Header` serves both without one (decision 3).
- **Retrying a header timeout or net/http's idle-connection error.** The endpoint may still be
  running the request, and the error can be matched only by its text.
- **An idempotency key on a retried POST.** It would be one endpoint's native header, not the
  standard's; a caller who sets a policy accepts a run twice.
- **An Open Responses SSE encoder of the stream events.** A service relays its own event shapes,
  and an encoder would be a type before its need (decision 4).
- **No idle bound, or one left to the caller's own timer.** A caller's timer sees only events, so
  it can't tell a stalled endpoint from one still pinging (decision 7).
- **An idle bound on by default, as go-storage's `ReadIdleTimeout` is.** go-ai sets no policy, so
  the zero `StreamIdleTimeout` sets none (decision 7).
- **`sse.Decoder.Retry`.** The client never reconnects, so nothing reads it.
- **SSE hard-coded in the client, or a framing chosen by the reply's Content-Type.** The framing
  is the format's to declare, and a reply's Content-Type doesn't bind the client; the client
  reads only through the format's `EventStream`.
- **A typed field for a native engine parameter.** The standard tier is the external standard and
  no wider; a native parameter stays raw JSON in `Native`.
- **A protocol trimmed to what its first callers use.** The client's scope is chat with tools and
  reasoning, each proven against the endpoints the testdata is recorded from.
- **One `MaxOutputTokens` minimum for every format.** 16 is Open Responses' minimum alone, and
  holding every format to it refused caps Chat Completions and Messages allow.
- **Reasoning efforts limited to Open Responses'.** No model could then be sent Chat Completions'
  minimal or Messages' max; an effort joins when an implemented format's spec defines it, and a
  format refuses one its spec lacks.
- **Refusals as untyped errors.** A caller could tell a format's refusal from a failure only by
  its text; `format.UnsupportedError` names the format and the feature.
- **A router build's account in a format's package comment.** The package comment describes the
  API; the account moves to the testdata READMEs.
- **One set of usage names across embeddings, transcription, and generation.** Each usage type
  carries its endpoint's names, and a caller maps them in one place.
- **A one-shot transcription clip, never retried.** The clip reopens per attempt, so `Transcribe`
  joins the one retry loop.
- **Transcription through the route alone.** An endpoint whose audio model has no transcription
  route, as vLLM serving Gemma 4 has none, transcribes only over chat (decision 9).
- **Probing the transcription route and falling back to chat.** It costs a request per call and
  hides a misconfigured endpoint; the mode is the caller's, set once (decision 9).
- **Chat Completions pinned in the root package for chat-mode transcription.** The transport would
  name a format; chat mode sends through the Client's own (decision 9).
- **`RetryAfter` as a time of day.** A wait from the reply's arrival needs no clock or zone, and is
  what a redelivery's delay takes (decision 5).
- **Refusing `Embed`, `Transcribe`, and `Models` on a format without the OpenAI-compatible
  routes.** The router serves all three beside `/messages`, and an endpoint without the route
  answers with an `*APIError` (decision 9).
- **A default router address for the integration tier, or a CI job that runs it.** The tier skips
  unless a caller names an endpoint, and the models are on the organization's own machines.
- **One endpoint variable for the integration tier, or a model name fixed in it.** vLLM serves one
  model per endpoint, so each role has its variable, and each scenario sends to the one model its
  endpoint lists, or to the one its role's optional `AI_<ROLE>_MODEL` names.
- **An integration check that logs the outcome and passes either way.** A test proves a behavior
  only if it can fail.
- **A README that names the repositories built on go-ai.** A repository never names its
  dependents.
- **A `go` directive naming a patch release.** The directive names the current minor, and
  `mise.toml` pins the toolchain's patch (modules, decision 2).
- **depguard alone as the base module's guard, or the line as a judgement call.** An edit to the
  allow list widens depguard quietly; the no-`require` assertion states the line, and a mechanical
  line is the check's (modules, decision 3).
- **A committed `replace` of the root in `tools/infer`.** `infer` requires a released go-ai; a
  `replace` is a transient bridge, removed once the root is tagged (modules, decision 5).
- **Continuing the spike's tag line, or carrying its changelog entries.** The spike's tags name
  another module path, so go-ai's start at `v0.1.0` (modules, decision 6).
- **A first release at 1.0.** The surface is open to change until its callers build on it
  (modules, decision 6).
- **One changelog and a `v*` trigger alone.** go-ai has two artifacts, each with its own changelog
  and tag namespace (modules, decision 7).

