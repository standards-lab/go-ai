package ai_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
)

var strict = json.RejectUnknownMembers(true)

func e5() ai.EmbeddingProfile {
	return ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputQuery:    {Prefix: "query: "},
		ai.InputDocument: {Prefix: "passage: "},
	}}
}

func recordEmbeddings(t *testing.T) (*aitest.Endpoint, *[]aitest.EmbeddingRequest) {
	t.Helper()
	var got []aitest.EmbeddingRequest
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		got = append(got, req)
		return aitest.EmbeddingsReply{Vectors: make([][]float32, len(req.Input))}
	}})
	return e, &got
}

func embedOne(t *testing.T, c *ai.Client, e *aitest.Endpoint, got *[]aitest.EmbeddingRequest, req *ai.EmbeddingRequest) aitest.EmbeddingRequest {
	t.Helper()
	*got = nil
	if _, err := c.Embed(t.Context(), req); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("endpoint received %d embeddings requests, want 1", len(*got))
	}
	return (*got)[0]
}

func TestEmbedAppliesPrefix(t *testing.T) {
	e, got := recordEmbeddings(t)
	c := newClient(t, e.URL(), ai.Options{Embedding: e5()})
	cases := []struct {
		typ  ai.InputType
		want []string
	}{
		{ai.InputQuery, []string{"query: how tall", "query: "}},
		{ai.InputDocument, []string{"passage: how tall", "passage: "}},
		{ai.InputNone, []string{"how tall", ""}},
	}
	for _, tc := range cases {
		input := []string{"how tall", ""}
		r := embedOne(t, c, e, got, &ai.EmbeddingRequest{Model: "m", Input: input, InputType: tc.typ})
		if !slices.Equal(r.Input, tc.want) {
			t.Errorf("%v: input sent = %q, want %q", tc.typ, r.Input, tc.want)
		}
		if len(r.Native) != 0 {
			t.Errorf("%v: native sent = %s, want none", tc.typ, r.Native)
		}
		if !slices.Equal(input, []string{"how tall", ""}) {
			t.Errorf("%v: Embed changed the request's input to %q", tc.typ, input)
		}
	}
}

func TestEmbedSendsUndeclaredTypeBare(t *testing.T) {
	e, got := recordEmbeddings(t)
	for name, profile := range map[string]ai.EmbeddingProfile{
		"zero profile":    {},
		"other type only": {InputTypes: map[ai.InputType]ai.InputConvention{ai.InputDocument: {Prefix: "passage: ", Native: jsontext.Value(`{"input_type":"document"}`)}}},
	} {
		c := newClient(t, e.URL(), ai.Options{Embedding: profile})
		r := embedOne(t, c, e, got, &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: ai.InputQuery})
		if !slices.Equal(r.Input, []string{"a"}) || len(r.Native) != 0 {
			t.Errorf("%s: sent input %q, native %s; want [a] and no native", name, r.Input, r.Native)
		}
	}
}

func TestEmbedMergesConventionNative(t *testing.T) {
	e, got := recordEmbeddings(t)
	c := newClient(t, e.URL(), ai.Options{Embedding: ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputQuery:    {Native: jsontext.Value(`{"input_type": "search_query"}`)},
		ai.InputDocument: {Prefix: "search_document: ", Native: jsontext.Value(`{"input_type":"search_document","truncate":"END"}`)},
	}}})
	cases := []struct {
		typ        ai.InputType
		native     string
		wantInput  []string
		wantNative string
	}{
		{ai.InputQuery, "", []string{"a"}, `{"input_type":"search_query"}`},
		{ai.InputQuery, `{"dimensions":256}`, []string{"a"}, `{"dimensions":256,"input_type":"search_query"}`},
		{ai.InputDocument, `{"dimensions":256}`, []string{"search_document: a"},
			`{"dimensions":256,"input_type":"search_document","truncate":"END"}`},
		{ai.InputNone, `{"dimensions":256}`, []string{"a"}, `{"dimensions":256}`},
	}
	for _, tc := range cases {
		req := &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: tc.typ}
		if tc.native != "" {
			req.Native = jsontext.Value(tc.native)
		}
		r := embedOne(t, c, e, got, req)
		if !slices.Equal(r.Input, tc.wantInput) || string(r.Native) != tc.wantNative {
			t.Errorf("%v with native %s: sent input %q, native %s; want %q, %s", tc.typ, tc.native, r.Input, r.Native,
				tc.wantInput, tc.wantNative)
		}
	}
}

func TestEmbedRefusesConventionClash(t *testing.T) {
	e, _ := recordEmbeddings(t)
	var tokens int
	c := newClient(t, e.URL(), ai.Options{
		Token: func(context.Context) (string, error) { tokens++; return "", nil },
		Embedding: ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
			ai.InputQuery: {Native: jsontext.Value(`{"input_type":"search_query"}`)},
		}},
	})
	_, err := c.Embed(t.Context(), &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: ai.InputQuery,
		Native: jsontext.Value(`{"input_type":"classification"}`)})
	if err == nil || !strings.HasPrefix(err.Error(), "ai: embed: ") ||
		!strings.Contains(err.Error(), "native member input_type clashes with the query convention's") {
		t.Errorf("Embed error = %v, want the input_type clash refused", err)
	}
	if tokens != 0 {
		t.Errorf("the token source was called %d times, want 0", tokens)
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("endpoint received %d requests, want 0", n)
	}
}

func TestEmbedRefusesUnknownInputType(t *testing.T) {
	e, _ := recordEmbeddings(t)
	c := newClient(t, e.URL(), ai.Options{Embedding: e5()})
	_, err := c.Embed(t.Context(), &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: 7})
	if err == nil || !strings.Contains(err.Error(), "unknown input type 7") {
		t.Errorf("Embed error = %v, want the unknown input type refused", err)
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("endpoint received %d requests, want 0", n)
	}
}

func TestNewRefusesEmbeddingProfile(t *testing.T) {
	conv := func(native string) ai.EmbeddingProfile {
		return ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
			ai.InputDocument: {Prefix: "passage: ", Native: jsontext.Value(native)},
		}}
	}
	cases := []struct {
		name    string
		profile ai.EmbeddingProfile
		want    string
	}{
		{"input type none", ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{ai.InputNone: {Prefix: "x"}}},
			"convention for input type none"},
		{"unknown input type", ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{7: {Prefix: "x"}}},
			"convention for unknown input type 7"},
		{"native not an object", conv(`["a"]`), "document convention: native field isn't a JSON object"},
		{"native null", conv(`null`), "document convention: native field isn't a JSON object"},
		{"native repeated member", conv(`{"a":1,"a":2}`), "isn't a JSON object with unique member names"},
		{"native clashes with model", conv(`{"model":"x"}`), "native member model clashes"},
		{"native clashes with input", conv(`{"input":["x"]}`), "native member input clashes"},
		{"native clashes with encoding_format", conv(`{"encoding_format":"float"}`), "native member encoding_format clashes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(completions.Options{}),
				ai.Options{Embedding: tc.profile})
			if err == nil || !strings.HasPrefix(err.Error(), "ai: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("New = %v, %v; want an error containing %q", c, err, tc.want)
			}
		})
	}
}

func TestClientEmbeddingProfile(t *testing.T) {
	c := newClient(t, "http://h/v1", ai.Options{})
	if got := c.EmbeddingProfile(); !reflect.DeepEqual(got, ai.EmbeddingProfile{}) {
		t.Errorf("EmbeddingProfile() = %+v, want the zero profile", got)
	}

	opts := ai.Options{Embedding: ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputQuery:    {Prefix: "query: ", Native: jsontext.Value(`{"input_type":"query"}`)},
		ai.InputDocument: {Prefix: "passage: "},
	}}}
	want := ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputQuery:    {Prefix: "query: ", Native: jsontext.Value(`{"input_type":"query"}`)},
		ai.InputDocument: {Prefix: "passage: "},
	}}
	e, got := recordEmbeddings(t)
	c = newClient(t, e.URL(), opts)
	opts.Embedding.InputTypes[ai.InputQuery].Native[2] = 'X'
	delete(opts.Embedding.InputTypes, ai.InputDocument)
	if p := c.EmbeddingProfile(); !reflect.DeepEqual(p, want) {
		t.Errorf("after changing Options, EmbeddingProfile() = %+v, want %+v", p, want)
	}

	p := c.EmbeddingProfile()
	p.InputTypes[ai.InputQuery].Native[2] = 'X'
	p.InputTypes[ai.InputDocument] = ai.InputConvention{Prefix: "changed: "}
	p.InputTypes[ai.InputNone] = ai.InputConvention{Prefix: "none: "}
	if p := c.EmbeddingProfile(); !reflect.DeepEqual(p, want) {
		t.Errorf("after changing the returned profile, EmbeddingProfile() = %+v, want %+v", p, want)
	}
	r := embedOne(t, c, e, got, &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: ai.InputDocument})
	if !slices.Equal(r.Input, []string{"passage: a"}) {
		t.Errorf("input sent = %q, want [passage: a]", r.Input)
	}
	r = embedOne(t, c, e, got, &ai.EmbeddingRequest{Model: "m", Input: []string{"a"}, InputType: ai.InputQuery})
	if string(r.Native) != `{"input_type":"query"}` {
		t.Errorf("native sent = %s, want {\"input_type\":\"query\"}", r.Native)
	}
}

func TestEmbeddingProfileJSON(t *testing.T) {
	in := `{"input_types":{"document":{"prefix":"passage: "},"query":{"prefix":"query: ","native":{"input_type":"q"}}}}`
	var got ai.EmbeddingProfile
	if err := json.Unmarshal([]byte(in), &got, strict); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
		ai.InputDocument: {Prefix: "passage: "},
		ai.InputQuery:    {Prefix: "query: ", Native: jsontext.Value(`{"input_type":"q"}`)},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarshal = %+v, want %+v", got, want)
	}
	data, err := json.Marshal(got, json.Deterministic(true))
	if err != nil || string(data) != in {
		t.Errorf("Marshal = %s, %v; want %s", data, err, in)
	}
	var again ai.EmbeddingProfile
	if err := json.Unmarshal(data, &again, strict); err != nil || !reflect.DeepEqual(again, want) {
		t.Errorf("Unmarshal(Marshal) = %+v, %v; want %+v", again, err, want)
	}
	if data, err := json.Marshal(ai.EmbeddingProfile{}); err != nil || string(data) != `{}` {
		t.Errorf("Marshal(zero) = %s, %v; want {}", data, err)
	}
}

func TestEmbeddingProfileJSONRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   string
		opts []json.Options
		want string
	}{
		{"unknown member", `{"input_type":{}}`, nil, "input_type"},
		{"unknown convention member", `{"input_types":{"query":{"suffix":"x"}}}`, nil, "suffix"},
		{"Go field name", `{"InputTypes":{}}`, nil, "InputTypes"},
		{"Go field name, case-insensitive decoder", `{"input_types":{"query":{"Prefix":"x"}}}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "Prefix"},
		{"unknown input type", `{"input_types":{"passage":{"prefix":"x"}}}`, nil, `unknown input type "passage"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got ai.EmbeddingProfile
			err := json.Unmarshal([]byte(tc.in), &got, append([]json.Options{strict}, tc.opts...)...)
			if err == nil {
				t.Fatalf("Unmarshal(%s) = %+v, nil; want an error", tc.in, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Unmarshal error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestInputTypeText(t *testing.T) {
	for typ, name := range map[ai.InputType]string{ai.InputNone: "none", ai.InputQuery: "query", ai.InputDocument: "document"} {
		if got := typ.String(); got != name {
			t.Errorf("InputType(%d).String() = %q, want %q", int(typ), got, name)
		}
		text, err := typ.MarshalText()
		if err != nil || string(text) != name {
			t.Errorf("InputType(%d).MarshalText() = %q, %v; want %q", int(typ), text, err, name)
		}
		got := ai.InputType(-1)
		if err := got.UnmarshalText([]byte(name)); err != nil || got != typ {
			t.Errorf("UnmarshalText(%q) = %d, %v; want %d", name, int(got), err, int(typ))
		}
	}
}

func TestInputTypeTextRefusesUnknown(t *testing.T) {
	for _, typ := range []ai.InputType{3, 7, -1} {
		want := fmt.Sprintf("InputType(%d)", int(typ))
		if got := typ.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		if text, err := typ.MarshalText(); err == nil || !strings.HasPrefix(err.Error(), "ai: ") {
			t.Errorf("InputType(%d).MarshalText() = %q, %v; want an error", int(typ), text, err)
		}
	}
	for _, name := range []string{"", "Query", "DOCUMENT", "1", "passage"} {
		typ := ai.InputDocument
		err := typ.UnmarshalText([]byte(name))
		want := fmt.Sprintf("ai: unknown input type %q, want one of none, query, document", name)
		if err == nil || err.Error() != want {
			t.Errorf("UnmarshalText(%q) = %v, want %q", name, err, want)
		}
		if typ != ai.InputDocument {
			t.Errorf("UnmarshalText(%q) changed the type to %v", name, typ)
		}
	}
}
