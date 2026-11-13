# messages testdata

Anthropic Messages replies recorded from llama.cpp b11669, its router on gb10-2 serving set A, each
unstreamed (`.json`), and some streamed too (`.sse`, the body as it arrived) for the same request,
sent with `temperature` 0 and `seed` 7 to `/v1/messages?autoload=false`. The decode tests compare
what `DecodeResponse` gives for each `.json` with the response it should be. The stream tests
translate each `.sse` and fold its events, and compare the result with what `DecodeResponse` gives
for its `.json` given the stream's reply id and tool_use ids, as the two were separate requests,
and with each call's arguments the stream's text, which the `.json`'s input holds as the same JSON
written compact: the router parses a call's arguments for an unstreamed reply, and streams the
model's own text.

Each streamed request sent `"stream":true`, as `EncodeStreamRequest` does, and each unstreamed
one `"stream":false`. The router's prompt cache makes a repeated request's reply differ at times,
so a `.sse` is paired with the `.json` it matches: `text.sse` and `length.sse` with replies
recorded before them, which they match token for token, and `tool_use.sse` with the `.json`
recorded beside it.

- `text.json`: gpt-oss-120b with `system` "Answer with the names alone.", asked "Name three
  French cities, comma-separated, nothing else." with `max_tokens` 256; a thinking block, its
  signature empty, then a text block, stop_reason `end_turn`, and usage with
  `cache_read_input_tokens`. `text.sse` streams the same reply: message_start, the thinking
  block's start and deltas, the text block's, then the thinking block's empty signature_delta and
  every block's stop at the end, then message_delta and message_stop.
- `tool_use.json`: gpt-oss-120b asked "What is the weather in Paris right now?" with the tool
  `get_weather` (its description "Get the current temperature in a city, in degrees Celsius." and
  an object schema of a required string `city`), `tool_choice` `{"type":"any"}`, and `max_tokens`
  1024; a thinking block, its signature empty, then a tool_use block of input `{"city":"Paris"}`,
  stop_reason `tool_use`, sent with `stream` false as the format sends it. `tool_use.sse` streams
  the same reply, its tool_use block's start of no input member and its input_json deltas the
  model's text, `{\n  "city": "Paris"\n}`.
- `length.json`: Gemma 4 E4B asked "Count from 1 to 20, separated by spaces." with `max_tokens` 8;
  a text block, stop_reason `max_tokens`. `length.sse` streams the same text, its usage split
  otherwise between input_tokens and cache_read_input_tokens, their sum the same.
- `conversation.json`: Gemma 4 E4B with `system` as one text block, "Answer in one word.", and the
  messages a system message of "Use capital letters only.", the user's "What is the capital of
  France?", the assistant's "PARIS", and the user's "And of Italy?", with `max_tokens` 64; a
  text block, "ROME", stop_reason `end_turn`, showing the router takes a system message where it
  stands.
Neither error body is recorded: the tests write Anthropic's, `{"type":"error","error":{"type",
"message"}}`, inline in the shape the Messages API documents, and the router's OpenAI-style one,
`{"error":{"code","message","type"}}`, inline in the shape it answers with.

## What the router departs from

llama.cpp's router, b11669, rebuilds a Messages body as a Chat Completions one and keeps only the
members it reads: messages, tools, tool_choice, stop_sequences, max_tokens, temperature, top_p,
top_k, stream, chat_template_kwargs, thinking, and metadata. It drops output_config, so reasoning
effort's level and structured output's schema reach no model, and a tool's strict flag; it
treats tool_choice none as no choice, the tools still live, and a named tool as any. A Native
member reaches its model only if the router reads it, as temperature, top_p, top_k, and
chat_template_kwargs are; Anthropic's API reads every member it defines.

Its streamed reply stops every content block only at the end of the reply, after the last block
began, where Anthropic stops each before the next begins, and its error frame mid-stream is an
OpenAI-style `{"code","message","type"}` body. It passes a system message through where it
stands, and makes an assistant message of a run of reasoning, which the reasoning alone leaves
without content.

The replies recorded from vLLM 0.33.0 are in `vllm/`, which has its own README.
