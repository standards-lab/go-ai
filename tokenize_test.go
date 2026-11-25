package ai_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

func serveTokenize(t *testing.T, reply aitest.Reply) (*aitest.Endpoint, *[]aitest.TokenizeRequest) {
	t.Helper()
	var got []aitest.TokenizeRequest
	e := aitest.Serve(t, aitest.Routes{Tokenize: func(_ context.Context, req aitest.TokenizeRequest) aitest.Reply {
		got = append(got, req)
		return reply
	}})
	return e, &got
}

func rootURL(e *aitest.Endpoint) string { return strings.TrimSuffix(e.URL(), "/v1") }

func TestTokenizeWireRequest(t *testing.T) {
	e, _ := serveTokenize(t, aitest.TokenizeReply{Count: 1, Tokens: []int{7}})
	c := newClient(t, rootURL(e), ai.Options{
		Query:  url.Values{"autoload": {"false"}},
		Token:  func(context.Context) (string, error) { return "secret", nil },
		Header: http.Header{"X-Consumer-Key": {"k"}},
	})
	if _, err := c.Tokenize(t.Context(), &ai.TokenizeRequest{Model: "embedding", Text: "hello"}); err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	reqs := e.Requests()
	if len(reqs) != 1 {
		t.Fatalf("endpoint received %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Method != http.MethodPost || got.Path != "/tokenize" || got.Query.Encode() != "autoload=false" {
		t.Errorf("request = %s %s?%s, want POST /tokenize?autoload=false", got.Method, got.Path, got.Query.Encode())
	}
	for k, want := range map[string]string{
		"Content-Type":   "application/json",
		"Accept":         "application/json",
		"Authorization":  "Bearer secret",
		"X-Consumer-Key": "k",
	} {
		if v := got.Header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if want := `{"model":"embedding","prompt":"hello","add_special_tokens":true}`; string(got.Body) != want {
		t.Errorf("body = %s\nwant   %s", got.Body, want)
	}
}

func TestTokenizeAppendsRouteToBase(t *testing.T) {
	e, _ := serveTokenize(t, aitest.TokenizeReply{Count: 1})
	for _, tc := range []struct{ base, want string }{
		{rootURL(e), "/tokenize"},
		{rootURL(e) + "/", "/tokenize"},
		{rootURL(e) + "/x/v1", "/x/v1/tokenize"},
	} {
		before := len(e.Requests())
		_, _ = newClient(t, tc.base, ai.Options{}).Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "a"})
		reqs := e.Requests()
		if len(reqs) != before+1 || reqs[before].Path != tc.want {
			t.Errorf("base %s: requests %+v, want one more to %s", tc.base, reqs[before:], tc.want)
		}
	}
}

func TestTokenizeSendsEmptyText(t *testing.T) {
	e, got := serveTokenize(t, aitest.TokenizeReply{Count: 2, Tokens: []int{0, 2}})
	if _, err := newClient(t, rootURL(e), ai.Options{}).Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m"}); err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Prompt != "" || !(*got)[0].AddSpecialTokens {
		t.Errorf("route got %+v, want one request of an empty prompt with special tokens", *got)
	}
}

func TestTokenizeDecodesCount(t *testing.T) {
	cases := []struct {
		name  string
		reply aitest.Reply
		want  ai.Tokens
	}{
		{"whole reply", aitest.TokenizeReply{Count: 3, MaxModelLen: 512, Tokens: []int{0, 33600, 2}},
			ai.Tokens{Count: 3, MaxModelLen: 512, IDs: []int{0, 33600, 2}}},
		{"count alone", aitest.TokenizeReply{Count: 4}, ai.Tokens{Count: 4}},
		{"null tokens", aitest.RawReply{Body: []byte(`{"count":2,"tokens":null}`)}, ai.Tokens{Count: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := serveTokenize(t, tc.reply)
			tokens, err := newClient(t, rootURL(e), ai.Options{}).Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "a"})
			if err != nil {
				t.Fatalf("Tokenize: %v", err)
			}
			if !reflect.DeepEqual(*tokens, tc.want) {
				t.Errorf("Tokenize = %+v, want %+v", *tokens, tc.want)
			}
		})
	}
}

func TestTokenizeAppliesPrefix(t *testing.T) {
	profile := e5()
	conv := profile.InputTypes[ai.InputQuery]
	conv.Native = jsontext.Value(`{"input_type":"search_query"}`)
	profile.InputTypes[ai.InputQuery] = conv
	e, got := serveTokenize(t, aitest.TokenizeReply{Count: 1})
	c := newClient(t, rootURL(e), ai.Options{Embedding: profile})
	cases := []struct {
		typ  ai.InputType
		want string
	}{
		{ai.InputQuery, "query: how tall"},
		{ai.InputDocument, "passage: how tall"},
		{ai.InputNone, "how tall"},
	}
	for _, tc := range cases {
		*got = nil
		before := len(e.Requests())
		if _, err := c.Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "how tall", InputType: tc.typ}); err != nil {
			t.Fatalf("%v: Tokenize: %v", tc.typ, err)
		}
		if len(*got) != 1 || (*got)[0].Prompt != tc.want {
			t.Errorf("%v: route got %+v, want prompt %q", tc.typ, *got, tc.want)
		}
		body := `{"model":"m","prompt":"` + tc.want + `","add_special_tokens":true}`
		if sent := string(e.Requests()[before].Body); sent != body {
			t.Errorf("%v: body = %s, want %s", tc.typ, sent, body)
		}
	}
}

func TestTokenizeSendsUndeclaredTypeBare(t *testing.T) {
	e, got := serveTokenize(t, aitest.TokenizeReply{Count: 1})
	for name, profile := range map[string]ai.EmbeddingProfile{
		"zero profile":    {},
		"other type only": {InputTypes: map[ai.InputType]ai.InputConvention{ai.InputDocument: {Prefix: "passage: "}}},
	} {
		*got = nil
		c := newClient(t, rootURL(e), ai.Options{Embedding: profile})
		if _, err := c.Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "how tall", InputType: ai.InputQuery}); err != nil {
			t.Fatalf("%s: Tokenize: %v", name, err)
		}
		if len(*got) != 1 || (*got)[0].Prompt != "how tall" {
			t.Errorf("%s: route got %+v, want prompt \"how tall\"", name, *got)
		}
	}
}

func TestTokenizeRefusesBeforeSending(t *testing.T) {
	cases := []struct {
		name string
		req  *ai.TokenizeRequest
		want string
	}{
		{"nil request", nil, "ai: tokenize: no request"},
		{"no model", &ai.TokenizeRequest{Text: "a"}, "ai: tokenize: request has no model"},
		{"unknown input type", &ai.TokenizeRequest{Model: "m", Text: "a", InputType: 7}, "ai: tokenize: request has unknown input type 7"},
		{"negative input type", &ai.TokenizeRequest{Model: "m", Text: "a", InputType: -1}, "ai: tokenize: request has unknown input type -1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := serveTokenize(t, aitest.TokenizeReply{Count: 1})
			calls := 0
			c := newClient(t, rootURL(e), ai.Options{
				Embedding: e5(),
				Token: func(context.Context) (string, error) {
					calls++
					return "t", nil
				},
			})
			_, err := c.Tokenize(t.Context(), tc.req)
			if err == nil || err.Error() != tc.want {
				t.Errorf("Tokenize error = %v, want %q", err, tc.want)
			}
			if calls != 0 {
				t.Errorf("token source called %d times, want 0", calls)
			}
			if n := len(e.Requests()); n != 0 {
				t.Errorf("endpoint received %d requests, want 0", n)
			}
		})
	}
}

func TestTokenizeAPIError(t *testing.T) {
	e, _ := serveTokenize(t, aitest.ErrorReply{Status: http.StatusBadRequest, Message: "prompt too long"})
	_, err := newClient(t, rootURL(e), ai.Options{}).Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "a"})
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Tokenize error %v wraps no *ai.APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message != "prompt too long" {
		t.Errorf("APIError = {Status:%d Message:%q}, want 400 \"prompt too long\"", apiErr.Status, apiErr.Message)
	}
	if !strings.HasPrefix(err.Error(), "ai: tokenize: ") {
		t.Errorf("error = %q, want the prefix \"ai: tokenize: \"", err)
	}
}

func TestTokenizeRefusesReply(t *testing.T) {
	raw := func(body string) aitest.Reply { return aitest.RawReply{Body: []byte(body)} }
	cases := []struct {
		name  string
		reply aitest.Reply
		want  string
	}{
		{"count disagrees with tokens", aitest.TokenizeReply{Count: 4, Tokens: []int{0, 1, 2}}, "response counts 4 tokens but lists 3"},
		{"count with empty tokens", raw(`{"count":2,"tokens":[]}`), "response counts 2 tokens but lists 0"},
		{"no count", raw(`{"tokens":[0,2],"max_model_len":512}`), "response has no count"},
		{"null count", raw(`{"count":null}`), "response has no count"},
		{"negative count", raw(`{"count":-1}`), "response counts -1 tokens"},
		{"not JSON", raw(`count: 3`), "decode response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := serveTokenize(t, tc.reply)
			_, err := newClient(t, rootURL(e), ai.Options{}).Tokenize(t.Context(), &ai.TokenizeRequest{Model: "m", Text: "a"})
			if err == nil || !strings.HasPrefix(err.Error(), "ai: tokenize: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Tokenize error = %v, want one prefixed \"ai: tokenize: \" containing %q", err, tc.want)
			}
		})
	}
}
