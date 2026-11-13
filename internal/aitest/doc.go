// Package aitest holds the test helpers the module's test packages share: a loopback HTTP
// server that answers with a fixed reply and records the requests it receives, and the
// assertions the format packages' tests make of encoded bodies and decoded responses.
//
// Its exported identifiers:
//
//   - [Request] is one request a Server received, as it arrived on the wire.
//   - [Server] is a loopback HTTP server answering every request with one status and body.
//   - [NewServer] starts a Server, closed when the test ends.
//   - [Server.Requests] returns the requests the Server received, in order.
//   - [Server.Only] returns the one request the Server received, failing the test otherwise.
//   - [ReadTestdata] returns a file under the test's testdata directory.
//   - [Fold] folds streaming events with a protocol.Accumulator into its Response.
//   - [AssertResponse] fails the test unless two responses are the same.
//   - [AssertJSON] fails the test unless a body is the JSON wanted, whatever its member order
//     and spacing.
//   - [SameArguments] holds two responses' function call arguments to their JSON value, so
//     an endpoint's respacing of them compares equal.
package aitest
