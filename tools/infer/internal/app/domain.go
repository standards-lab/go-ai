package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-ai"
)

// defineDomain defines the domain services on g into n. infer has none: its
// commands call the client node's methods directly, so the domain layer is
// its commands alone.
func defineDomain(g *graph.Graph, n *Nodes) {}

// mountDomain mounts infer's commands at root, each declaring with Use the
// nodes it reads, so a run builds only what its command needs.
func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(modelsCommand(n), embedCommand(n))
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

// resolveModel returns the model a request to client names: model, the
// model setting, when it is set, without a request; otherwise the one model
// the endpoint lists, which serves an endpoint that hosts a single model,
// such as a gateway's per-purpose API root. An endpoint that lists no
// model, or several, is refused with exit 1, not as a usage error: the
// command line is sound, and the endpoint's answer is what leaves the model
// open. Several are named in the endpoint's order. Every command that sends
// to a model resolves it here, after its own input is read and before it
// sends.
func resolveModel(ctx context.Context, client *ai.Client, model string) (string, error) {
	if model != "" {
		return model, nil
	}
	models, err := client.Models(ctx)
	if err != nil {
		return "", fmt.Errorf("no model set, and listing the endpoint's: %w", err)
	}
	switch len(models) {
	case 0:
		return "", errors.New("no model set, and the endpoint lists none; pass --model")
	case 1:
		return models[0].ID, nil
	}
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return "", fmt.Errorf("several models: %s; pass --model", strings.Join(ids, ", "))
}

// embedCommand returns the embed command, which sends its inputs to the
// model as one batch, one Embed request, and writes each input's vector as
// one JSON array of numbers per line, in input order, then the request's
// token usage to stderr, so stdout holds the vectors alone. Each positional
// argument is one input, or, with none, each line of stdin, as
// [stdinLines] splits it. No input at all is a usage error, found before
// any request.
func embedCommand(n *Nodes) *cli.Command {
	return (&cli.Command{
		Name:     "embed",
		Summary:  "Embed each argument, or each line of stdin, writing one JSON vector per line",
		Synopsis: "[<input> ...]",
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			inputs := inv.Args
			if len(inputs) == 0 {
				var err error
				if inputs, err = stdinLines(inv.Stdin); err != nil {
					return err
				}
			}
			if len(inputs) == 0 {
				return cli.Usagef("no input: give each as an argument, or each as a line of stdin")
			}
			client := inv.Get(n.Client)
			model, err := resolveModel(ctx, client, inv.Get(n.Model))
			if err != nil {
				return err
			}
			e, err := client.Embed(ctx, &ai.EmbeddingRequest{Model: model, Input: inputs})
			if err != nil {
				return err
			}
			for _, v := range e.Vectors {
				line, err := json.Marshal(v)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(inv.Stdout, "%s\n", line); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(inv.Stderr, "usage: %d prompt tokens, %d total tokens\n", e.Usage.PromptTokens, e.Usage.TotalTokens)
			return err
		},
	}).Use(n.Client, n.Model)
}

// stdinLines reads r to its end and returns each of its lines, without its
// line ending, "\n" or "\r\n". A final newline ends the last line rather
// than starting an empty one, so "a\nb\n" is two lines and an empty r is
// none. A blank line is a line, an empty input, which keeps the vectors
// embed writes line for line with the input it read.
func stdinLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	// A line is one input, so it may be as long as a model's context; the
	// scanner's default bound of 64 KiB would refuse a long one.
	sc.Buffer(nil, maxLine)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return lines, nil
}

// maxLine is the longest stdin line embed reads as one input: 64 MiB, far
// beyond any embedding model's context.
const maxLine = 64 << 20
