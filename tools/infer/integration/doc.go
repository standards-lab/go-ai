// Package integration is infer's integration tier: the harness that runs
// the program as the built binary, one shot at a time, and, under the
// integration build tag, the suite that asserts its behavior through the
// interfaces a terminal uses: its arguments, its two output streams, and
// its exit code. The harness is built on go-core's processtest; nothing in
// the runtime exists for the tests' sake.
//
//   - [Main] is the suite's TestMain: it clears the INFER_ environment and
//     builds cmd/infer once per run.
//   - [Run] runs the built program once with its arguments and returns its
//     processtest.Result.
//
// The harness is untagged, so the unit tier type-checks it on every pull
// request; the suite files carry the integration tag.
package integration
