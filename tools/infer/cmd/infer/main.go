// Command infer is go-ai's command-line client of one model endpoint. Its
// main derives the signal context and makes one call into the composition
// root, internal/app.
package main

import (
	"os"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
)

func main() {
	os.Exit(run())
}

// run runs infer and returns its exit code. It is separate from main so the
// deferred stop releases the signal registration before os.Exit, which
// skips deferred calls.
func run() int {
	ctx, stop := process.SignalContext()
	defer stop()
	return app.New(cli.Streams{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}).Run(ctx, os.Args[1:])
}
