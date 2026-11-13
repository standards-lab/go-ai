package app

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
)

// envPrefix is the prefix of every environment variable infer reads its
// configuration from: go-core's config convention composes the rest, such
// as INFER_BASE_URL and INFER_SHUTDOWN_TIMEOUT.
const envPrefix = "INFER"

// defaultFormat is the format setting when neither INFER_FORMAT nor
// --format is given.
const defaultFormat = "completions"

// formats maps each format setting to its format package's constructor.
// This file is the one that names a format package; the rest of the
// program sees only the format.Format the format node holds.
var formats = map[string]func() format.Format{
	"completions": func() format.Format { return completions.New() },
	"messages":    func() format.Format { return messages.New() },
	"responses":   func() format.Format { return responses.New(responses.Options{}) },
}

// defineInfrastructure defines the infrastructure nodes on g into n: the
// lifecycle configuration; the endpoint settings, each read from its
// INFER_ variable and its root flag; and the client of the endpoint, built
// from them and INFER_TOKEN. It constructs nothing: each value is read in
// its node's constructor, which runs only when a Build reaches the node, so
// configuration is read only for a command that declares a node, never in
// [New] and never for a run that builds nothing.
func defineInfrastructure(g *graph.Graph, n *Nodes, flags *rootFlags) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.BaseURL = g.Define("base url", newSetting(flags, "base-url", ""))
	n.Format = g.Define("format", newFormat(flags))
	n.Model = g.Define("model", newSetting(flags, "model", ""))
	n.Client = g.Define("client", newClient(n.BaseURL, n.Format))
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
// name overrides: it starts from def, replaces it with the variable
// composed from envPrefix and name, such as INFER_BASE_URL for "base-url",
// when that is set and non-empty, and replaces that with the flag's value
// when the dispatch set the flag, even to an empty value. It reads only
// when a Build reaches the node.
func newSetting(flags *rootFlags, name, def string) func(*graph.Scope) (string, error) {
	return func(*graph.Scope) (string, error) {
		v := def
		var fromEnv *string
		if err := config.SetFromEnv(&fromEnv, config.EnvName(envPrefix, name), parseString); err != nil {
			return "", err
		}
		if fromEnv != nil {
			v = *fromEnv
		}
		if given, ok := flags.value(name); ok {
			v = given
		}
		return v, nil
	}
}

// parseString is a string setting's parse: any value is the setting.
func parseString(s string) (string, error) { return s, nil }

// newFormat returns the format node's constructor: the format the format
// setting names, from INFER_FORMAT and --format, completions when neither
// is given. A name formats doesn't hold is a usage error naming the flag
// and its variable.
func newFormat(flags *rootFlags) func(*graph.Scope) (format.Format, error) {
	setting := newSetting(flags, "format", defaultFormat)
	return func(s *graph.Scope) (format.Format, error) {
		name, err := setting(s)
		if err != nil {
			return nil, err
		}
		newFormat, ok := formats[name]
		if !ok {
			return nil, cli.Usagef("unknown format %q: %s is completions, messages, or responses", name, flagAndVar("format"))
		}
		return newFormat(), nil
	}
}

// newClient returns the client node's constructor: the go-ai client of the
// endpoint the base url node names, in the format node's format, sending
// INFER_TOKEN as its bearer token, read once here; an empty token sends no
// Authorization header, as go-ai's TokenSource documents. A missing base
// URL, or one ai.New refuses, is a usage error naming --base-url and
// INFER_BASE_URL. The client sets no timeout: a long stream is bounded by
// the run's context alone. It makes no request.
func newClient(baseURL *graph.Node[string], wireFormat *graph.Node[format.Format]) func(*graph.Scope) (*ai.Client, error) {
	return func(s *graph.Scope) (*ai.Client, error) {
		base := s.Use(baseURL)
		if base == "" {
			return nil, cli.Usagef("no base URL: set %s", flagOrVar("base-url"))
		}
		token := os.Getenv(config.EnvName(envPrefix, "token"))
		opts := ai.Options{
			Token: func(context.Context) (string, error) { return token, nil },
		}
		client, err := ai.New(base, &http.Client{}, s.Use(wireFormat), opts)
		if err != nil {
			// Every Options field infer sets is valid as set, so a refusal
			// is the base URL's.
			return nil, cli.Usagef("%s: %w", flagAndVar("base-url"), err)
		}
		return client, nil
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
// each naming the variable it overrides.
func bindRootFlags(fs *flag.FlagSet) *rootFlags {
	env := func(name string) string { return " (env " + config.EnvName(envPrefix, name) + ")" }
	fs.String("base-url", "", "the endpoint's API root, such as http://localhost:8080/v1"+env("base-url"))
	fs.String("format", defaultFormat, "the wire format: completions, messages, or responses"+env("format"))
	fs.String("model", "", "the model the request names"+env("model"))
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
