// Package app is infer's composition root. [New] describes the program's
// dependencies as one go-core graph and builds the command tree on
// go-cli-sdk over it; [App.Run] runs that tree, as a go-cli-sdk
// cli.Program, on the process arguments. Each architecture layer has one
// file, which defines the layer's nodes in its define function and owns
// its mount, so the layer files are the architecture's layer list:
//
//   - app.go: [App], [New], and [Nodes], which call the layer files'
//     define and mount functions in layer order, lowest first;
//   - infrastructure.go: defineInfrastructure, the root flags and the
//     configuration, read from INFER_ variables, the lifecycle
//     configuration, the retry policy, and the go-ai client of the one
//     endpoint a run talks to; the commands' own flags over a variable,
//     transcribe's --mode and chat's --idle-timeout; and the transcriber
//     and the chat client, clients of the same endpoint in the
//     transcription mode and bounded by the idle timeout; it is the one
//     file that names a format package;
//   - domain.go: defineDomain and mountDomain, infer's commands over the
//     client, models, embed, chat, and transcribe, and resolveModel, the
//     one-model fallback each command that sends to a model resolves its
//     model with;
//   - version.go: the version command, which declares no nodes.
//
// Each run talks to one endpoint. The root flags --base-url, --format, and
// --model, accepted at any depth, each override a variable, INFER_BASE_URL,
// INFER_FORMAT, and INFER_MODEL, when given; --format is completions,
// messages, or responses, and completions when neither is given. The
// bearer token is INFER_TOKEN's, never a flag's, and an empty one sends
// none. The root flags --retries, --retry-base-delay, and
// --retry-max-delay, over INFER_RETRIES, INFER_RETRY_BASE_DELAY, and
// INFER_RETRY_MAX_DELAY, set go-ai's retry policy for every command:
// --retries is the most attempts of a request, the first included, and
// unset retries nothing. A missing base URL, an unknown format, a retry
// setting that doesn't parse, or a base URL or retry policy go-ai's New
// refuses is a usage error, exit 2, naming the flag and its variable,
// before any request.
//
// With no model set, a command that sends to a model lists the endpoint's
// models first and sends to the one listed; none, or several, exits 1,
// several named in the endpoint's order, as in "several models: agent,
// vision; pass --model". A set model lists nothing first. models writes
// each model ID the endpoint lists, one per line, in the endpoint's order,
// and needs no model. embed sends its
// inputs, each argument or else each line of stdin, as one batch, and
// writes one JSON array of numbers per input, one per line, in input order,
// and the token usage to stderr.
//
// chat sends one prompt, the arguments joined by spaces or else stdin,
// with --instructions, --image (repeatable: a file inlined as a data URL,
// or an http(s) URL as given), --reasoning, --max-tokens, and --native, a
// JSON object merged into the request's top level. A request go-ai's
// Validate or the format's encoding refuses is a usage error, exit 2,
// before any request. It streams by default, writing each text delta as
// it arrives; --stream=false sends one unstreamed request. Either way it
// ends the text with a newline and writes the reply's status, token usage,
// and reasoning item count to stderr; a reply that isn't completed exits 1
// naming its status and reason, as in "incomplete: max_output_tokens".
// chat's --idle-timeout, set from INFER_IDLE_TIMEOUT, ends a stream that
// sends nothing for that long, exit 1; it reaches only the chat client, so
// no other command reads it, and a negative or unparsable one is a usage
// error naming both.
//
// transcribe sends one audio file, its one argument, which go-ai opens
// again for each attempt, and writes the transcript, ending it with a
// newline, and the token usage to stderr. --mode, set from
// INFER_TRANSCRIPTION_MODE, is route, the endpoint's transcription route,
// when neither is given, or chat, a chat request in the format; an unknown
// mode is a usage error naming both. The mode reaches only the
// transcriber, so no other command reads it. Chat mode through a format
// that can't carry audio, such as messages, is go-ai's refusal, exit 1,
// before the file is opened; an incomplete chat-mode reply exits 1 naming
// its status and reason.
//
// [Nodes] holds one handle per node: each define function fills its own
// part of one Nodes value, and its constructors read the lower layers'
// nodes from it with Use. Each field's node is named for the field, so no
// comment names it; graph_test.go checks that rule with go-core's
// graphtest.CheckNames, and checks that every leaf of the tree, as
// Command.Leaves yields it, builds its nodes without dropping an order
// between layers. Defining the nodes constructs nothing. A command
// declares the nodes it needs with Use; the dispatcher builds them, with
// the lifecycle configuration node, only when the command runs. A run that
// builds nothing, such as help, a usage error, or version, reads no
// configuration and sends nothing.
//
// The App publishes its composition: [App.Graph], [App.Nodes], and
// [App.Root] return the graph, a handle on each of its nodes, and the
// command tree. The program itself never calls them; they are for a caller
// that observes what a run builds, Replaces a node's constructor with a
// substitute, or adds a command over the nodes, before the App runs.
//
// The package exports:
//
//   - [App], the program, and [New], which describes it cold
//   - [App.Run], which dispatches the program arguments and returns the
//     exit code
//   - [Nodes], one handle per node of the graph
//   - [App.Graph], [App.Nodes], and [App.Root], the composition the App
//     publishes
package app
