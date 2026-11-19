// Package apptest holds the graph fixtures internal/app's tests share for
// running infer in-process: over the composition [app.App] publishes, its
// graph and its nodes, with the environment a run reads. The tests serve
// their loopback endpoint with go-ai's public aitest package, never with
// go-ai's internal helpers.
//
// [Builds] records the handle of every node a run's Build begins, through
// graph.Graph.Observe, so a test asserts which nodes a command builds,
// reading their names with [Names]. [Halt] fails the lifecycle
// configuration node, which the dispatcher adds to every Build, so a run of
// a command that declares nodes fails before anything starts. [Env] clears
// the INFER_ variables a test inherited and sets its own.
//
// The package exports:
//
//   - [Recorder], the events a test records, in order, and its
//     [Recorder.Record] and [Recorder.Log]
//   - [Builds], which returns a Recorder of the nodes the App's runs build,
//     and [Names], their names
//   - [Halt], which stops the App's runs before anything starts, and
//     [ErrHalted], what such a run fails with
//   - [Env], which gives a test the INFER_ environment a run reads
package apptest
