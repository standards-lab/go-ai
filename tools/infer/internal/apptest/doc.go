// Package apptest holds the fixtures internal/app's tests share for running
// infer in-process: over the composition [app.App] publishes, its graph and
// its nodes, against loopback endpoints, with the environment a run reads.
// It is infer's own; the tests never reach go-ai's internal helpers.
//
// [Builds] records the handle of every node a run's Build begins, through
// graph.Graph.Observe, so a test asserts which nodes a command builds,
// reading their names with [Names]. [Halt] fails the lifecycle
// configuration node, which the dispatcher adds to every Build, so a run of
// a command that declares nodes fails before anything starts. [Env] clears
// the INFER_ variables a test inherited and sets its own. [Serve] starts an
// [Endpoint], an httptest server that records each [Request] it receives
// before answering, such as with [Models] or [Embeddings].
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
//   - [Endpoint], [Serve], and [Endpoint.Requests], a loopback endpoint and
//     the [Request] values it received
//   - [Models], a handler that answers the model list, and [Embeddings],
//     one that answers an embeddings request
package apptest
