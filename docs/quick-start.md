# Quick start

A tutorial that builds a small program, `ask`, from an empty directory. Each step adds one
capability of go-ai:

- lists an endpoint's models, and asks one question
- reads the errors an endpoint returns
- streams a reply, calls a tool, and asks for JSON that a schema describes
- adds retries and timeouts, then switches to another wire format
- embeds passages and transcribes an audio clip

Each step writes one file in full and runs it. Every block comes from a program that compiles,
and every output is from a real run. The chat outputs are from Gemma 4 26B
(`nvidia/Gemma-4-26B-A4B-NVFP4`), served by vLLM. Outputs vary by model. The addresses, such as
`http://localhost:8000/v1`, stand in for your own. The [concepts](concepts.md) document explains
the model the program uses, and the [features](features.md) document states each rule.

## Prerequisites

- [Go](https://go.dev/dl/) 1.27 or later.
- An endpoint that serves Chat Completions with one chat model, such as a
  [vLLM](https://docs.vllm.ai) instance. The last two steps also use an embedding model and an
  audio model.

## 1. Create the module

```sh
mkdir ask && cd ask
go mod init example.com/ask
go get github.com/standards-lab/go-ai@v0.1.0
```

Name the endpoint by its API root, `/v1` included:

```sh
export ASK_BASE_URL=http://localhost:8000/v1
```

## 2. Build a client and list the models

`main.go` runs one step, named by its argument. Each later step adds a line to its `steps` map.

```go
// Command ask runs one step of the go-ai quick start against the endpoint ASK_BASE_URL names.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: ask <step>")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	model := os.Getenv("ASK_MODEL")

	steps := map[string]func() error{
		"models": func() error { return models(ctx, client) },
	}
	step, ok := steps[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "ask: unknown step %q\n", os.Args[1])
		os.Exit(2)
	}
	if err := step(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

`client.go` builds the client. `ai.New` takes the API root, the `*http.Client` to send with, and
the wire format. `ai.Options{}`, the zero value, adds nothing: no token, no retry, no timeout.

```go
package main

import (
	"net/http"
	"os"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// newClient returns a client of the endpoint ASK_BASE_URL names.
func newClient() (*ai.Client, error) {
	return ai.New(os.Getenv("ASK_BASE_URL"), http.DefaultClient, completions.New(), ai.Options{})
}
```

`models.go` prints each model the endpoint serves:

```go
package main

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-ai"
)

// models prints each model the endpoint serves.
func models(ctx context.Context, client *ai.Client) error {
	list, err := client.Models(ctx)
	if err != nil {
		return err
	}
	for _, m := range list {
		fmt.Println(m.ID)
	}
	return nil
}
```

```text
$ go run . models
nvidia/Gemma-4-26B-A4B-NVFP4
```

go-ai has no default model, so set the one the endpoint lists:

```sh
export ASK_MODEL=nvidia/Gemma-4-26B-A4B-NVFP4
```

## 3. Ask a question

A request is a `protocol.Request`: the model, optional instructions, and the input, a
conversation of items. The reply is a `protocol.Response` whose `Output` holds the model's
items. `generate.go` also defines two helpers that later steps reuse: `userText` builds a user
message, and `outputText` reads the text back out.

```go
package main

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// generate asks one question and prints the answer.
func generate(ctx context.Context, client *ai.Client, model string) error {
	resp, err := client.Generate(ctx, &protocol.Request{
		Model:        model,
		Instructions: "Answer in one sentence.",
		Input:        []protocol.Item{userText("What is a wire format?")},
	})
	if err != nil {
		return err
	}
	fmt.Println(outputText(resp))
	fmt.Printf("status: %s, %d tokens\n", resp.Status, resp.Usage.TotalTokens)
	return nil
}

// userText is a user message of one text part.
func userText(text string) protocol.Message {
	return protocol.Message{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{protocol.InputText{Text: text}},
	}
}

// outputText joins the text of every message in a response's output.
func outputText(resp *protocol.Response) string {
	var text string
	for _, item := range resp.Output {
		if m, ok := item.(protocol.Message); ok {
			for _, part := range m.Content {
				if t, ok := part.(protocol.OutputText); ok {
					text += t.Text
				}
			}
		}
	}
	return text
}
```

Add the step to `main.go`:

```go
		"generate": func() error { return generate(ctx, client, model) },
```

```text
$ go run . generate
A wire format is the specific sequence of bits and bytes used to encode data for transmission over a network or storage on a device.
status: completed, 57 tokens
```

## 4. Read a failure

An endpoint's refusal comes back as an `*ai.APIError` with the status, the endpoint's message,
and any Retry-After the reply asked for. Match it with `errors.As`. A model the endpoint doesn't
serve is one such refusal:

```text
$ ASK_MODEL=gemma go run . generate
ai: generate: 404 Not Found: The model `gemma` does not exist. Valid aliases: nvidia/Gemma-4-26B-A4B-NVFP4. (NotFoundError, 404)
exit status 1
```

An API root without `/v1` reaches the server but not its API, so it fails the same way. A root
with no scheme is refused by `ai.New` before anything is sent:

```text
$ ASK_BASE_URL=http://localhost:8000 go run . models
ai: models: 404 Not Found: {"detail":"Not Found"}
exit status 1
$ ASK_BASE_URL=localhost:8000/v1 go run . models
ai: base URL "localhost:8000/v1" isn't absolute
exit status 1
```

A reply can also arrive with no error and still be incomplete, for example when the model
stopped at its token bound. Check `resp.Status` before trusting the output, as step 7 does.

## 5. Stream the reply

`GenerateStream` returns an iterator. Ranging it sends the request, and each event arrives in
your loop, with no goroutine. A `protocol.Accumulator` folds the events into the same `Response`
that `Generate` returns.

```go
package main

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// stream prints the answer as it arrives, then folds the events into the whole response.
func stream(ctx context.Context, client *ai.Client, model string) error {
	req := &protocol.Request{
		Model: model,
		Input: []protocol.Item{userText("Name three rivers, one per line.")},
	}
	var acc protocol.Accumulator
	for event, err := range client.GenerateStream(ctx, req) {
		if err != nil {
			return err
		}
		if d, ok := event.(protocol.OutputTextDelta); ok {
			fmt.Print(d.Delta)
		}
		if err := acc.Add(event); err != nil {
			return err
		}
	}
	resp := acc.Response()
	fmt.Printf("\nstatus: %s, %d output tokens (%d reasoning)\n",
		resp.Status, resp.Usage.OutputTokens, resp.Usage.OutputTokensDetails.ReasoningTokens)
	return nil
}
```

```go
		"stream":   func() error { return stream(ctx, client, model) },
```

```text
$ go run . stream
Nile
Amazon
Mississippi
status: completed, 7 output tokens (0 reasoning)
```

Cancelling the context closes the connection. After a break or an error, `acc.Response()` holds
the output that arrived.

## 6. Call a tool

A `protocol.FunctionTool` describes a function by name and a JSON Schema of its arguments. When
the model calls it, the output holds a `protocol.FunctionCall`. Your program runs the function
and sends the result back as a `protocol.FunctionCallOutput` with the same `CallID`, after the
model's own output, and asks again.

```go
package main

import (
	"context"
	"encoding/json/jsontext"
	"fmt"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

var getWeather = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the current temperature in a city, in degrees Celsius.",
	Parameters: jsontext.Value(`{"type": "object",
		"properties": {"city": {"type": "string"}},
		"required": ["city"]}`),
}

// tools lets the model call get_weather, answers the call, and prints the model's reply.
func tools(ctx context.Context, client *ai.Client, model string) error {
	req := &protocol.Request{
		Model: model,
		Input: []protocol.Item{userText("What is the weather in Paris right now?")},
		Tools: []protocol.FunctionTool{getWeather},
	}
	resp, err := client.Generate(ctx, req)
	if err != nil {
		return err
	}

	// Send the conversation back: the model's output, then a result for each call.
	req.Input = append(req.Input, resp.Output...)
	for _, item := range resp.Output {
		if call, ok := item.(protocol.FunctionCall); ok {
			fmt.Printf("call: %s(%s)\n", call.Name, call.Arguments)
			req.Input = append(req.Input, protocol.FunctionCallOutput{
				CallID: call.CallID,
				Output: `{"city": "Paris", "celsius": 17}`,
			})
		}
	}

	resp, err = client.Generate(ctx, req)
	if err != nil {
		return err
	}
	fmt.Println(outputText(resp))
	return nil
}
```

```go
		"tools":    func() error { return tools(ctx, client, model) },
```

```text
$ go run . tools
call: get_weather({"city": "Paris"})
The current temperature in Paris is 17°C.
```

go-ai is stateless: every request carries the whole conversation, so a follow-up appends to
`Input` and sends it all again.

## 7. Ask for structured output

A `protocol.JSONSchemaFormat` in the request's `Text` asks the model to answer with JSON that a
schema describes. The answer is still text, so decode it yourself.

```go
package main

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

type river struct {
	Name      string `json:"name"`
	Continent string `json:"continent"`
	LengthKM  int    `json:"length_km"`
}

// structured asks for JSON that a schema describes and decodes it.
func structured(ctx context.Context, client *ai.Client, model string) error {
	resp, err := client.Generate(ctx, &protocol.Request{
		Model: model,
		Input: []protocol.Item{userText("Give the Nile's name, continent, and length.")},
		Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
			Name: "river",
			Schema: jsontext.Value(`{"type": "object",
				"properties": {
					"name": {"type": "string", "description": "The river's name."},
					"continent": {"type": "string"},
					"length_km": {"type": "integer", "description": "The length in kilometers."}},
				"required": ["name", "continent", "length_km"],
				"additionalProperties": false}`),
			Strict: true,
		}},
	})
	if err != nil {
		return err
	}
	if resp.Status != protocol.StatusCompleted {
		return fmt.Errorf("the reply is %s, not completed", resp.Status)
	}
	var r river
	if err := json.Unmarshal([]byte(outputText(resp)), &r); err != nil {
		return err
	}
	fmt.Printf("%+v\n", r)
	return nil
}
```

```go
		"structured": func() error { return structured(ctx, client, model) },
```

```text
$ go run . structured
{Name:Nile Continent:Africa LengthKM:6650}
```

The schema controls the shape of the answer, not whether it is right. The descriptions help the
model fill each field as you mean it.

## 8. Add retries and timeouts

go-ai sets no retry and no timeout until you ask. Replace `client.go`:

```go
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// newClient returns a client of the endpoint ASK_BASE_URL names.
func newClient() (*ai.Client, error) {
	// The library sets no timeout: bound the wait for a reply's headers on the transport.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	httpClient := &http.Client{Transport: transport}

	return ai.New(os.Getenv("ASK_BASE_URL"), httpClient, completions.New(), ai.Options{
		Token: func(context.Context) (string, error) { return os.Getenv("ASK_TOKEN"), nil },
		Retry: ai.RetryPolicy{
			MaxAttempts: 4,
			BaseDelay:   500 * time.Millisecond,
			MaxDelay:    30 * time.Second,
		},
		StreamIdleTimeout: time.Minute,
	})
}
```

- `Token` is called before each attempt, and its result is sent as the bearer token. An empty
  token sends none, so the program still runs against an endpoint that needs no credential.
- `Retry` sends a request up to four times in all, on the replies and failures that
  [features](features.md#retries) lists. It waits a random backoff of up to 500ms, doubling each
  time, at most 30s. A retried POST may run twice on the endpoint.
- `StreamIdleTimeout` ends a stream that sends nothing for a minute.
- The transport's `ResponseHeaderTimeout` bounds the wait for a reply. `http.Client.Timeout`
  would also cut off a long stream, so it stays unset.

```text
$ go run . generate
A wire format is a standardized way of encoding data into a sequence of bytes so that it can be transmitted over a network or stored in a file.
status: completed, 61 tokens
```

## 9. Switch the wire format

The request model doesn't change with the format. To send the same requests as Open Responses,
change the import and the constructor in `client.go`:

```go
	"github.com/standards-lab/go-ai/format/responses"
```

```go
	return ai.New(os.Getenv("ASK_BASE_URL"), httpClient, responses.New(responses.Options{}), ai.Options{
```

```text
$ go run . generate
A wire format is a standardized way of encoding data into a binary or text-based stream so that it can be transmitted across a network or stored in a file.
status: completed, 64 tokens
```

`messages.New()` sends Anthropic Messages the same way. Messages requires `MaxOutputTokens`, so
a request without it is refused before anything is sent. Each format's package comment lists
what it sends, refuses, and drops. The [features](features.md#the-formats) document compares
them.

## 10. Embed passages

`Embed` sends the whole batch as one request to the endpoint's embeddings route, whatever the
client's format. Each vector is placed by its input's index.

```go
package main

import (
	"context"
	"fmt"

	"github.com/standards-lab/go-ai"
)

// embed embeds two passages in one request.
func embed(ctx context.Context, client *ai.Client, model string) error {
	emb, err := client.Embed(ctx, &ai.EmbeddingRequest{
		Model: model,
		Input: []string{"The Nile flows north.", "The Amazon carries the most water."},
	})
	if err != nil {
		return err
	}
	for i, v := range emb.Vectors {
		fmt.Printf("%d: %d numbers, starting %.4f\n", i, len(v), v[0])
	}
	fmt.Printf("%d tokens\n", emb.Usage.TotalTokens)
	return nil
}
```

```go
		"embed":      func() error { return embed(ctx, client, model) },
```

Point it at an embedding endpoint:

```text
$ ASK_BASE_URL=http://localhost:8002/v1 ASK_MODEL=ibm-granite/granite-embedding-english-r2 go run . embed
0: 768 numbers, starting -1.4219
1: 768 numbers, starting -0.2383
16 tokens
```

The batch size, and how many batches run at once, are yours to choose.

## 11. Transcribe a clip

`Transcribe` has two modes. Route mode, the default, streams the clip to the endpoint's
`audio/transcriptions` route. Many audio chat models have no such route, and the endpoint
answers 404:

```text
ai: transcribe: 404 Not Found: {"detail":"Not Found"}
```

Chat mode sends the clip as input audio in a chat request instead, so it needs a format that
carries audio, as Chat Completions does. The mode is an option of the client, so
`transcribe.go` builds a client of its own:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// transcribe transcribes the clip ASK_CLIP names, sending it to the audio model as a chat
// request, for an endpoint that serves no transcription route.
func transcribe(ctx context.Context, model string) error {
	client, err := ai.New(os.Getenv("ASK_BASE_URL"), http.DefaultClient, completions.New(),
		ai.Options{TranscriptionMode: ai.TranscribeChat})
	if err != nil {
		return err
	}
	path := os.Getenv("ASK_CLIP")
	tr, err := client.Transcribe(ctx, &ai.TranscriptionRequest{
		Model:    model,
		Open:     func(context.Context) (io.ReadCloser, error) { return os.Open(path) },
		FileName: path,
		Language: "English",
	})
	if err != nil {
		return err
	}
	fmt.Println(tr.Text)
	return nil
}
```

```go
		"transcribe": func() error { return transcribe(ctx, model) },
```

`Open` is called once per attempt, so a retry reads the clip again. The file name's extension,
`.wav` or `.mp3`, gives the clip's encoding.

```text
$ ASK_BASE_URL=http://localhost:8001/v1 ASK_MODEL=google/gemma-4-E4B-it ASK_CLIP=phrase.wav go run . transcribe
The access code is 7429.
```

The clip is go-ai's own test file, `integration/testdata/phrase.wav`.

## Next

- [Concepts](concepts.md) explains the request model, the formats, and streaming.
- [Features](features.md) states every option and rule, package by package.
- [`infer`](../tools/infer/README.md) does all of this from the command line, with no program
  of your own.
