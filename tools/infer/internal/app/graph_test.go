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

func TestGraph_NodesNamedByTheirFields(t *testing.T) {
	a := app.New(cli.Streams{})
	graphtest.CheckNames(t, a.Nodes())
}

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
