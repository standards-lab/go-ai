package app

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
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

// defineDomain defines the domain services on g into n. infer has none: its
// commands call the client node's methods directly, so the domain layer is
// its commands alone.
func defineDomain(g *graph.Graph, n *Nodes) {}

// mountDomain mounts infer's commands at root, each declaring with Use the
// nodes it reads, so a run builds only what its command needs. cmdFlags
// are the commands' own flags over a variable, each bound by its command:
// chat's --idle-timeout, which the idle timeout node reads, and
// transcribe's --mode, which the transcription mode node reads.
func mountDomain(root *cli.Command, n *Nodes, cmdFlags commandFlags) {
	root.Add(modelsCommand(n), embedCommand(n), chatCommand(n, cmdFlags.idleTimeout), transcribeCommand(n, cmdFlags.mode))
}

// modelsCommand returns the models command, which writes the ID of each
// model the endpoint lists, one per line, in the endpoint's order.
func modelsCommand(n *Nodes) *cli.Command {
	return (&cli.Command{
		Name:    "models",
		Summary: "List the endpoint's model IDs, one per line",
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			models, err := inv.Get(n.Client).Models(ctx)
			if err != nil {
				return err
			}
			for _, m := range models {
				if _, err := fmt.Fprintln(inv.Stdout, m.ID); err != nil {
					return err
				}
			}
			return nil
		},
	}).Use(n.Client)
}

// resolveModel returns the model a request to client names: model, the
// model setting, when it is set, without a request; otherwise the one model
// the endpoint lists, which serves an endpoint that hosts a single model,
// such as a gateway's per-purpose API root. An endpoint that lists no
// model, or several, is refused with exit 1, not as a usage error: the
// command line is sound, and the endpoint's answer is what leaves the model
// open. Several are named in the endpoint's order. Every command that sends
// to a model resolves it here, after its own input is read and before it
// sends.
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

// embedCommand returns the embed command, which sends its inputs to the
// model as one batch, one Embed request, and writes each input's vector as
// one JSON array of numbers per line, in input order, then the request's
// token usage to stderr, so stdout holds the vectors alone. Each positional
// argument is one input, or, with none, each line of stdin, as
// [stdinLines] splits it. No input at all is a usage error, found before
// any request.
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

// stdinLines reads r to its end and returns each of its lines, without its
// line ending, "\n" or "\r\n". A final newline ends the last line rather
// than starting an empty one, so "a\nb\n" is two lines and an empty r is
// none. A blank line is a line, an empty input, which keeps the vectors
// embed writes line for line with the input it read.
func stdinLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	// A line is one input, so it may be as long as a model's context; the
	// scanner's default bound of 64 KiB would refuse a long one.
	sc.Buffer(nil, maxLine)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	return lines, nil
}

// maxLine is the longest stdin line embed reads as one input: 64 MiB, far
// beyond any embedding model's context.
const maxLine = 64 << 20

// chatFlags are chat's own flags, each setting a field of the request it
// sends.
type chatFlags struct {
	instructions string
	images       []string // each a file or an http(s) URL, in the order given
	reasoning    string   // the effort; empty leaves it to the endpoint
	maxTokens    int      // zero leaves the bound to the endpoint
	native       string   // a JSON object; empty sends none
	stream       bool
}

// chatCommand returns the chat command, which sends one prompt to the
// model, with chat's flags setting the request's other fields, and writes
// the reply's text to stdout, ending it with a newline, and then the
// reply's summary to stderr, as [writeSummary] writes it. It streams by
// default, writing each text delta as it arrives; with --stream=false it
// sends one unstreamed request and writes the reply's text once it is
// whole. The prompt is the positional arguments joined by spaces, or, with
// none, stdin, less one final line ending; no prompt at all is a usage
// error.
//
// The request is checked before anything is sent, the model listing of
// [resolveModel] included, by go-ai itself: protocol.Request's Validate and
// then the format's own encoding, the same checks the client makes before
// it sends. A refusal is a usage error, as each is a flag's value or a
// combination of them the format can't carry, such as a --max-tokens below
// go-ai's minimum, an effort go-ai doesn't define, a --native that isn't a
// JSON object with unique members or that sets a member a typed flag sets,
// or, through messages, no --max-tokens at all. The model isn't resolved
// yet when the request is checked, so an unset one is checked as a stand-in
// name.
//
// A reply that isn't completed keeps the text already written, and its
// summary, and exits 1 naming its status, with the reason an incomplete
// reply gives, as in "incomplete: max_output_tokens", or the error a failed
// one gives.
//
// chat sends through the chat client, whose streams idle, chat's
// --idle-timeout, set from INFER_IDLE_TIMEOUT, bounds: a stream that sends
// no bytes for that long ends with go-ai's error, exit 1, the text already
// written kept. An unstreamed reply isn't bounded by it.
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
	fs.StringVar(&f.reasoning, "reasoning", "", "the reasoning effort: none, low, medium, high, or xhigh")
	fs.IntVar(&f.maxTokens, "max-tokens", 0, "the most tokens the model generates, reasoning included; 16 or more")
	fs.StringVar(&f.native, "native", "", "JSON object merged into the request's top level, for parameters the typed flags don't cover")
	fs.BoolVar(&f.stream, "stream", true, "stream the reply, writing its text as it arrives")
	idle.bind(fs, "", "end a streamed reply that sends nothing for this `duration`, such as 30s; 0 sets no bound")
	return cmd.Use(n.ChatClient, n.Model, n.Format)
}

// chat is the chat command's Run, over the flags f holds.
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
	if err := checkRequest(req, inv.Get(n.Format), f.stream); err != nil {
		return cli.Usagef("%w", err)
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
		err = out.write(outputText(resp))
	}
	if err != nil {
		// The text already written is kept, its line ended.
		if out.last != 0 {
			_ = out.end()
		}
		return err
	}
	if err := out.end(); err != nil {
		return err
	}
	if err := writeSummary(inv.Stderr, resp, reportsReasoningTokens(inv.Get(n.Format))); err != nil {
		return err
	}
	return replyError(resp)
}

// chatPrompt returns the prompt: the positional arguments joined by spaces,
// or, with none, all of stdin less one final line ending, "\n" or "\r\n",
// which ends the prompt's last line rather than belonging to it.
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

// request returns the request of prompt to model, which may be empty, not
// yet resolved, with f's fields set: one user message of prompt's text and
// then each image, in the order given.
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

// imageURL returns the URL an --image of ref sends: ref as given when it is
// an http or https URL, and otherwise the file ref names, read whole and
// inlined as a base64 data URL. The file's media type is its extension's,
// or, for an extension with none, the one its content is sniffed as; one
// that isn't an image type is a usage error. A file that can't be read is
// an error, exit 1.
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
	// A type may carry parameters, such as a charset; the data URL takes
	// the type alone.
	mediaType, _, err = mime.ParseMediaType(mediaType)
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return "", cli.Usagef("--image %s: not an image, by its extension or its content", ref)
	}
	return protocol.DataURL(mediaType, data), nil
}

// standInModel is the model an unresolved request is checked under: any
// name will do, as the checks refuse only a request with none.
const standInModel = "unresolved"

// checkRequest returns go-ai's refusal of req, or nil: its Validate, and
// then wireFormat's encoding of it, streamed when stream is set, which
// refuses what the format can't carry. The client makes the same checks
// before it sends; making them here finds a refusal before resolveModel
// lists the models. An unresolved req is checked under standInModel.
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

// streamReply sends req through client as a stream, writing each output
// text delta to out as it arrives, and returns the response the stream's
// events fold to. A stream that breaks returns its error, the text already
// written kept.
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

// outputText returns the text of resp's output: each output_text part of
// its messages, in order, joined as they are.
func outputText(resp *protocol.Response) string {
	var b strings.Builder
	for _, it := range resp.Output {
		m, ok := it.(protocol.Message)
		if !ok {
			continue
		}
		for _, p := range m.Content {
			if t, ok := p.(protocol.OutputText); ok {
				b.WriteString(t.Text)
			}
		}
	}
	return b.String()
}

// textOut writes a reply's text to w as it comes, flushing w after each
// write when w buffers, as a *bufio.Writer does, and remembers the last
// byte written, so [textOut.end] ends the text with one newline.
type textOut struct {
	w    io.Writer
	last byte // the last byte written; zero before any
}

// write writes s to o's writer, and flushes it.
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

// end ends the text with a newline, unless it ends with one already; an
// empty text is an empty line.
func (o *textOut) end() error {
	if o.last == '\n' {
		return nil
	}
	return o.write("\n")
}

// writeSummary writes resp's summary to w: its status, its token usage,
// with the output tokens spent reasoning shown as part of the output, and
// the count of its reasoning output items, each on its own line:
//
//	status: completed
//	usage: 78 input tokens, 38 output tokens (27 reasoning), 116 total tokens
//	reasoning items: 1
//
// The reasoning tokens are as the endpoint reports them. When reasoning is
// false, the format's usage has no reasoning count, as messages' has none,
// so the summary says so, "(reasoning not reported)", rather than show a
// zero go-ai can't tell from none. The item count is the output's own, so
// it shows a reply with no reasoning whatever the endpoint reports.
func writeSummary(w io.Writer, resp *protocol.Response, reasoning bool) error {
	items := 0
	for _, it := range resp.Output {
		if _, ok := it.(protocol.Reasoning); ok {
			items++
		}
	}
	u := resp.Usage
	ofOutput := "reasoning not reported"
	if reasoning {
		ofOutput = fmt.Sprintf("%d reasoning", u.OutputTokensDetails.ReasoningTokens)
	}
	_, err := fmt.Fprintf(w, "status: %s\nusage: %d input tokens, %d output tokens (%s), %d total tokens\nreasoning items: %d\n",
		resp.Status, u.InputTokens, u.OutputTokens, ofOutput, u.TotalTokens, items)
	return err
}

// replyError returns nil for a completed resp, and otherwise the error,
// exit 1, that names its status: with an incomplete reply's reason, as in
// "incomplete: max_output_tokens", or a failed reply's error.
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

// transcribeCommand returns the transcribe command, which sends one audio
// file, its one positional argument, to the model through the transcriber,
// in the transcription mode --mode or INFER_TRANSCRIPTION_MODE sets, and
// writes the transcript to stdout, ending it with a newline, and then the
// token usage to stderr, so stdout holds the transcript alone.
//
// The file is opened only by go-ai's Transcribe, through the request's
// Open, once per attempt, so each attempt a retry makes sends the whole
// clip again; the file's base name names it, its extension giving a chat
// mode clip's encoding. infer neither reads nor checks the file itself, so
// a missing one is go-ai's error, exit 1. In chat mode, go-ai refuses a
// format that can't carry audio, such as messages, before the file is
// opened or anything is sent; the refusal is exit 1, not a usage error, as
// it is the endpoint's format and the mode together, not either value,
// that can't transcribe. It follows the model's resolution, as go-ai
// refuses in Transcribe, so with no model set the models are listed first.
// A chat-mode reply that isn't completed is go-ai's
// *ai.IncompleteTranscriptError, exit 1, naming its status and reason, and
// writes no transcript, as it holds part of one at most.
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
				return err
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
	return cmd.Use(n.Transcriber, n.Model)
}
