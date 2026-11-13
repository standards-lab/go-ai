package ai

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// TokenSource returns the bearer token for one attempt. The Client calls it once per attempt,
// so again for each attempt a RetryPolicy makes, and a source whose tokens expire can refresh
// them. An empty token sends no Authorization header.
type TokenSource func(ctx context.Context) (string, error)

// Options are New's optional inputs. The zero value selects every default: no query, no token,
// no header, no retry, and no idle bound. New copies Query, Header, and Retry, so changing them
// after New doesn't change the Client.
type Options struct {
	// Query is added to every request's URL.
	Query url.Values
	// Token authenticates each attempt as a bearer token. Nil sends no Authorization header.
	Token TokenSource
	// Header holds fixed headers sent on every request, such as Anthropic's x-api-key or a
	// gateway's consumer-key header. A header the format sets, such as anthropic-version,
	// replaces one of the same name. New refuses one naming Content-Type, Accept, or
	// Authorization, which the Client sets itself.
	Header http.Header
	// Retry is the policy for sending a request again when the endpoint may answer it later, as
	// RetryPolicy describes. The zero value retries nothing.
	Retry RetryPolicy
	// StreamIdleTimeout bounds each read of a started stream's body: a read that gets no bytes
	// for this long ends the stream with an error matching io.ErrUnexpectedEOF and
	// os.ErrDeadlineExceeded. Any bytes reset it, a comment such as llama.cpp's ping included,
	// and the clock runs only while a read waits, so a caller slow to range the stream isn't
	// timed. Zero sets none.
	StreamIdleTimeout time.Duration
}

// Client makes requests of one endpoint in one wire format. It is safe for concurrent use when
// its TokenSource and format are, and sets no limit of its own.
type Client struct {
	base   url.URL
	query  string
	token  TokenSource
	header http.Header
	http   *http.Client
	format format.Format
	// retry is the Options' policy, its zero value retrying nothing.
	retry RetryPolicy
	// idle is the Options' StreamIdleTimeout, 0 for none.
	idle time.Duration
	// jitter is the backoff's jitter source, fullJitter outside the tests.
	jitter func(time.Duration) time.Duration
}

// clientHeaders are the headers the Client sets on every request, which Options.Header may not
// name.
var clientHeaders = []string{"Content-Type", "Accept", "Authorization"}

// New returns a Client of the endpoint at baseURL, sending its requests with httpClient in
// wireFormat, such as completions.New(). It makes no request.
//
// baseURL is the endpoint's API root, such as "http://localhost:8080/v1", an absolute http or
// https URL with no query or fragment; Options.Query holds the query. Each route, such as
// "chat/completions", is appended to its path with exactly one slash between them.
//
// httpClient's transport and timeouts are the Client's: the library sets no timeout or limit of
// its own. A timeout is the caller's, set by the context, by the transport's
// ResponseHeaderTimeout, which bounds the wait for a reply without cutting off a stream, or by
// Options.StreamIdleTimeout; http.Client.Timeout, which bounds the whole exchange, ends a long
// stream part way.
//
// A nil httpClient or wireFormat panics by name, as a wiring defect. A baseURL that isn't an
// absolute http or https URL without query or fragment, a negative attempt count or delay, a
// BaseDelay above MaxDelay, or a Header naming Content-Type, Accept, or Authorization is an
// error.
func New(baseURL string, httpClient *http.Client, wireFormat format.Format, opts Options) (*Client, error) {
	if httpClient == nil {
		panic("ai: New: httpClient is nil; pass the *http.Client that sends the requests")
	}
	if wireFormat == nil {
		panic("ai: New: wireFormat is nil; pass a format's constructor, such as completions.New()")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("ai: no base URL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("ai: base URL: %w", err)
	}
	switch {
	case !u.IsAbs() || u.Host == "":
		return nil, fmt.Errorf("ai: base URL %q isn't absolute", baseURL)
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, fmt.Errorf("ai: base URL %q isn't http or https", baseURL)
	case u.RawQuery != "" || u.ForceQuery:
		return nil, fmt.Errorf("ai: base URL %q has a query; set Options.Query instead", baseURL)
	case u.Fragment != "" || u.RawFragment != "":
		return nil, fmt.Errorf("ai: base URL %q has a fragment", baseURL)
	}
	if err := opts.Retry.check(); err != nil {
		return nil, err
	}
	header, err := fixedHeader(opts.Header)
	if err != nil {
		return nil, err
	}
	// The query is encoded once, here, so a caller changing its url.Values after New can't
	// change the requests.
	return &Client{
		base:   *u,
		query:  opts.Query.Encode(),
		token:  opts.Token,
		header: header,
		http:   httpClient,
		format: wireFormat,
		retry:  opts.Retry,
		idle:   opts.StreamIdleTimeout,
		jitter: fullJitter,
	}, nil
}

// fixedHeader returns a copy of h under canonical names, refusing one the Client sets itself.
// Names that differ only in case are one header, their values in the order of the names'
// spellings, so the copy doesn't depend on the map's order.
func fixedHeader(h http.Header) (http.Header, error) {
	out := make(http.Header, len(h))
	for _, name := range slices.Sorted(maps.Keys(h)) {
		key := http.CanonicalHeaderKey(name)
		if slices.Contains(clientHeaders, key) {
			return nil, fmt.Errorf("ai: Options.Header names %s, which the Client sets", key)
		}
		out[key] = append(out[key], h[name]...)
	}
	return out, nil
}

// Generate sends req in the Client's format and returns the model's response. It validates req
// with its Validate method, and the format encodes it, before the token source is called or
// anything is sent. A non-2xx reply is an *APIError; a transport failure or a cancelled ctx is
// returned wrapped. With an Options.Retry policy, a reply or failure the policy retries is sent
// again, and the last attempt's error is returned as it would be without one.
func (c *Client) Generate(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("ai: generate: %w", err)
	}
	header := make(http.Header)
	body, err := c.format.EncodeRequest(req, header)
	if err != nil {
		return nil, fmt.Errorf("ai: generate: %w", err)
	}
	reply, err := c.post(ctx, c.format.Route(), header, body, c.format.DecodeError)
	if err != nil {
		return nil, fmt.Errorf("ai: generate: %w", err)
	}
	resp, err := c.format.DecodeResponse(reply)
	if err != nil {
		return nil, fmt.Errorf("ai: generate: %w", err)
	}
	return resp, nil
}

// endpoint is the URL of route: the base path and route joined by exactly one slash, with the
// Options' query.
func (c *Client) endpoint(route string) string {
	u := c.base.JoinPath(route)
	u.RawQuery = c.query
	return u.String()
}

// post sends body as a JSON POST to route, with header and the bearer token, and returns the
// reply's body, as send describes: retried as the Client's policy has it.
func (c *Client) post(ctx context.Context, route string, header http.Header, body []byte,
	decodeError func(body []byte) (message, typ, code string)) ([]byte, error) {
	resp, err := c.send(ctx, route, header, body, "application/json", "application/json", decodeError)
	if err != nil {
		return nil, err
	}
	return readReply(resp)
}

// readReply reads and closes the body of a 2xx reply.
func readReply(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return reply, nil
}

// request builds one attempt's POST of body, of contentType, to route, accepting accept, with
// the Options' fixed headers, then header, the format's, replacing any of the same name, then
// the bearer token. The token source is called here, before the request exists, so a source
// error sends nothing. A body of unknown length, such as a pipe, goes chunked. Its errors are
// every way an attempt fails before anything is sent, which is why it is apart from do: send
// returns them at once, never classifying them for a retry, so a token source that fails on its
// own network call isn't mistaken for the endpoint's connection failing.
func (c *Client) request(ctx context.Context, route string, header http.Header, body io.Reader, contentType, accept string) (*http.Request, error) {
	token := ""
	if c.token != nil {
		t, err := c.token(ctx)
		if err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}
		token = t
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(route), body)
	if err != nil {
		return nil, err
	}
	// Both are copied by canonical name, the fixed headers by New and the format's by
	// header.Set, so the format's replace the fixed ones by key. Each value slice is cloned, so
	// a transport that changes the request's header changes neither.
	for k, v := range c.header {
		req.Header[k] = slices.Clone(v)
	}
	for k, v := range header {
		req.Header[k] = slices.Clone(v)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

// do sends req and returns a 2xx reply, its body unread, for the caller to read and close. A
// non-2xx reply is an *APIError, its body read and closed, its details decoded by decodeError,
// the Client's format's, and its Retry-After read as the reply arrives. The Retry-After is also
// returned beside the error, reporting whether the reply had one that could be read, for send's
// wait: a Retry-After of 0 waits none, where one that can't be read leaves the wait to the
// backoff.
func (c *Client) do(req *http.Request, decodeError func(body []byte) (message, typ, code string)) (*http.Response, time.Duration, bool, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func() { _ = resp.Body.Close() }()
		wait, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		// A failed read leaves the part of the body that was read, which is still the best
		// account of the error there is.
		reply, _ := io.ReadAll(resp.Body)
		e := &APIError{Status: resp.StatusCode, Body: reply, RetryAfter: wait}
		e.Message, e.Type, e.Code = decodeError(reply)
		return nil, wait, ok, e
	}
	return resp, 0, false, nil
}
