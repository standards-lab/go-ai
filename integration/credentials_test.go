package integration_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
)

// This file is untagged, so the unit tier runs its tests: the reading of a role's credentials,
// which the tagged scenarios use through newClient, is checked hermetically on every pull
// request.

// credentialVariables returns the names of the variables that hold the credentials of the role
// whose API root variable is baseVariable: AI_AGENT_BASE_URL's are AI_AGENT_TOKEN and
// AI_AGENT_HEADER.
func credentialVariables(baseVariable string) (token, header string) {
	prefix := strings.TrimSuffix(baseVariable, "_BASE_URL")
	return prefix + "_TOKEN", prefix + "_HEADER"
}

// withCredentials returns opts with the credentials of the role whose API root variable is
// baseVariable, read through getenv: AI_<ROLE>_TOKEN, when set and non-empty, as Options.Token,
// and the headers AI_<ROLE>_HEADER holds, as parseHeader reads them, as Options.Header. With
// both unset it returns opts as given, so the role's endpoint is sent what it was before either
// existed. Its error names the variable and the line, never a value, as a value is a secret.
func withCredentials(opts ai.Options, baseVariable string, getenv func(string) string) (ai.Options, error) {
	tokenVariable, headerVariable := credentialVariables(baseVariable)
	if token := getenv(tokenVariable); token != "" {
		opts.Token = func(context.Context) (string, error) { return token, nil }
	}
	header, err := parseHeader(getenv(headerVariable))
	if err != nil {
		return opts, fmt.Errorf("%s %w", headerVariable, err)
	}
	if header != nil {
		opts.Header = header
	}
	return opts, nil
}

// parseHeader returns the headers v holds: one "Name: value" per line, split at the first colon,
// the line, the name, and the value each trimmed of surrounding space, and a blank line skipped;
// a name given on several lines has each value. A line with no colon, or no name before it, is
// an error naming the line's number, never its text. It refuses nothing else: ai.New refuses a
// name the Client sets and a name or value net/http can't send. An empty v is nil.
func parseHeader(v string) (http.Header, error) {
	var h http.Header
	for i, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf(`line %d has no colon: each line is "Name: value"`, i+1)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf(`line %d has no name before its colon: each line is "Name: value"`, i+1)
		}
		if h == nil {
			h = make(http.Header)
		}
		h.Add(name, strings.TrimSpace(value))
	}
	return h, nil
}

// newClient returns a Client of the endpoint at base, the API root baseVariable holds, speaking
// f, adjusted by opts and by the role's credentials, which withCredentials reads through getenv.
// Its error names baseVariable and base when New refuses, and never a token or header value, so
// a failing scenario that prints it prints no secret.
func newClient(baseVariable, base string, f format.Format, opts ai.Options, getenv func(string) string) (*ai.Client, error) {
	opts, err := withCredentials(opts, baseVariable, getenv)
	if err != nil {
		return nil, err
	}
	c, err := ai.New(base, &http.Client{}, f, opts)
	if err != nil {
		return nil, fmt.Errorf("New(%s=%q): %w", baseVariable, base, err)
	}
	return c, nil
}

// env is a getenv over the variables it holds, any other unset.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// The credential variables are the role's, named from its API root variable.
func TestCredentialVariables(t *testing.T) {
	token, header := credentialVariables("AI_EMBEDDING_BASE_URL")
	if token != "AI_EMBEDDING_TOKEN" || header != "AI_EMBEDDING_HEADER" {
		t.Errorf("credentialVariables = %s, %s, want AI_EMBEDDING_TOKEN, AI_EMBEDDING_HEADER", token, header)
	}
}

// AI_<ROLE>_HEADER's lines each become a header: one line or several, each trimmed of
// surrounding space, a blank line skipped, a value kept whole after its first colon, and a name
// given twice keeping both values; blank lines alone are no header.
func TestParseHeader(t *testing.T) {
	for _, tc := range []struct {
		name, v string
		want    http.Header
	}{
		{"unset", "", nil},
		{"blank lines alone", "\n \t\n", nil},
		{"one", "api-key: secret", http.Header{"Api-Key": {"secret"}}},
		{
			name: "several, trimmed, blank lines skipped",
			v:    "\n  api-key :  secret \r\n\nx-route:a:b\n",
			want: http.Header{"Api-Key": {"secret"}, "X-Route": {"a:b"}},
		},
		{"a name twice", "x-tag: one\nX-Tag: two", http.Header{"X-Tag": {"one", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHeader(tc.v)
			if err != nil {
				t.Fatalf("parseHeader: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseHeader = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// With neither credential variable set, the options are as given: no token source and no
// header, as before the variables existed.
func TestWithCredentials_Unset(t *testing.T) {
	given := ai.Options{TranscriptionMode: ai.TranscribeChat}
	got, err := withCredentials(given, "AI_AGENT_BASE_URL", env(map[string]string{"AI_VISION_TOKEN": "other"}))
	if err != nil {
		t.Fatalf("withCredentials: %v", err)
	}
	if got.Token != nil || got.Header != nil || got.TranscriptionMode != ai.TranscribeChat {
		t.Errorf("withCredentials = %+v, want the options as given", got)
	}
}

// The role's token and headers reach the endpoint's requests: the token as the bearer token,
// each header beside it.
func TestNewClient_SendsCredentials(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"m","object":"model"}]}`)
	}))
	defer srv.Close()
	vars := map[string]string{"AI_AGENT_TOKEN": "tok", "AI_AGENT_HEADER": "api-key: key\nx-tenant: docs"}

	c, err := newClient("AI_AGENT_BASE_URL", srv.URL+"/v1", completions.New(), ai.Options{}, env(vars))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if _, err := c.Models(t.Context()); err != nil {
		t.Fatalf("Models: %v", err)
	}
	for name, want := range map[string]string{"Authorization": "Bearer tok", "Api-Key": "key", "X-Tenant": "docs"} {
		if got.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, got.Get(name), want)
		}
	}
}

// secret is the token and header value of each refused credential, which no failure may print.
const secret = "s3cret-value"

// Each refused credential fails newClient naming the header variable or the role's base URL,
// never the token or the header's value: a header line with no colon or no name, a name the
// Client sets, and a name or value net/http can't send.
func TestNewClient_FailureOmitsSecrets(t *testing.T) {
	const base = "http://localhost:8000/v1"
	for _, tc := range []struct {
		name, header, want string
	}{
		{"no colon", "api-key: ok\n" + secret, "AI_AGENT_HEADER line 2 has no colon"},
		{"no name", ": " + secret, "AI_AGENT_HEADER line 1 has no name"},
		{"authorization", "Authorization: Bearer " + secret, `New(AI_AGENT_BASE_URL="` + base + `"): ai: Options.Header names Authorization`},
		{"unsendable name", "api key: " + secret, `New(AI_AGENT_BASE_URL="` + base + `"): ai: Options.Header name "api key"`},
		{"unsendable value", "api-key: " + secret + "\x7f", `New(AI_AGENT_BASE_URL="` + base + `"): ai: Options.Header Api-Key has a value with a control character`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := map[string]string{"AI_AGENT_TOKEN": secret, "AI_AGENT_HEADER": tc.header}

			_, err := newClient("AI_AGENT_BASE_URL", base, completions.New(), ai.Options{}, env(vars))

			if err == nil {
				t.Fatal("newClient succeeded, want it refused")
			}
			if !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to start with %q", err, tc.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("err = %q, want it never to print the token or the header's value", err)
			}
		})
	}
}
