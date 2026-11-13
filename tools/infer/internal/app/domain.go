package app

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
)

// defineDomain defines the domain services on g into n. infer has none: its
// commands call the client node's methods directly, so the domain layer is
// its commands alone.
func defineDomain(g *graph.Graph, n *Nodes) {}

// mountDomain mounts infer's commands at root, each declaring with Use the
// nodes it reads, so a run builds only what its command needs.
func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(modelsCommand(n))
}

// modelsCommand returns the models command, which writes the ID of each
// model the endpoint lists, one per line, in the endpoint's order.
func modelsCommand(n *Nodes) *cli.Command {
	return (&cli.Command{
		Name:    "models",
		Summary: "List the endpoint's model IDs, one per line",
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			models, err := inv.Get(n.Client).Models(ctx)
			if err != nil {
				return err
			}
			for _, m := range models {
				if _, err := fmt.Fprintln(inv.Stdout, m.ID); err != nil {
					return err
				}
			}
			return nil
		},
	}).Use(n.Client)
}
