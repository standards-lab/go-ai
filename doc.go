// Package ai is go-ai's model client: one transport over the protocol, in the wire format its
// constructor is given, with Go primitives over the inference engines' native APIs and no
// vendor SDK.
//
// The module is four layers, importing one way. Package protocol is the model, Open Responses'
// item model in its stateless subset; package format is the contract a wire format meets; each
// wire format is a package below it, named for the endpoint it targets, format/completions,
// format/messages, and format/responses; and this package is the transport, which sends a
// protocol.Request in the format chosen by its constructor at the caller's composition root.
//
// The library sets no policy of its own: no default model, token budget, timeout, retry, size
// limit, or concurrency bound. The caller's http.Client and context carry the timeouts, and
// every option New takes is the caller's, its zero value setting none: the retry policy, and
// the idle bound on a started stream. The Client logs nothing; instrumentation is a
// RoundTripper on the caller's http.Client.
//
// Its exported identifiers:
//
//   - [New] returns a Client of an endpoint's base URL, sending with an http.Client in a wire
//     format, adjusted by Options.
//   - [Options] are New's optional inputs: query, token source, fixed headers, retry policy,
//     and the stream idle bound.
//   - [TokenSource] returns the bearer token for one attempt.
//   - [RetryPolicy] is the caller's opt-in policy for sending a request again: its most
//     attempts and the backoff's base and most delays.
//   - [Client] makes requests of one endpoint in one wire format.
//   - [Client.Generate] sends a Request and returns its Response.
//   - [Client.GenerateStream] sends a Request when ranged and yields its response's streaming
//     events.
//   - [APIError] is a non-2xx reply, matched with errors.As, carrying the reply's Retry-After.
package ai
