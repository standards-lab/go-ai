package apptest

import (
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
)

// Recorder records events of type T in order. It is safe for concurrent
// use. The zero value is ready to use.
type Recorder[T any] struct {
	mu     sync.Mutex
	events []T
}

// Record appends event.
func (r *Recorder[T]) Record(event T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// Log returns a copy of the events recorded so far.
func (r *Recorder[T]) Log() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// Builds returns a Recorder of the handle of each node a's runs build, in
// the order each Build begins them: depth-first from the roots, each node
// once per Build, a node whose constructor fails included. A run that
// builds nothing records nothing. Call it before a runs: the graph calls
// only the observers it has when a Build begins.
func Builds(a *app.App) *Recorder[graph.Ref] {
	r := &Recorder[graph.Ref]{}
	a.Graph().Observe(r.Record)
	return r
}

// Names returns the name of each node in refs, in order, as each handle's
// Name reports it, so a test compares what a run built with the names the
// dispatcher labels its errors with.
func Names(refs []graph.Ref) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name()
	}
	return names
}

// ErrHalted is what a run of an App given to [Halt] fails with, labelled
// "lifecycle config".
var ErrHalted = errors.New("halted")

// Halt makes a's lifecycle configuration node fail with ErrHalted. The
// dispatcher adds that node to every Build of a command that declares
// nodes, so such a run fails its Build before anything starts, and a run
// whose constructors do no I/O does none at all. A run that builds nothing,
// such as help or version, is unaffected. Halt is a Replace, so it panics
// once a has run a Build.
func Halt(a *app.App) {
	a.Graph().Replace(a.Nodes().LifecycleConfig, func(*graph.Scope) (lifecycle.Config, error) {
		return lifecycle.Config{}, ErrHalted
	})
}

// envPrefix opens the name of every variable infer reads.
const envPrefix = "INFER_"

// Env gives the test the environment a run reads: every INFER_ variable the
// test inherited is set empty, which infer reads as unset, and then each of
// vars, a name and its value, is set. The variables are restored when the
// test ends. Like testing.T.Setenv, it can't be used in a parallel test.
func Env(t testing.TB, vars map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, envPrefix) {
			t.Setenv(name, "")
		}
	}
	for name, value := range vars {
		t.Setenv(name, value)
	}
}
