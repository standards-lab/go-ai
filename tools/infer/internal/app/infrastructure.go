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

// envPrefix is the prefix of every environment variable infer reads its
// configuration from: go-core's config convention composes the rest, such
// as INFER_BASE_URL and INFER_SHUTDOWN_TIMEOUT.
const envPrefix = "INFER"

// defaultFormat is the format setting when neither INFER_FORMAT nor
// --format is given.
const defaultFormat = "completions"

// formatOptions holds the Options each format is built with, the profile
// of the deployment it speaks to; its zero value is each format's spec. It
// is what --profile and INFER_PROFILE hold, a JSON object keyed by format
// name, as [newProfiledFormat] decodes it: each member is that format's
// Options, under the snake_case, case-strict names go-ai's tags give them.
type formatOptions struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

// formats maps each format setting, the name of a go-ai format package, to
// its constructor, which builds the format with its member of opts. This
// file is the one that names a format package; the rest of the program
// sees only the format.Format the format nodes hold and the name the format
// name node holds.
var formats = map[string]func(opts formatOptions) format.Format{
	"completions": func(opts formatOptions) format.Format { return completions.New(opts.Completions) },
	"messages":    func(opts formatOptions) format.Format { return messages.New(opts.Messages) },
	"responses":   func(opts formatOptions) format.Format { return responses.New(opts.Responses) },
}

// anyFormatMaxTokens is the least max output tokens every format accepts:
// responses' minimum, its spec's, where completions and messages take any
// positive cap.
const anyFormatMaxTokens = responses.MinMaxOutputTokens

// defineInfrastructure defines the infrastructure nodes on g into n: the
// lifecycle configuration; the endpoint settings and the retry policy, each
// read from its INFER_ variables and its root flags; the client of the
// endpoint, built from them, INFER_TOKEN, and INFER_HEADER, in the format's
// spec form; the profiled format, the same format under the deployment's
// profile read from INFER_PROFILE and --profile; and two clients of the
// same endpoint in the profiled format for one command each: the
// transcriber, in the transcription mode read from INFER_TRANSCRIPTION_MODE
// and transcribe's --mode, and the chat client, bounded by the idle timeout
// read from INFER_IDLE_TIMEOUT and chat's --idle-timeout. It constructs
// nothing: each value is read in its node's constructor, which runs only
// when a Build reaches the node, so configuration is read only for a
// command that declares a node, never in [New] and never for a run that
// builds nothing.
//
// The mode and the idle timeout are settings of the client,
// ai.Options.TranscriptionMode and StreamIdleTimeout, but each is one
// command's, so each reaches a client of its own rather than the one the
// other commands share: building them into that one would read both
// variables for every command, and an unknown mode would refuse a chat that
// never transcribes. All three clients are [newClient]'s, so the endpoint,
// the token, the headers, and the retry policy reach each alike.
//
// The profile is likewise read only by the commands whose requests it
// changes. Every field of a format's Options is about a chat request,
// which neither models nor embed sends, so the client they share is in the
// format's spec form and reads no profile: a malformed INFER_PROFILE
// fails neither.
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

// newLifecycleConfig reads the lifecycle configuration, the shutdown
// budget, from its default and INFER_SHUTDOWN_TIMEOUT, and validates it.
// The dispatcher adds this node to every Build, so a command that declares
// any node reads it.
func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

// newSetting returns the constructor of the string setting the root flag
// name overrides, as [resolveSetting] resolves it from def, the variable
// composed from envPrefix and name, such as INFER_BASE_URL for "base-url",
// and the flag. It reads only when a Build reaches the node.
func newSetting(flags *rootFlags, name, def string) func(*graph.Scope) (string, error) {
	return func(*graph.Scope) (string, error) {
		given, ok := flags.value(name)
		return resolveSetting(def, config.EnvName(envPrefix, name), given, ok)
	}
}

// resolveSetting returns a string setting: def, replaced by variable's
// value when that is set and non-empty, replaced in turn by flagValue when
// flagGiven says the dispatch set the flag, even to an empty value.
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

// parseString is a string setting's parse: any value is the setting.
func parseString(s string) (string, error) { return s, nil }

// newFormat returns the format node's constructor: the format the format
// name node names, built with each format's zero Options, its spec, as
// [formatOf] checks the name.
func newFormat(n *Nodes) func(*graph.Scope) (format.Format, error) {
	return func(s *graph.Scope) (format.Format, error) {
		build, err := formatOf(s, n)
		if err != nil {
			return nil, err
		}
		return build(formatOptions{}), nil
	}
}

// newProfiledFormat returns the profiled format node's constructor: the
// format the format name node names, as [formatOf] checks the name,
// built with its member of the profile the root flag --profile, over
// INFER_PROFILE, holds, as [newSetting] resolves it, or with its zero
// Options when the profile has no such member. An unset or empty profile
// is the zero profile, each format's spec.
//
// The profile is decoded whole, as go-ai's own tests decode Options, with
// encoding/json/v2 and json.RejectUnknownMembers, so every member is
// checked, not only the one in use: an unknown format name, an unknown or
// misspelled field, or a Go field name in place of the snake_case one,
// which the case:strict tags refuse, is a usage error naming the flag and
// its variable ahead of the decoder's error, and so is a value a field
// refuses, such as an effort protocol.ReasoningEffort doesn't name.
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

// formatOf returns the constructor of the format the format name node
// names, from [formats], refusing a name it doesn't hold as a usage error
// naming the flag and its variable.
func formatOf(s *graph.Scope, n *Nodes) (func(formatOptions) format.Format, error) {
	name := s.Use(n.FormatName)
	build, ok := formats[name]
	if !ok {
		return nil, cli.Usagef("unknown format %q: %s is completions, messages, or responses", name, flagAndVar("format"))
	}
	return build, nil
}

// newClient returns the constructor of a client node: the go-ai client of
// the endpoint the base url node names, in wireFormat's format, the format
// node's or the profiled format node's, with the retry policy node's
// policy and the options opts returns, which reads the nodes of the one
// command the client is for, as [buildClient] builds it. The client node's
// opts returns none, so it transcribes through the route, go-ai's default,
// and bounds no stream, as no command that declares it transcribes or
// streams.
func newClient(n *Nodes, wireFormat *graph.Node[format.Format], opts func(*graph.Scope) ai.Options) func(*graph.Scope) (*ai.Client, error) {
	return func(s *graph.Scope) (*ai.Client, error) {
		o := opts(s)
		o.Retry = s.Use(n.RetryPolicy)
		return buildClient(s.Use(n.BaseURL), s.Use(wireFormat), o)
	}
}

// buildClient returns the go-ai client of the endpoint at base, in
// wireFormat, with opts, sending INFER_TOKEN as its bearer token and
// INFER_HEADER's headers on every request, each read once here; an empty
// token sends no Authorization header, as go-ai's TokenSource documents,
// and an empty INFER_HEADER sends no header. A missing base URL is a usage
// error naming --base-url and INFER_BASE_URL; an INFER_HEADER line that
// isn't "Name: value" is one naming INFER_HEADER, as [parseHeader] reports
// it; so is anything ai.New refuses, as [settingsRefusal] reports it. The
// client sets no timeout: a long stream is bounded by the run's context and
// the idle timeout alone. It makes no request.
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

// headerVariable is the variable that holds the fixed headers sent on
// every request, which no flag overrides: a header such as a gateway's
// api-key is a secret, as the token is.
var headerVariable = config.EnvName(envPrefix, "header")

// parseHeader returns the headers v holds: one "Name: value" per line,
// split at the first colon, the line, the name, and the value each trimmed
// of surrounding space, and a blank line skipped; a name given on several
// lines has each value. A line with no colon, or no name before it, is a
// usage error naming INFER_HEADER and the line's number, never its text,
// since the value is a secret. It refuses nothing else: ai.New refuses a
// name the client sets or a name or value net/http can't send, and
// [settingsRefusal] names INFER_HEADER for it. An empty v is no header.
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

// writeEnvironment is the root's help Footer: it names INFER_TOKEN and
// INFER_HEADER, which [buildClient] reads and no flag overrides, so no
// flags section lists them. The help renders it in memory, so the write
// cannot fail.
func writeEnvironment(w io.Writer) {
	_, _ = fmt.Fprintf(w, "Environment:\n"+
		"  %s    the bearer token sent with each request; unset or empty sends none\n"+
		"  %s   headers sent with each request, one \"Name: value\" per line, such as a gateway's api-key\n",
		config.EnvName(envPrefix, "token"), headerVariable)
}

// refusesAudio reports whether err is a format's refusal of an audio part,
// as a chat-mode transcription sends: a *format.UnsupportedError of the
// feature "content part" and the value protocol.InputAudio, as messages,
// which has no audio block, refuses it. go-ai is the authority: transcribe
// reads its typed refusal only to word it.
func refusesAudio(err error) bool {
	u, ok := errors.AsType[*format.UnsupportedError](err)
	return ok && u.Feature == "content part" && u.Value == fmt.Sprintf("%T", protocol.InputAudio{})
}

// settingsRefusal returns the usage error of err, ai.New's refusal of base
// and opts, naming the settings it refused, as "--base-url
// (INFER_BASE_URL): " ahead of err, or err alone when it accepts each
// alone. go-ai is the authority on what it refuses, and its errors carry no
// field, so each setting is put to ai.New again on its own, beside values
// it never refuses: the base URL with no options; INFER_HEADER's headers
// alone, which go-ai refuses naming the header but never giving its value;
// the attempt count alone; the most delay alone; and the base delay under
// the longest most delay. A base delay above the most delay is refused in
// neither of the last two alone, only together, so a policy refused when no
// field is names both delays. When the most delay is zero in that case,
// the refusal says the base delay needs one at least as long, which says
// what to change, rather than repeat go-ai's "above its most delay"; go-ai
// has still refused it. A most delay left unset is the default, never zero. Only a refused New calls it, so the
// probes cost nothing in a run that builds.
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
		// Refused together and not alone, a zero most delay is under a
		// base delay above zero.
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

// The retry policy's defaults, each the setting when neither its flag nor
// its variable is given, or either is given empty: three attempts, the
// first wait at most half a second, doubling, and no wait, nor any
// Retry-After honored, longer than half a minute. go-ai's zero policy
// retries nothing, which suits a library; infer is run by hand and by
// scripts against endpoints that shed load, where a transient 503 should
// not end the run.
const (
	defaultRetries        = 3
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultRetryMaxDelay  = 30 * time.Second
)

// newRetryPolicy returns the retry policy node's constructor: the policy
// --retries, --retry-base-delay, and --retry-max-delay set, each over its
// variable as [newSetting] resolves it, and each its default when neither
// is given, or either is given empty. The default base delay is capped at
// a most delay given below it, so --retry-max-delay alone, such as 100ms,
// is a working policy rather than an inverted one; a base delay given
// above the default most delay is not raised to, and ai.New refuses it,
// naming both. A value that doesn't parse, a count as a whole number and
// each delay as a Go duration, is a usage error naming the flag and its
// variable. The node checks nothing else: ai.New refuses a negative or
// inverted policy, in [buildClient].
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
		// A negative most delay is left to ai.New to refuse, naming it
		// alone, rather than carried into the default base delay.
		if !baseGiven && p.MaxDelay >= 0 {
			p.BaseDelay = min(p.BaseDelay, p.MaxDelay)
		}
		return p, nil
	}
}

// parseRootSetting sets *v to the root flag name's setting, resolved by
// [newSetting] with no default, parsed by parse, and reports whether the
// setting was given: an empty setting leaves *v, its default, as it is,
// and one parse refuses is a usage error naming the flag and its
// variable.
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

// parseCount parses a whole number, such as a count of attempts.
func parseCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a whole number", s)
	}
	return n, nil
}

// transcriptionModes maps each transcription mode setting to go-ai's mode.
var transcriptionModes = map[string]ai.TranscriptionMode{
	"route": ai.TranscribeRoute,
	"chat":  ai.TranscribeChat,
}

// defaultTranscriptionMode is the transcription mode setting when neither
// INFER_TRANSCRIPTION_MODE nor --mode is given: the endpoint's
// transcription route, as go-ai's default is.
const defaultTranscriptionMode = "route"

// newTranscriptionMode returns the transcription mode node's constructor:
// the mode the setting names, as mode's [commandFlag.setting] resolves it,
// from defaultTranscriptionMode, INFER_TRANSCRIPTION_MODE, and --mode. A
// name transcriptionModes doesn't hold is a usage error naming the flag
// and its variable.
func newTranscriptionMode(mode *commandFlag) func(*graph.Scope) (ai.TranscriptionMode, error) {
	return func(*graph.Scope) (ai.TranscriptionMode, error) {
		name, err := mode.setting(defaultTranscriptionMode)
		if err != nil {
			return 0, err
		}
		m, ok := transcriptionModes[name]
		if !ok {
			return 0, cli.Usagef("unknown transcription mode %q: %s is route or chat", name, mode)
		}
		return m, nil
	}
}

// newIdleTimeout returns the idle timeout node's constructor: the
// duration idle's [commandFlag.setting] resolves from INFER_IDLE_TIMEOUT
// and --idle-timeout, zero, no bound, when neither is given. A value that
// isn't a Go duration, or is negative, is a usage error naming the flag
// and its variable. go-ai sets no bound for a negative StreamIdleTimeout
// rather than refusing one, so the refusal is infer's: a negative bound is
// a mistake, not a request for none.
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

// flagAndVar names the root flag name and the variable it overrides, as
// in "--format (INFER_FORMAT)".
func flagAndVar(name string) string {
	return fmt.Sprintf("--%s (%s)", name, config.EnvName(envPrefix, name))
}

// flagOrVar names the two ways to give the root flag name's setting, as in
// "--base-url or INFER_BASE_URL".
func flagOrVar(name string) string {
	return fmt.Sprintf("--%s or %s", name, config.EnvName(envPrefix, name))
}

// rootFlags are the root's flags, accepted at any depth: each overrides
// its setting's variable when it is given on the command line. A flag left
// unset overrides nothing, so its variable still applies.
type rootFlags struct {
	fs    *flag.FlagSet
	given map[string]bool // the flags the current dispatch set, from preRun
}

// bindRootFlags defines the root flags on fs, the root command's flag set,
// each naming the variable it overrides. The retry flags are strings, not
// an int and durations, so a value that doesn't parse is refused with its
// variable's, naming both, rather than by the flag package alone. Help
// would then list each as a string, so each usage backquotes the type the
// node parses it as, `int` or `duration`, which flag.UnquoteUsage, and so
// go-cli-sdk's help, takes as the flag's placeholder in place of its Go
// type's name.
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

// preRun is the root's PreRun: it records which root flags the dispatch
// set, wherever on the command line, before the Build reads them. A flag
// variable alone cannot say so, since a flag given its default value still
// overrides the environment.
func (f *rootFlags) preRun(_ context.Context, inv *cli.Invocation) error {
	f.given = make(map[string]bool)
	f.fs.VisitAll(func(fl *flag.Flag) {
		f.given[fl.Name] = inv.Changed(fl.Name)
	})
	return nil
}

// value returns the root flag name's value, and whether the dispatch set
// it.
func (f *rootFlags) value(name string) (string, bool) {
	if !f.given[name] {
		return "", false
	}
	return f.fs.Lookup(name).Value.String(), true
}

// commandFlags are the commands' own flags that override a variable, each
// one command's: the setting it overrides is that command's alone, so no
// other command accepts the flag or reads the variable.
type commandFlags struct {
	mode        *commandFlag // transcribe's --mode
	idleTimeout *commandFlag // chat's --idle-timeout
}

// newCommandFlags returns the commands' flags, unbound. --mode's variable
// is named for the setting, not the flag, so it reads as transcription's
// beside the other INFER_ variables.
func newCommandFlags() commandFlags {
	return commandFlags{
		mode:        &commandFlag{name: "mode", variable: config.EnvName(envPrefix, "transcription-mode")},
		idleTimeout: &commandFlag{name: "idle-timeout", variable: config.EnvName(envPrefix, "idle-timeout")},
	}
}

// commandFlag is one command's string flag over a variable, which a node
// of that command reads: the command binds it with [commandFlag.bind] and
// records, in its Validate, whether the dispatch set it, as the root's
// PreRun does for the root flags.
type commandFlag struct {
	name     string // the flag's name, such as "mode"
	variable string // the variable it overrides, such as INFER_TRANSCRIPTION_MODE
	value    string
	given    bool // whether the current dispatch set the flag, from record
}

// bind defines the flag on fs, its command's flag set, with def as its
// default and usage naming the variable it overrides. A usage that
// backquotes a word, as in "this `duration`", gives help that word as the
// flag's placeholder in place of "string", as [bindRootFlags] describes.
func (f *commandFlag) bind(fs *flag.FlagSet, def, usage string) {
	fs.StringVar(&f.value, f.name, def, usage+" (env "+f.variable+")")
}

// record is the command's Validate: it records whether the dispatch set
// the flag, even to its default, before the Build reads it. It refuses
// nothing; the flag's value is checked with its variable's, in the node.
func (f *commandFlag) record(inv *cli.Invocation) error {
	f.given = inv.Changed(f.name)
	return nil
}

// setting returns the flag's setting, as [resolveSetting] resolves it
// from def, the variable, and the flag.
func (f *commandFlag) setting(def string) (string, error) {
	return resolveSetting(def, f.variable, f.value, f.given)
}

// String names the flag and its variable, as in
// "--mode (INFER_TRANSCRIPTION_MODE)".
func (f *commandFlag) String() string {
	return fmt.Sprintf("--%s (%s)", f.name, f.variable)
}
