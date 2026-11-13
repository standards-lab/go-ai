package ai

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// transcriptionsRoute is the OpenAI-compatible transcription endpoint, relative to the base URL.
const transcriptionsRoute = "audio/transcriptions"

// TranscriptionMode is how a Client transcribes: through the endpoint's transcription route, or
// as a chat request in the Client's format. It is set in Options.TranscriptionMode.
type TranscriptionMode int

// The modes a Client transcribes in.
const (
	// TranscribeRoute uploads the clip to the endpoint's OpenAI-compatible transcription route,
	// whatever format the Client speaks, streaming it as it is sent, never held whole. It is the
	// zero value. Prompt goes to the route as its prompt field, a decoding hint whose effect is
	// the endpoint's.
	TranscribeRoute TranscriptionMode = iota
	// TranscribeChat sends the clip to the model as a chat request in the Client's format, for
	// an endpoint with no transcription route, such as vLLM serving Gemma 4: one user message of
	// the clip as an InputAudio part and an instruction, through the format's route, as Generate
	// sends. It needs a format that carries audio, which today is Chat Completions; Messages
	// refuses audio, and Transcribe refuses the request before anything is sent. It reads the
	// clip whole, as the request's body holds it. Prompt is the instruction, in place of
	// TranscriptionInstruction's. As the route does, it returns a whole transcript or an error: a
	// reply the model didn't complete is an *IncompleteTranscriptError.
	TranscribeChat
)

// TranscriptionInstruction returns the instruction a chat-mode Transcribe gives the model when
// the request has no Prompt, modeled on Gemma 4's model card's transcription prompt. With a
// language, it names it as given, as Language holds it, so "English" reads as the model card
// has it:
//
//	Transcribe the following speech segment in English into English text. Only output the
//	transcription, with no newlines.
//
// With none, it names no language:
//
//	Transcribe the following speech segment into text. Only output the transcription, with no
//	newlines.
func TranscriptionInstruction(language string) string {
	if language == "" {
		return "Transcribe the following speech segment into text. Only output the transcription, with no newlines."
	}
	return "Transcribe the following speech segment in " + language + " into " + language +
		" text. Only output the transcription, with no newlines."
}

// TranscriptionRequest asks an audio model for the text of a clip. Transcription sits outside
// the protocol: in the Client's TranscriptionMode, it goes to the OpenAI-compatible
// transcription endpoint whatever format the Client speaks, an endpoint without the route
// answering with an *APIError, or to the model as a chat request in the Client's format.
//
// The transcription route's other form fields are not typed. response_format is always json,
// the one llama.cpp answers (it refuses text, srt, verbose_json, and vtt), so the reply
// carries no timestamps, and timestamp_granularities, which only verbose_json honors, has
// nothing to shape. temperature and max_tokens, which llama.cpp passes to its chat completion,
// and stream, which changes the reply's shape, are left to Native.
type TranscriptionRequest struct {
	// Model is the audio model's id. It is required.
	Model string
	// Open opens the clip. It is required. Transcribe calls it once per attempt, so again for
	// each attempt a RetryPolicy makes, and closes what it returns once the attempt is done
	// with it; Transcribe reads nothing of it after returning. In TranscribeRoute, the attempt
	// reads the clip as it uploads it, never holding it whole; in TranscribeChat, it reads the
	// clip whole, as the chat request's body holds it.
	Open func(ctx context.Context) (io.ReadCloser, error)
	// FileName names the clip, such as "clip.wav". It is required. In TranscribeRoute it names
	// the file part of the upload, as the endpoint takes the clip as a file. In TranscribeChat
	// its extension, in any case, gives the clip's protocol.AudioFormat: .wav or .mp3.
	FileName string
	// Language is the clip's language, when the caller knows it; empty sends none. In
	// TranscribeRoute it is the route's language field, such as "en", which llama.cpp adds to
	// its transcription instruction as a hint. In TranscribeChat, TranscriptionInstruction names
	// it, as given, in the instruction when Prompt is empty, so a name such as "English" reads as
	// the instruction means it where a code such as "en" doesn't, and it has no effect when
	// Prompt is set.
	Language string
	// Prompt guides the transcription; empty sends none. In TranscribeRoute it is the route's
	// prompt field, a decoding hint whose effect is the endpoint's: OpenAI reads it as preceding
	// context, while llama.cpp sends it to the model in place of its own transcription
	// instruction. In TranscribeChat it is the instruction the model is given beside the clip,
	// in place of TranscriptionInstruction's.
	Prompt string
	// Native is the native tier of TranscribeRoute: form fields sent as they are beside the
	// typed ones, for a feature of the endpoint the typed fields don't cover, such as
	// temperature. Each key goes out as a field once per value, keys in sorted order and each
	// key's values in order, after the typed fields and before the file. Its fields mean what
	// the endpoint makes them mean; one that changes the reply's shape, such as stream, makes a
	// reply Transcribe can't decode. A key that clashes with a field the typed fields set is
	// refused: model, response_format, and file always, and language and prompt when Language
	// and Prompt are set. So is an empty key, which names no field. TranscribeChat sends no
	// form, so it refuses a request with any.
	Native url.Values
}

// Transcription is an audio model's transcript of a clip.
type Transcription struct {
	// Text is the clip's transcript: the route's text, or in TranscribeChat, the reply's
	// output text.
	Text string
	// Usage counts the tokens the request took.
	Usage TranscriptionUsage
}

// TranscriptionUsage counts the tokens a transcription took, in the endpoint's names, which are
// protocol.Usage's: the clip and the instruction are input, the transcript output.
type TranscriptionUsage struct {
	// InputTokens is the tokens of the clip and the transcription instruction.
	InputTokens int
	// OutputTokens is the tokens of the transcript.
	OutputTokens int
	// TotalTokens is input and output tokens together.
	TotalTokens int
}

// Transcribe returns the transcript of req's clip, in the Client's TranscriptionMode. It sends
// through the one retry loop, as Generate does: each attempt calls the token source, then
// req.Open, and closes the clip once done with it. A clip that won't open, or whose read fails,
// is returned wrapped at once, not retried. A non-2xx reply is an *APIError; a transport failure
// or a cancelled ctx is returned wrapped. With an Options.Retry policy, a reply or failure the
// policy retries is sent again, with the clip opened again, and the last attempt's error is
// returned as it would be without one.
//
// In TranscribeRoute, the default, it uploads the clip to the endpoint's OpenAI-compatible
// transcription route, with the OpenAI-style error body's details on an *APIError. It refuses a
// nil request, one with no model, Open, or file name, and a Native key that is empty or clashes
// with a typed field, before the token source is called or anything is sent. Each attempt's
// upload streams: a goroutine writes the multipart form (the model, response_format json, the
// language and prompt when set, Native's fields, then the clip) into a pipe the request reads,
// so the clip is read as it is sent, never held whole, and goes chunked, its length unknown.
// Every attempt of one call sends the same form, its boundary included. An attempt ends only
// once that goroutine has, so it has stopped reading the clip: when the request ends first (a
// cancelled ctx, a transport failure, an early reply), the pipe is closed and the goroutine's
// next write fails. A clip whose Read blocks holds Transcribe until the Read returns.
//
// In TranscribeChat, it sends one request in the Client's format, to its route: Model, and one
// user message of the clip as a protocol.InputAudio part, in the format FileName's extension
// gives, and the instruction as an InputText part, Prompt or else TranscriptionInstruction's.
// It reads the clip whole, as the request's body holds it. It needs a format that carries
// audio, which today is Chat Completions. It refuses a nil request, one with no model or Open,
// a FileName whose extension isn't .wav or .mp3, any Native field, and a format that refuses
// audio, such as Messages, before the clip is opened, the token source is called, or anything
// is sent; an empty clip, which protocol.Request.Validate refuses, is refused once it is read,
// before it is sent. Text is the reply's output text, and Usage is the reply's usage. As the
// route returns a whole transcript or an error, so does chat mode: a reply whose status isn't
// completed, such as one the model stopped at the engine's output token bound, returns an
// *IncompleteTranscriptError and no Transcription, and is not sent again.
func (c *Client) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	if req == nil {
		return nil, errors.New("ai: transcribe: no request")
	}
	var (
		t   *Transcription
		err error
	)
	if c.transcription == TranscribeChat {
		t, err = c.transcribeChat(ctx, req)
	} else {
		t, err = c.transcribeRoute(ctx, req)
	}
	if err != nil {
		return nil, fmt.Errorf("ai: transcribe: %w", err)
	}
	return t, nil
}

// transcribeRoute uploads req's clip to the transcription route.
func (c *Client) transcribeRoute(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	fields, err := transcriptionFields(req)
	if err != nil {
		return nil, err
	}
	// One boundary for the call, so every attempt sends the same form.
	blank := multipart.NewWriter(io.Discard)
	boundary, contentType := blank.Boundary(), blank.FormDataContentType()
	upload := func(ctx context.Context, _ http.Header) (io.Reader, func() error, error) {
		rc, err := req.Open(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("open clip: %w", err)
		}
		clip := &clipReader{r: rc}
		pr, pw := io.Pipe()
		form := multipart.NewWriter(pw)
		if err := form.SetBoundary(boundary); err != nil {
			_ = rc.Close()
			return nil, nil, err
		}
		uploaded := make(chan struct{})
		go func() {
			defer close(uploaded)
			// The writer's error, or EOF on nil, ends the request body's read.
			pw.CloseWithError(writeTranscriptionForm(form, fields, req.FileName, clip))
		}()
		end := func() error {
			// An attempt that ended before reading the whole form leaves the goroutine blocked
			// on the pipe; closing the read side fails its write, and it ends. After a whole
			// read, this is a no-op.
			_ = pr.Close()
			<-uploaded
			// The clip is read, and a failure to close it changes nothing sent.
			_ = rc.Close()
			if clip.err != nil {
				// The upload failed on its own, reading the clip: that is the cause, whatever the
				// transport made of the body's failed read. Any other failure of the
				// goroutine's is a write into the pipe the request closed, which the attempt's
				// error accounts for.
				return fmt.Errorf("upload: %w", clip.err)
			}
			return nil
		}
		return pr, end, nil
	}
	reply, err := c.fetch(ctx, http.MethodPost, transcriptionsRoute, upload, contentType, format.DecodeOpenAIError)
	if err != nil {
		return nil, err
	}
	return decodeTranscription(reply)
}

// transcribeChat sends req's clip to the model as a chat request in the Client's format.
func (c *Client) transcribeChat(ctx context.Context, req *TranscriptionRequest) (*Transcription, error) {
	audioFormat, err := chatTranscription(req)
	if err != nil {
		return nil, err
	}
	// The format is asked to encode a request of the same shape, its clip a placeholder, before
	// the clip is opened or the token source called: a format that refuses audio refuses it
	// here, and what it refuses doesn't depend on the clip's bytes.
	probe := chatRequest(req, []byte{0}, audioFormat)
	if _, err := c.format.EncodeRequest(probe, make(http.Header)); err != nil {
		return nil, err
	}
	chat := func(ctx context.Context, header http.Header) (io.Reader, func() error, error) {
		rc, err := req.Open(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("open clip: %w", err)
		}
		data, err := io.ReadAll(rc)
		// The clip is read, and a failure to close it changes nothing sent.
		_ = rc.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("read clip: %w", err)
		}
		r := chatRequest(req, data, audioFormat)
		if err := r.Validate(); err != nil {
			return nil, nil, err
		}
		body, err := c.format.EncodeRequest(r, header)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewReader(body), nil, nil
	}
	reply, err := c.fetch(ctx, http.MethodPost, c.format.Route(), chat, "application/json", c.format.DecodeError)
	if err != nil {
		return nil, err
	}
	resp, err := c.format.DecodeResponse(reply)
	if err != nil {
		return nil, err
	}
	// A reply the model didn't finish holds part of a transcript at most, which reads as a whole
	// one. It is refused after the retry loop, not inside it: sending the same clip again meets
	// the same bound or filter.
	if resp.Status != protocol.StatusCompleted {
		e := &IncompleteTranscriptError{Status: resp.Status}
		if resp.IncompleteDetails != nil {
			e.Reason = resp.IncompleteDetails.Reason
		}
		return nil, e
	}
	return &Transcription{
		Text: outputText(resp),
		Usage: TranscriptionUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}

// IncompleteTranscriptError is a chat-mode Transcribe's reply that isn't completed, so it holds
// part of a transcript at most: most often one incomplete because the model stopped at the
// engine's output token bound, which a chat-mode request can't raise, as it takes no Native
// field. Transcribe returns it wrapped, with no Transcription, so callers reach it with
// errors.As. It is not retried: the same clip meets the same bound or filter again.
type IncompleteTranscriptError struct {
	// Status is the reply's status: protocol.StatusIncomplete, or any other but
	// protocol.StatusCompleted.
	Status protocol.Status
	// Reason is why the reply is incomplete, from its IncompleteDetails, or empty when it gave
	// none.
	Reason protocol.IncompleteReason
}

// Error reports the status and the reason, when the reply gave one, then what they mean and what
// the caller can do about it, such as:
//
//	transcript incomplete (status incomplete, reason max_output_tokens): the reply stopped at
//	the engine's output token bound before the transcript ended; send a shorter clip or raise
//	the engine's default bound
func (e *IncompleteTranscriptError) Error() string {
	status := string(e.Status)
	if status == "" {
		status = "none"
	}
	s := "transcript incomplete (status " + status
	if e.Reason != "" {
		s += ", reason " + string(e.Reason)
	}
	return s + "): " + e.cause()
}

// cause says what e's status and reason mean, and what the caller can do about them.
func (e *IncompleteTranscriptError) cause() string {
	switch {
	case e.Status == protocol.StatusIncomplete && e.Reason == protocol.ReasonMaxOutputTokens:
		return "the reply stopped at the engine's output token bound before the transcript ended; " +
			"send a shorter clip or raise the engine's default bound"
	case e.Status == protocol.StatusIncomplete && e.Reason == protocol.ReasonContentFilter:
		return "the endpoint's content filter stopped the reply before the transcript ended; " +
			"the clip can't be transcribed whole through this endpoint's filter"
	case e.Status == protocol.StatusIncomplete:
		return "the model stopped before the transcript ended"
	case e.Status == protocol.StatusFailed:
		return "the endpoint reported that the reply failed; send the request again or check the endpoint"
	}
	return "the reply isn't completed, so its text can't be taken as the whole transcript"
}

// audioFormats maps a clip's file name extension, in lower case, to the protocol's encoding of
// it, for every encoding the protocol takes.
var audioFormats = map[string]protocol.AudioFormat{
	".wav": protocol.AudioFormatWAV,
	".mp3": protocol.AudioFormatMP3,
}

// chatTranscription validates req for TranscribeChat and returns the clip's encoding, from its
// file name's extension.
func chatTranscription(req *TranscriptionRequest) (protocol.AudioFormat, error) {
	switch {
	case req.Model == "":
		return "", errors.New("request has no model")
	case req.Open == nil:
		return "", errors.New("request has no Open")
	case req.FileName == "":
		return "", errors.New("request has no file name")
	case len(req.Native) > 0:
		return "", errors.New("chat mode sends no form, so it takes no native fields")
	}
	ext := path.Ext(req.FileName)
	f, ok := audioFormats[strings.ToLower(ext)]
	if !ok {
		return "", fmt.Errorf("file name %q has no audio extension the protocol takes (.wav or .mp3)", req.FileName)
	}
	return f, nil
}

// chatRequest is TranscribeChat's request of req's clip, data, in audioFormat: one user message
// of the clip and the instruction.
func chatRequest(req *TranscriptionRequest, data []byte, audioFormat protocol.AudioFormat) *protocol.Request {
	instruction := req.Prompt
	if instruction == "" {
		instruction = TranscriptionInstruction(req.Language)
	}
	return &protocol.Request{
		Model: req.Model,
		Input: []protocol.Item{protocol.Message{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{
				protocol.InputAudio{Data: data, Format: audioFormat},
				protocol.InputText{Text: instruction},
			},
		}},
	}
}

// outputText is the text of resp's output: every OutputText part of its messages, joined in
// order.
func outputText(resp *protocol.Response) string {
	var b strings.Builder
	for _, item := range resp.Output {
		m, ok := item.(protocol.Message)
		if !ok {
			continue
		}
		for _, part := range m.Content {
			if t, ok := part.(protocol.OutputText); ok {
				b.WriteString(t.Text)
			}
		}
	}
	return b.String()
}

// transcriptionFields validates req for TranscribeRoute and returns the form fields it sends
// ahead of the file, in order: the typed fields, then Native's. The file part's name, file, is
// always set, so a Native key of that name clashes too.
func transcriptionFields(req *TranscriptionRequest) ([][2]string, error) {
	switch {
	case req.Model == "":
		return nil, errors.New("request has no model")
	case req.Open == nil:
		return nil, errors.New("request has no Open")
	case req.FileName == "":
		return nil, errors.New("request has no file name")
	}
	fields := [][2]string{{"model", req.Model}, {"response_format", "json"}}
	if req.Language != "" {
		fields = append(fields, [2]string{"language", req.Language})
	}
	if req.Prompt != "" {
		fields = append(fields, [2]string{"prompt", req.Prompt})
	}
	set := map[string]bool{"file": true}
	for _, f := range fields {
		set[f[0]] = true
	}
	// Sorted keys make the form the same for the same request, whatever the map's order.
	for _, k := range slices.Sorted(maps.Keys(req.Native)) {
		switch {
		case k == "":
			return nil, errors.New("request's native field has an empty name")
		case set[k]:
			return nil, fmt.Errorf("native field %s clashes with a field the request sets", k)
		}
		for _, v := range req.Native[k] {
			fields = append(fields, [2]string{k, v})
		}
	}
	return fields, nil
}

// writeTranscriptionForm writes fields, then file as the file part named fileName, as the
// transcription endpoint's multipart form, and closes the form.
func writeTranscriptionForm(form *multipart.Writer, fields [][2]string, fileName string, file io.Reader) error {
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	// The part's Content-Type is application/octet-stream: a type guessed from the file name
	// would depend on the host's MIME tables, and llama.cpp reads the clip's format from its
	// bytes.
	part, err := form.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	return form.Close()
}

// clipReader is a clip as the upload reads it, keeping the error a Read returned, other than
// io.EOF, so the attempt can tell the clip's failure from the request's.
type clipReader struct {
	r   io.Reader
	err error
}

func (c *clipReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err != nil && err != io.EOF {
		c.err = err
	}
	return n, err
}

// decodeTranscription decodes the body of a 2xx transcription route reply.
func decodeTranscription(body []byte) (*Transcription, error) {
	var wire transcriptionReply
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &Transcription{
		Text: wire.Text,
		Usage: TranscriptionUsage{
			InputTokens:  wire.Usage.InputTokens,
			OutputTokens: wire.Usage.OutputTokens,
			TotalTokens:  wire.Usage.TotalTokens,
		},
	}, nil
}

// transcriptionReply is the OpenAI-compatible transcription reply body under response_format
// json. llama.cpp also sends type transcript.text.done, a usage type of tokens, and
// usage.input_tokens_details.cached_tokens, which the client doesn't keep.
type transcriptionReply struct {
	Text  string `json:"text"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
