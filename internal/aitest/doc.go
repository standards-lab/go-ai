// Package aitest holds the test helpers the module's test packages share: a token source that
// counts its calls, and the assertions the format packages' tests make of encoded bodies,
// refusals, and decoded responses.
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
//   - [SameArguments] holds two responses' function call arguments to their JSON value, so
//     an endpoint's respacing of them compares equal.
package aitest
