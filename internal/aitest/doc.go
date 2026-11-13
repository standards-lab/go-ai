// Package aitest holds the test helpers the module's test packages share: a loopback HTTP
// server that answers with a fixed reply and records the requests it receives.
//
// Its exported identifiers:
//
//   - [Request] is one request a Server received, as it arrived on the wire.
//   - [Server] is a loopback HTTP server answering every request with one status and body.
//   - [NewServer] starts a Server, closed when the test ends.
//   - [Server.Requests] returns the requests the Server received, in order.
//   - [Server.Only] returns the one request the Server received, failing the test otherwise.
package aitest
