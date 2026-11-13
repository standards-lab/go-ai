package app_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/graph/graphtest"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// This file pins the graph's structural properties the composition root
// owns: each Nodes field names its node, and each leaf command's Build
// keeps every order its nodes declare. It builds the nodes without
// starting them, so it needs nothing running. A failing layer assertion
// prints the System's layer table.

// Each node's name is its Nodes field's name lowercased, ignoring the
// name's spaces, so a field names the node it holds without a comment
// repeating the name, and every field holds a handle once New returns.
func TestGraph_NodesNamedByTheirFields(t *testing.T) {
	a := app.New(cli.Streams{})
	graphtest.CheckNames(t, a.Nodes())
}

// leafLayers builds refs, a leaf's node set, with lifecycleConfig from g,
// as the dispatcher builds them for a dispatch to the leaf, and reports, as
// an error, a failed Build or each After edge the Build dropped, the latter
// carrying the layer table: a dropped After means the leaf's path forgot to
// declare a node something it builds is ordered against, so that order is
// silently lost for the leaf's run.
func leafLayers(g *graph.Graph, lifecycleConfig graph.Ref, refs []graph.Ref) error {
	sys, err := g.Build(append(slices.Clone(refs), lifecycleConfig)...)
	if err != nil {
		return fmt.Errorf("Build: %w", err)
	}
	var drops []string
	for _, d := range sys.Dropped() {
		drops = append(drops, fmt.Sprintf("%s's After of %s", d.Node.Name(), d.After.Name()))
	}
	if len(drops) > 0 {
		return fmt.Errorf("dropped %s; want every After to order\nlayers:\n%s", strings.Join(drops, ", "), sys)
	}
	return nil
}

// Every leaf of the App's tree builds, from the nodes its path declares and
// the lifecycle config node, a System that drops no order. The leaves are
// the root's own, not a list copied here, so a command mounted later is
// checked too. The Build constructs the client, which reads a base URL and
// sends nothing, so the one given is never reached.
func TestGraph_LeafLayers(t *testing.T) {
	apptest.Env(t, map[string]string{"INFER_BASE_URL": "http://127.0.0.1:1/v1"})
	a := app.New(cli.Streams{})
	for path, refs := range a.Root().Leaves() {
		t.Run(path, func(t *testing.T) {
			if err := leafLayers(a.Graph(), a.Nodes().LifecycleConfig, refs); err != nil {
				t.Error(err)
			}
		})
	}
}

// The layer guard fails a leaf it should, naming the dropped order and
// carrying the layer table, so the test above can fail: a fixture leaf
// declares a node ordered after one its path does not declare.
func TestGraph_LeafLayersGuardFails(t *testing.T) {
	a := app.New(cli.Streams{})
	undeclared := a.Graph().Value("undeclared", struct{}{})
	ordered := a.Graph().Value("ordered", struct{}{}, graph.After(undeclared))
	a.Root().Add((&cli.Command{
		Name:    "fixture",
		Summary: "Declare a node ordered after an undeclared one",
		Run:     func(context.Context, *cli.Invocation) error { return nil },
	}).Use(ordered))

	var found bool
	for path, refs := range a.Root().Leaves() {
		if path != "infer fixture" {
			continue
		}
		found = true
		err := leafLayers(a.Graph(), a.Nodes().LifecycleConfig, refs)
		// The same set again, for the table the error should carry.
		sys, buildErr := a.Graph().Build(append(slices.Clone(refs), a.Nodes().LifecycleConfig)...)
		if buildErr != nil {
			t.Fatalf("Build: %v", buildErr)
		}
		switch {
		case err == nil:
			t.Fatal("the guard passed a leaf whose node's order is dropped")
		case !strings.Contains(err.Error(), "dropped ordered's After of undeclared"):
			t.Errorf("guard error = %q, want it to name the dropped order", err)
		case !strings.Contains(err.Error(), "\nlayers:\n"+sys.String()):
			t.Errorf("guard error = %q, want it to carry the layer table:\n%s", err, sys)
		}
	}
	if !found {
		t.Fatal(`Leaves yielded no "infer fixture" leaf`)
	}
}
