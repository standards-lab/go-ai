package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

const envPrefix = "INFER"

const defaultFormat = "completions"

type formatOptions struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

var formats = map[string]func(opts formatOptions) format.Format{
	"completions": func(opts formatOptions) format.Format { return completions.New(opts.Completions) },
	"messages":    func(opts formatOptions) format.Format { return messages.New(opts.Messages) },
	"responses":   func(opts formatOptions) format.Format { return responses.New(opts.Responses) },
}

const anyFormatMaxTokens = responses.MinMaxOutputTokens

// See docs/design.md, "Implementation notes".
func defineInfrastructure(g *graph.Graph, n *Nodes, flags *rootFlags, cmd commandFlags) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.BaseURL = g.Define("base url", newSetting(flags, "base-url", ""))
	n.FormatName = g.Define("format name", newSetting(flags, "format", defaultFormat))
	n.Format = g.Define("format", newFormat(n))
	n.ProfiledFormat = g.Define("profiled format", newProfiledFormat(n, flags))
	n.Model = g.Define("model", newSetting(flags, "model", ""))
	n.RetryPolicy = g.Define("retry policy", newRetryPolicy(flags))
	n.Client = g.Define("client", newClient(n, n.Format, func(*graph.Scope) ai.Options { return ai.Options{} }))
	n.TranscriptionMode = g.Define("transcription mode", newTranscriptionMode(cmd.mode))
	n.Transcriber = g.Define("transcriber", newClient(n, n.ProfiledFormat, func(s *graph.Scope) ai.Options {
		return ai.Options{TranscriptionMode: s.Use(n.TranscriptionMode)}
	}))
	n.IdleTimeout = g.Define("idle timeout", newIdleTimeout(cmd.idleTimeout))
	n.ChatClient = g.Define("chat client", newClient(n, n.ProfiledFormat, func(s *graph.Scope) ai.Options {
		return ai.Options{StreamIdleTimeout: s.Use(n.IdleTimeout)}
	}))
}

func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

func newSetting(flags *rootFlags, name, def string) func(*graph.Scope) (string, error) {
	return func(*graph.Scope) (string, error) {
		given, ok := flags.value(name)
		return resolveSetting(def, config.EnvName(envPrefix, name), given, ok)
	}
}

func resolveSetting(def, variable, flagValue string, flagGiven bool) (string, error) {
	v := def
	var fromEnv *string
	if err := config.SetFromEnv(&fromEnv, variable, parseString); err != nil {
		return "", err
	}
	if fromEnv != nil {
		v = *fromEnv
	}
	if flagGiven {
		v = flagValue
	}
	return v, nil
}

func parseString(s string) (string, error) { return s, nil }

func newFormat(n *Nodes) func(*graph.Scope) (format.Format, error) {
	return func(s *graph.Scope) (format.Format, error) {
		build, err := formatOf(s, n)
		if err != nil {
			return nil, err
		}
		return build(formatOptions{}), nil
	}
}

func newProfiledFormat(n *Nodes, flags *rootFlags) func(*graph.Scope) (format.Format, error) {
	return func(s *graph.Scope) (format.Format, error) {
		build, err := formatOf(s, n)
		if err != nil {
			return nil, err
		}
		v, err := newSetting(flags, "profile", "")(s)
		if err != nil {
			return nil, err
		}
		var opts formatOptions
		if v != "" {
			if err := json.Unmarshal([]byte(v), &opts, json.RejectUnknownMembers(true)); err != nil {
				return nil, cli.Usagef("%s: %w", flagAndVar("profile"), err)
			}
		}
		return build(opts), nil
	}
}

func formatOf(s *graph.Scope, n *Nodes) (func(formatOptions) format.Format, error) {
	name := s.Use(n.FormatName)
	build, ok := formats[name]
	if !ok {
		return nil, cli.Usagef("unknown format %q: %s is completions, messages, or responses", name, flagAndVar("format"))
	}
	return build, nil
}

func newClient(n *Nodes, wireFormat *graph.Node[format.Format], opts func(*graph.Scope) ai.Options) func(*graph.Scope) (*ai.Client, error) {
	return func(s *graph.Scope) (*ai.Client, error) {
		o := opts(s)
		o.Retry = s.Use(n.RetryPolicy)
		return buildClient(s.Use(n.BaseURL), s.Use(wireFormat), o)
	}
}

func buildClient(base string, wireFormat format.Format, opts ai.Options) (*ai.Client, error) {
	if base == "" {
		return nil, cli.Usagef("no base URL: set %s", flagOrVar("base-url"))
	}
	token := os.Getenv(config.EnvName(envPrefix, "token"))
	opts.Token = func(context.Context) (string, error) { return token, nil }
	header, err := parseHeader(os.Getenv(headerVariable))
	if err != nil {
		return nil, err
	}
	opts.Header = header
	client, err := ai.New(base, &http.Client{}, wireFormat, opts)
	if err != nil {
		return nil, settingsRefusal(base, wireFormat, opts, err)
	}
	return client, nil
}

var headerVariable = config.EnvName(envPrefix, "header")

func parseHeader(v string) (http.Header, error) {
	var h http.Header
	for i, line := range strings.Split(v, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, cli.Usagef("%s line %d has no colon: each line is \"Name: value\"", headerVariable, i+1)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, cli.Usagef("%s line %d has no name before its colon: each line is \"Name: value\"", headerVariable, i+1)
		}
		if h == nil {
			h = make(http.Header)
		}
		h.Add(name, strings.TrimSpace(value))
	}
	return h, nil
}

func writeEnvironment(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Environment:\n"+
		"  %s    the bearer token sent with each request; unset or empty sends none\n"+
		"  %s   headers sent with each request, one \"Name: value\" per line, such as a gateway's api-key\n",
		config.EnvName(envPrefix, "token"), headerVariable)
}

func refusesAudio(err error) bool {
	u, ok := errors.AsType[*format.UnsupportedError](err)
	return ok && u.Feature == "content part" && u.Value == fmt.Sprintf("%T", protocol.InputAudio{})
}

// See docs/design.md, "Implementation notes".
func settingsRefusal(base string, wireFormat format.Format, opts ai.Options, err error) error {
	refuses := func(base string, o ai.Options) bool {
		_, err := ai.New(base, &http.Client{}, wireFormat, o)
		return err != nil
	}
	if refuses(base, ai.Options{}) {
		return cli.Usagef("%s: %w", flagAndVar("base-url"), err)
	}
	p := opts.Retry
	var names []string
	if refuses(base, ai.Options{Header: opts.Header}) {
		names = append(names, headerVariable)
	}
	if refuses(base, ai.Options{Retry: ai.RetryPolicy{MaxAttempts: p.MaxAttempts}}) {
		names = append(names, flagAndVar("retries"))
	}
	if refuses(base, ai.Options{Retry: ai.RetryPolicy{BaseDelay: p.BaseDelay, MaxDelay: math.MaxInt64}}) {
		names = append(names, flagAndVar("retry-base-delay"))
	}
	if refuses(base, ai.Options{Retry: ai.RetryPolicy{MaxDelay: p.MaxDelay}}) {
		names = append(names, flagAndVar("retry-max-delay"))
	}
	if len(names) == 0 && refuses(base, ai.Options{Retry: p}) {
		if p.MaxDelay == 0 {
			return cli.Usagef("%s needs %s at least as long", flagAndVar("retry-base-delay"), flagAndVar("retry-max-delay"))
		}
		names = append(names, flagAndVar("retry-base-delay"), flagAndVar("retry-max-delay"))
	}
	if len(names) == 0 {
		return cli.Usagef("%w", err)
	}
	return cli.Usagef("%s: %w", strings.Join(names, " and "), err)
}

const (
	defaultRetries        = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultRetryMaxDelay  = 30 * time.Second
)

// See docs/design.md, "Implementation notes".
func newRetryPolicy(flags *rootFlags) func(*graph.Scope) (ai.RetryPolicy, error) {
	return func(s *graph.Scope) (ai.RetryPolicy, error) {
		p := ai.RetryPolicy{MaxAttempts: defaultRetries, BaseDelay: defaultRetryBaseDelay, MaxDelay: defaultRetryMaxDelay}
		if _, err := parseRootSetting(s, flags, "retries", parseCount, &p.MaxAttempts); err != nil {
			return p, err
		}
		baseGiven, err := parseRootSetting(s, flags, "retry-base-delay", time.ParseDuration, &p.BaseDelay)
		if err != nil {
			return p, err
		}
		if _, err := parseRootSetting(s, flags, "retry-max-delay", time.ParseDuration, &p.MaxDelay); err != nil {
			return p, err
		}
		if !baseGiven && p.MaxDelay >= 0 {
			p.BaseDelay = min(p.BaseDelay, p.MaxDelay)
		}
		return p, nil
	}
}

func parseRootSetting[T any](s *graph.Scope, flags *rootFlags, name string, parse func(string) (T, error), v *T) (bool, error) {
	setting, err := newSetting(flags, name, "")(s)
	if err != nil || setting == "" {
		return false, err
	}
	t, err := parse(setting)
	if err != nil {
		return false, cli.Usagef("%s: %w", flagAndVar(name), err)
	}
	*v = t
	return true, nil
}

func parseCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a whole number", s)
	}
	return n, nil
}

const defaultTranscriptionMode = "route"

func newTranscriptionMode(mode *commandFlag) func(*graph.Scope) (ai.TranscriptionMode, error) {
	return func(*graph.Scope) (ai.TranscriptionMode, error) {
		name, err := mode.setting(defaultTranscriptionMode)
		if err != nil {
			return 0, err
		}
		var m ai.TranscriptionMode
		if err := m.UnmarshalText([]byte(name)); err != nil {
			return 0, cli.Usagef("unknown transcription mode %q: %s is route or chat", name, mode)
		}
		return m, nil
	}
}

func newIdleTimeout(idle *commandFlag) func(*graph.Scope) (time.Duration, error) {
	return func(*graph.Scope) (time.Duration, error) {
		v, err := idle.setting("")
		if err != nil || v == "" {
			return 0, err
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, cli.Usagef("%s: %w", idle, err)
		}
		if d < 0 {
			return 0, cli.Usagef("%s: negative duration %s; 0 sets no bound", idle, d)
		}
		return d, nil
	}
}

func flagAndVar(name string) string {
	return fmt.Sprintf("--%s (%s)", name, config.EnvName(envPrefix, name))
}

func flagOrVar(name string) string {
	return fmt.Sprintf("--%s or %s", name, config.EnvName(envPrefix, name))
}

type rootFlags struct {
	fs    *flag.FlagSet
	given map[string]bool
}

func bindRootFlags(fs *flag.FlagSet) *rootFlags {
	env := func(name string) string { return " (env " + config.EnvName(envPrefix, name) + ")" }
	fs.String("base-url", "", "the endpoint's API root, such as http://localhost:8000/v1"+env("base-url"))
	fs.String("format", defaultFormat, "the wire format: completions, messages, or responses"+env("format"))
	fs.String("model", "", "the model the request names"+env("model"))
	fs.String("profile", "", "the deployment's profile, a `JSON` object keyed by format name, each member that format's options, "+
		`such as {"responses":{"refused_efforts":["none"]}}; unset or empty is each format's spec`+env("profile"))
	fs.String("retries", strconv.Itoa(defaultRetries), "the most attempts of each request, an `int` that counts the first, so 3 retries twice, "+
		"when the endpoint answers 408, 429, 502, 503, or 504 or the connection fails; 0 or 1 sends once"+env("retries"))
	fs.String("retry-base-delay", defaultRetryBaseDelay.String(), "the longest wait before the first retry, a `duration`, doubling for each retry after it; "+
		"at most --retry-max-delay, and the default at most a --retry-max-delay given below it"+env("retry-base-delay"))
	fs.String("retry-max-delay", defaultRetryMaxDelay.String(), "the longest wait between attempts, a `duration`, and the longest Retry-After waited for; "+
		"a longer Retry-After ends the retries"+env("retry-max-delay"))
	return &rootFlags{fs: fs}
}

func (f *rootFlags) preRun(_ context.Context, inv *cli.Invocation) error {
	f.given = make(map[string]bool)
	f.fs.VisitAll(func(fl *flag.Flag) {
		f.given[fl.Name] = inv.Changed(fl.Name)
	})
	return nil
}

func (f *rootFlags) value(name string) (string, bool) {
	if !f.given[name] {
		return "", false
	}
	return f.fs.Lookup(name).Value.String(), true
}

type commandFlags struct {
	mode        *commandFlag
	idleTimeout *commandFlag
}

func newCommandFlags() commandFlags {
	return commandFlags{
		mode:        &commandFlag{name: "mode", variable: config.EnvName(envPrefix, "transcription-mode")},
		idleTimeout: &commandFlag{name: "idle-timeout", variable: config.EnvName(envPrefix, "idle-timeout")},
	}
}

type commandFlag struct {
	name     string
	variable string
	value    string
	given    bool
}

func (f *commandFlag) bind(fs *flag.FlagSet, def, usage string) {
	fs.StringVar(&f.value, f.name, def, usage+" (env "+f.variable+")")
}

func (f *commandFlag) record(inv *cli.Invocation) error {
	f.given = inv.Changed(f.name)
	return nil
}

func (f *commandFlag) setting(def string) (string, error) {
	return resolveSetting(def, f.variable, f.value, f.given)
}

func (f *commandFlag) String() string {
	return fmt.Sprintf("--%s (%s)", f.name, f.variable)
}
