package app

import (
	"context"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
)

// App is the program: its dependency graph and the [Nodes] that describe
// it, its command tree, the root flags every command accepts, and the
// streams it reads from and reports to.
type App struct {
	graph   *graph.Graph
	nodes   Nodes
	root    *cli.Command
	streams cli.Streams
}

// New describes the graph, layer by layer, lowest first, builds the command
// tree, and returns the App. It is cold: it constructs nothing, reads no
// configuration, and writes nothing until [App.Run]. Every run reads from
// and writes to streams; only a command that reads standard input reads
// Stdin.
func New(streams cli.Streams) *App {
	a := &App{
		graph: graph.New(),
		root: &cli.Command{
			Name:    "infer",
			Summary: "infer sends requests to one model endpoint through go-ai.",
		},
		streams: streams,
	}
	flags := bindRootFlags(a.root.Flags())
	a.root.PreRun = flags.preRun
	mode := new(modeFlag)
	defineInfrastructure(a.graph, &a.nodes, flags, mode)
	defineDomain(a.graph, &a.nodes)
	a.root.Add(versionCommand())
	mountDomain(a.root, &a.nodes, mode)
	return a
}

// Run dispatches args, the program arguments without the program name, as
// a go-cli-sdk cli.Program over the App's tree, graph, and lifecycle
// configuration node, and returns the process exit code. The dispatcher
// builds the nodes the selected command's path declares with Use, with the
// lifecycle configuration node, and shuts what it built down before Run
// returns, so a Build, start, or shutdown error is reported with the
// command's result. An App runs one command at a time: each dispatch
// restores the flags the previous one set on the App's tree, so two runs
// at once would share its flag values.
func (a *App) Run(ctx context.Context, args []string) int {
	p := cli.Program{Root: a.root, Graph: a.graph, LifecycleConfig: a.nodes.LifecycleConfig}
	return p.Run(ctx, args, a.streams)
}

// Nodes is the App's graph, one handle per node: the single description of
// what the program is composed of. Each layer file's define function fills
// its own part of one Nodes value, and its constructors read the lower
// layers' nodes from that same value. Each field's node or group is named
// for the field, its words lowercased and spaced, so LifecycleConfig holds
// "lifecycle config", the name the dispatcher labels its errors with; the
// field names the node, and no comment repeats it.
type Nodes struct {
	LifecycleConfig *graph.Node[lifecycle.Config]
	BaseURL         *graph.Node[string]        // the endpoint's API root, such as http://localhost:8080/v1
	Format          *graph.Node[format.Format] // the wire format the client speaks
	Model           *graph.Node[string]        // the model a request names; empty when not given
	Client          *graph.Node[*ai.Client]    // the client of the endpoint, in the format, with the token
	// TranscriptionMode is how transcribe sends a clip: to the endpoint's
	// transcription route, or as a chat request in the format.
	TranscriptionMode *graph.Node[ai.TranscriptionMode]
	// Transcriber is the client transcribe sends through: Client's
	// endpoint, format, and token, transcribing in the transcription mode.
	// Only transcribe declares it, so no other command reads the mode.
	Transcriber *graph.Node[*ai.Client]
}

// Graph returns the graph a's commands are built from, as [New] described
// it. The program itself never calls it; it is published so a caller can,
// before the App runs, observe what a run builds with graph.Graph.Observe,
// or Replace a node's constructor with a substitute. A Replace changes the
// App itself, and the graph panics on a Replace once a run has built from
// it.
func (a *App) Graph() *graph.Graph { return a.graph }

// Nodes returns a handle on each of a's graph nodes, for a caller's Replace
// or for a command it adds over them.
func (a *App) Nodes() Nodes { return a.nodes }

// Root returns a's root command. A command a caller Adds to it before the
// App runs is part of the App's tree from then on.
func (a *App) Root() *cli.Command { return a.root }
