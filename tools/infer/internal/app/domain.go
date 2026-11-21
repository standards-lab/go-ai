package app

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

func defineDomain(g *graph.Graph, n *Nodes) {}

func mountDomain(root *cli.Command, n *Nodes, cmdFlags commandFlags) {
	root.Add(modelsCommand(n), embedCommand(n), chatCommand(n, cmdFlags.idleTimeout), transcribeCommand(n, cmdFlags.mode))
}

func modelsCommand(n *Nodes) *cli.Command {
	return (&cli.Command{
		Name:    "models",
		Summary: "List the endpoint's model IDs, one per line, each with its root where listed",
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			models, err := inv.Get(n.Client).Models(ctx)
			if err != nil {
				return err
			}
			for _, m := range models {
				line := m.ID
				if root := modelRoot(m); root != "" {
					line += "\t" + root
				}
				if _, err := fmt.Fprintln(inv.Stdout, line); err != nil {
					return err
				}
			}
			return nil
		},
	}).Use(n.Client)
}

func modelRoot(m ai.Model) string {
	var entry struct {
		Root string `json:"root"`
	}
	if err := jsonv2.Unmarshal(m.Native, &entry); err != nil {
		return ""
	}
	return entry.Root
}

func resolveModel(ctx context.Context, client *ai.Client, model string) (string, error) {
	if model != "" {
		return model, nil
	}
	models, err := client.Models(ctx)
	if err != nil {
		return "", fmt.Errorf("no model set, and listing the endpoint's: %w", err)
	}
	switch len(models) {
	case 0:
		return "", errors.New("no model set, and the endpoint lists none; pass --model")
	case 1:
		return models[0].ID, nil
	}
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return "", fmt.Errorf("several models: %s; pass --model", strings.Join(ids, ", "))
}

func embedCommand(n *Nodes) *cli.Command {
	return (&cli.Command{
		Name:     "embed",
		Summary:  "Embed each argument, or each line of stdin, writing one JSON vector per line",
		Synopsis: "[<input> ...]",
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			inputs := inv.Args
			if len(inputs) == 0 {
				var err error
				if inputs, err = stdinLines(inv.Stdin); err != nil {
					return err
				}
			}
			if len(inputs) == 0 {
				return cli.Usagef("no input: give each as an argument, or each as a line of stdin")
			}
			client := inv.Get(n.Client)
			model, err := resolveModel(ctx, client, inv.Get(n.Model))
			if err != nil {
				return err
			}
			e, err := client.Embed(ctx, &ai.EmbeddingRequest{Model: model, Input: inputs})
			if err != nil {
				return err
			}
			for _, v := range e.Vectors {
				line, err := json.Marshal(v)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(inv.Stdout, "%s\n", line); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintf(inv.Stderr, "usage: %d input tokens, %d total tokens\n", e.Usage.PromptTokens, e.Usage.TotalTokens)
			return err
		},
	}).Use(n.Client, n.Model)
}

// See docs/design.md, "Implementation notes".
func stdinLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, maxLine)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return lines, nil
}

const maxLine = 64 << 20

type chatFlags struct {
	instructions string
	images       []string
	reasoning    string
	maxTokens    int
	native       string
	stream       bool
}

func chatCommand(n *Nodes, idle *commandFlag) *cli.Command {
	var f chatFlags
	cmd := &cli.Command{
		Name:     "chat",
		Summary:  "Send a prompt, from the arguments or stdin, and write the model's reply",
		Synopsis: "[<prompt> ...]",
		Validate: idle.record,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			return chat(ctx, inv, n, &f)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&f.instructions, "instructions", "", "instructions that guide the model, ahead of the prompt")
	fs.Var(cli.StringsValue(&f.images, nil), "image", "an image file, sent inline, or an http(s) URL, sent as given")
	fs.StringVar(&f.reasoning, "reasoning", "", "the reasoning effort: none, minimal, low, medium, high, xhigh, or max")
	fs.IntVar(&f.maxTokens, "max-tokens", 0, "the most tokens the model generates, reasoning included; 16 or more through responses")
	fs.StringVar(&f.native, "native", "", "JSON object merged into the request's top level, for parameters the typed flags don't cover")
	fs.BoolVar(&f.stream, "stream", true, "stream the reply, writing its text as it arrives")
	idle.bind(fs, "", "end a streamed reply that sends nothing for this `duration`, such as 30s; 0 sets no bound")
	return cmd.Use(n.ChatClient, n.Model, n.ProfiledFormat, n.FormatName)
}

func chat(ctx context.Context, inv *cli.Invocation, n *Nodes, f *chatFlags) error {
	prompt, err := chatPrompt(inv)
	if err != nil {
		return err
	}
	if prompt == "" {
		return cli.Usagef("no prompt: give it as arguments, or on stdin")
	}
	req, err := f.request(prompt, inv.Get(n.Model))
	if err != nil {
		return err
	}
	wireFormat, formatName := inv.Get(n.ProfiledFormat), inv.Get(n.FormatName)
	if err := checkRequest(req, wireFormat, f.stream); err != nil {
		return refusedRequest(req, wireFormat, formatName, f.stream, err)
	}
	client := inv.Get(n.ChatClient)
	if req.Model, err = resolveModel(ctx, client, req.Model); err != nil {
		return err
	}
	out := &textOut{w: inv.Stdout}
	var resp *protocol.Response
	if f.stream {
		resp, err = streamReply(ctx, client, req, out)
	} else if resp, err = client.Generate(ctx, req); err == nil {
		err = out.write(resp.OutputText())
	}
	if err != nil {
		if out.last != 0 {
			_ = out.end()
		}
		return err
	}
	if err := out.end(); err != nil {
		return err
	}
	if err := writeSummary(inv.Stderr, resp); err != nil {
		return err
	}
	return replyError(resp)
}

func chatPrompt(inv *cli.Invocation) (string, error) {
	if len(inv.Args) > 0 {
		return strings.Join(inv.Args, " "), nil
	}
	b, err := io.ReadAll(inv.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	s := strings.TrimSuffix(string(b), "\n")
	if len(s) < len(b) {
		s = strings.TrimSuffix(s, "\r")
	}
	return s, nil
}

func (f *chatFlags) request(prompt, model string) (*protocol.Request, error) {
	content := []protocol.ContentPart{protocol.InputText{Text: prompt}}
	for _, ref := range f.images {
		u, err := imageURL(ref)
		if err != nil {
			return nil, err
		}
		content = append(content, protocol.InputImage{ImageURL: u})
	}
	req := &protocol.Request{
		Model:           model,
		Instructions:    f.instructions,
		Input:           []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: content}},
		Reasoning:       protocol.ReasoningParam{Effort: protocol.ReasoningEffort(f.reasoning)},
		MaxOutputTokens: f.maxTokens,
	}
	if f.native != "" {
		req.Native = jsontext.Value(f.native)
	}
	return req, nil
}

func imageURL(ref string) (string, error) {
	if u, err := url.Parse(ref); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return ref, nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("--image: %w", err)
	}
	mediaType := http.DetectContentType(data)
	if t := mime.TypeByExtension(filepath.Ext(ref)); t != "" {
		mediaType = t
	}
	mediaType, _, err = mime.ParseMediaType(mediaType)
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", cli.Usagef("--image %s: not an image, by its extension or its content", ref)
	}
	return protocol.DataURL(mediaType, data), nil
}

const standInModel = "unresolved"

func checkRequest(req *protocol.Request, wireFormat format.Format, stream bool) error {
	check := *req
	if check.Model == "" {
		check.Model = standInModel
	}
	if err := check.Validate(); err != nil {
		return err
	}
	encode := wireFormat.EncodeRequest
	if stream {
		encode = wireFormat.EncodeStreamRequest
	}
	_, err := encode(&check, make(http.Header))
	return err
}

// See docs/design.md, "Implementation notes".
func refusedRequest(req *protocol.Request, wireFormat format.Format, formatName string, stream bool, err error) error {
	maxTokens := "--max-tokens"
	if req.MaxOutputTokens == 0 {
		maxTokens = fmt.Sprintf("--format %s needs --max-tokens", formatName)
	}
	if u, ok := errors.AsType[*format.UnsupportedError](err); ok {
		if flag := featureFlag(u.Feature, maxTokens); flag != "" {
			if u.Declared {
				return cli.Usagef("%s: %w; declared by %s", flag, err, flagAndVar("profile"))
			}
			return cli.Usagef("%s: %w", flag, err)
		}
	}
	probes := []struct {
		flag  string
		clear func(*protocol.Request)
	}{
		{maxTokens, func(r *protocol.Request) { r.MaxOutputTokens = anyFormatMaxTokens }},
		{"--reasoning", func(r *protocol.Request) { r.Reasoning = protocol.ReasoningParam{} }},
		{"--native", func(r *protocol.Request) { r.Native = nil }},
	}
	var accepted, changed []string
	for _, p := range probes {
		r := *req
		p.clear(&r)
		switch perr := checkRequest(&r, wireFormat, stream); {
		case perr == nil:
			accepted = append(accepted, p.flag)
		case perr.Error() != err.Error():
			changed = append(changed, p.flag)
		}
	}
	flags := accepted
	if len(accepted) == 0 {
		flags = changed
	}
	if len(flags) != 1 || flags[0] == "" {
		return cli.Usagef("%w", err)
	}
	return cli.Usagef("%s: %w", flags[0], err)
}

func featureFlag(feature, maxTokens string) string {
	switch feature {
	case "reasoning effort":
		return "--reasoning"
	case "max output tokens":
		return maxTokens
	case "native member":
		return "--native"
	}
	return ""
}

func streamReply(ctx context.Context, client *ai.Client, req *protocol.Request, out *textOut) (*protocol.Response, error) {
	var acc protocol.Accumulator
	for event, err := range client.GenerateStream(ctx, req) {
		if err != nil {
			return nil, err
		}
		if err := acc.Add(event); err != nil {
			return nil, err
		}
		if d, ok := event.(protocol.OutputTextDelta); ok {
			if err := out.write(d.Delta); err != nil {
				return nil, err
			}
		}
	}
	return acc.Response(), nil
}

type textOut struct {
	w    io.Writer
	last byte
}

func (o *textOut) write(s string) error {
	if s == "" {
		return nil
	}
	if _, err := io.WriteString(o.w, s); err != nil {
		return err
	}
	o.last = s[len(s)-1]
	if f, ok := o.w.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func (o *textOut) end() error {
	if o.last == '\n' {
		return nil
	}
	return o.write("\n")
}

func writeSummary(w io.Writer, resp *protocol.Response) error {
	items := 0
	for _, it := range resp.Output {
		if _, ok := it.(protocol.Reasoning); ok {
			items++
		}
	}
	u := resp.Usage
	var ofOutput string
	switch n := u.OutputTokensDetails.ReasoningTokens; {
	case n > 0:
		ofOutput = fmt.Sprintf(" (%d reasoning)", n)
	case items > 0:
		ofOutput = " (reasoning not reported)"
	}
	_, err := fmt.Fprintf(w, "status: %s\nusage: %d input tokens, %d output tokens%s, %d total tokens\nreasoning items: %d\n",
		resp.Status, u.InputTokens, u.OutputTokens, ofOutput, u.TotalTokens, items)
	return err
}

func replyError(resp *protocol.Response) error {
	switch resp.Status {
	case protocol.StatusCompleted:
		return nil
	case protocol.StatusIncomplete:
		if d := resp.IncompleteDetails; d != nil && d.Reason != "" {
			return fmt.Errorf("%s: %s", resp.Status, d.Reason)
		}
	case protocol.StatusFailed:
		if e := resp.Error; e != nil && e.Message != "" {
			if e.Code != "" {
				return fmt.Errorf("%s: %s (%s)", resp.Status, e.Message, e.Code)
			}
			return fmt.Errorf("%s: %s", resp.Status, e.Message)
		}
		if e := resp.Error; e != nil && e.Code != "" {
			return fmt.Errorf("%s: %s", resp.Status, e.Code)
		}
	}
	return fmt.Errorf("reply ended %s", resp.Status)
}

func transcribeCommand(n *Nodes, mode *commandFlag) *cli.Command {
	cmd := &cli.Command{
		Name:     "transcribe",
		Summary:  "Transcribe an audio file, writing its transcript",
		Synopsis: "<file>",
		Args:     cli.ExactArgs(1),
		Validate: mode.record,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			file := inv.Args[0]
			client := inv.Get(n.Transcriber)
			model, err := resolveModel(ctx, client, inv.Get(n.Model))
			if err != nil {
				return err
			}
			t, err := client.Transcribe(ctx, &ai.TranscriptionRequest{
				Model:    model,
				Open:     func(context.Context) (io.ReadCloser, error) { return os.Open(file) },
				FileName: filepath.Base(file),
			})
			if err != nil {
				return transcribeError(err, inv.Get(n.TranscriptionMode), inv.Get(n.FormatName))
			}
			out := &textOut{w: inv.Stdout}
			if err := out.write(t.Text); err != nil {
				return err
			}
			if err := out.end(); err != nil {
				return err
			}
			_, err = fmt.Fprintf(inv.Stderr, "usage: %d input tokens, %d output tokens, %d total tokens\n",
				t.Usage.InputTokens, t.Usage.OutputTokens, t.Usage.TotalTokens)
			return err
		},
	}
	mode.bind(cmd.Flags(), defaultTranscriptionMode,
		"how to transcribe: route, the endpoint's transcription route, or chat, a chat request in the format")
	return cmd.Use(n.Transcriber, n.Model, n.TranscriptionMode, n.FormatName)
}

// See docs/design.md, "Implementation notes".
func transcribeError(err error, mode ai.TranscriptionMode, formatName string) error {
	switch mode {
	case ai.TranscribeChat:
		if refusesAudio(err) {
			return fmt.Errorf("--mode chat needs a format that carries audio; --format %s doesn't: %w", formatName, err)
		}
	case ai.TranscribeRoute:
		if apiErr, ok := errors.AsType[*ai.APIError](err); ok && apiErr.Status == http.StatusNotFound {
			return fmt.Errorf("%w\n(the endpoint serves no transcription route; try --mode chat)", err)
		}
	}
	return err
}
