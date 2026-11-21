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

func withCredentials(opts ai.Options, baseVariable string, getenv func(string) string) (ai.Options, error) {
	role := strings.TrimSuffix(baseVariable, "_BASE_URL")
	tokenVariable, headerVariable := role+"_TOKEN", role+"_HEADER"
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

func modelVariable(baseVariable string) string {
	return strings.TrimSuffix(baseVariable, "_BASE_URL") + "_MODEL"
}

func namedModel(baseVariable string, getenv func(string) string) string {
	return getenv(modelVariable(baseVariable))
}

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

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

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

func TestNewClient_SendsCredentials(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"m","object":"model"}]}`)
	}))
	defer srv.Close()
	vars := map[string]string{"AI_AGENT_TOKEN": "tok", "AI_AGENT_HEADER": "api-key: key\nx-tenant: docs"}

	c, err := newClient("AI_AGENT_BASE_URL", srv.URL+"/v1", completions.New(completions.Options{}), ai.Options{}, env(vars))
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

const secret = "s3cret-value"

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

			_, err := newClient("AI_AGENT_BASE_URL", base, completions.New(completions.Options{}), ai.Options{}, env(vars))

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

func TestNamedModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"unset", map[string]string{"AI_VISION_MODEL": "other"}, ""},
		{"empty", map[string]string{"AI_AGENT_MODEL": ""}, ""},
		{"set", map[string]string{"AI_AGENT_MODEL": "gpt-oss-120b", "AI_VISION_MODEL": "other"}, "gpt-oss-120b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := namedModel("AI_AGENT_BASE_URL", env(tc.vars)); got != tc.want {
				t.Errorf("namedModel = %q, want %q", got, tc.want)
			}
		})
	}
}
