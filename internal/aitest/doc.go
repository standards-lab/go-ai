// Package aitest is go-ai's format-test kit: the assertions its test packages make of encoded
// bodies, refusals, decoded responses, and the requests an endpoint received, a reader of each
// test's recorded testdata, and a token source that counts its calls. It serves no endpoint: a
// test that needs one starts the public package github.com/standards-lab/go-ai/aitest, which any
// module's tests can use, while this kit stays go-ai's own.
//
// Its exported identifiers:
//
//   - [TokenCounter] is a token source that counts its calls.
//   - [TokenCounter.Token] returns a fixed token, counting the call.
//   - [ReplyMessage] returns a recorded error reply's message.
//   - [ReadTestdata] returns a file under the test's testdata directory.
//   - [Fold] folds streaming events with a protocol.Accumulator into its Response.
//   - [AssertResponse] fails the test unless two responses are the same.
//   - [AssertJSON] fails the test unless a body is the JSON wanted, whatever its member order
//     and spacing.
//   - [AssertUnsupported] fails the test unless an error is a message wrapping a format's
//     *format.UnsupportedError of the fields wanted.
//   - [Only] returns the one request a public aitest Endpoint received, failing the test
//     unless it received exactly one.
//   - [SameArguments] holds two responses' function call arguments to their JSON value, so
//     an endpoint's respacing of them compares equal.
package aitest
