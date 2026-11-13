//go:build integration

package integration_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// getWeather is a function tool the weather question calls.
var getWeather = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the current temperature in a city, in degrees Celsius.",
	Parameters: jsontext.Value(`{"type":"object",
		"properties":{"city":{"type":"string","description":"The city's name."}},
		"required":["city"]}`),
}

// getTime is a second function tool, for a named choice to pass over.
var getTime = protocol.FunctionTool{
	Name:        "get_time",
	Description: "Get the current local time in a city.",
	Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
}

// weatherQuestion is the user message that asks for the weather in Paris.
var weatherQuestion = userText("What is the weather in Paris right now?")

// weatherOutput is get_weather's answer for Paris, which an answer that uses it carries as 17.
const weatherOutput = `{"city":"Paris","celsius":17}`

// functionCalls returns resp's function calls, in output order.
func functionCalls(resp *protocol.Response) []protocol.FunctionCall {
	var calls []protocol.FunctionCall
	for _, it := range resp.Output {
		if c, ok := it.(protocol.FunctionCall); ok {
			calls = append(calls, c)
		}
	}
	return calls
}

// weatherCall returns the one function_call to get_weather in resp, failing t unless resp is
// completed and holds one, completed, with a call id and arguments that are a JSON object.
func weatherCall(t *testing.T, resp *protocol.Response) protocol.FunctionCall {
	t.Helper()
	if resp.Status != protocol.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed", resp.Status, resp.IncompleteDetails)
	}
	calls := functionCalls(resp)
	if len(calls) != 1 {
		t.Fatalf("Output has %d function calls, want 1: %#v", len(calls), resp.Output)
	}
	c := calls[0]
	if c.Name != getWeather.Name || c.CallID == "" || c.Status != protocol.StatusCompleted {
		t.Fatalf("FunctionCall = %#v, want a completed call of %s with a call id", c, getWeather.Name)
	}
	if args := jsontext.Value(c.Arguments); args.Kind() != jsontext.KindBeginObject || !args.IsValid() {
		t.Fatalf("FunctionCall arguments %q aren't a JSON object", c.Arguments)
	}
	return c
}

// answerWithWeather sends req again with resp's output, as it came, and the weather call's
// output, under the auto choice, and wants a completed answer that uses the tool's temperature.
// generate is how the scenario sends a request, unstreamed or streamed and folded.
func answerWithWeather(t *testing.T, req *protocol.Request, resp *protocol.Response, call protocol.FunctionCall,
	generate func(*protocol.Request) *protocol.Response) {
	t.Helper()
	req.Input = append(req.Input, resp.Output...)
	req.Input = append(req.Input, protocol.FunctionCallOutput{CallID: call.CallID, Output: weatherOutput})
	req.ToolChoice = protocol.ToolChoiceAuto
	answer := completedText(t, generate(req))
	if !strings.Contains(answer, "17") {
		t.Errorf("the answer %q doesn't use the tool's temperature, 17", answer)
	}
}

// generator returns a generate for answerWithWeather that sends unstreamed with c, failing t on
// an error.
func generator(t *testing.T, c *ai.Client) func(*protocol.Request) *protocol.Response {
	return func(req *protocol.Request) *protocol.Response {
		t.Helper()
		resp, err := c.Generate(requestContext(t), req)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		return resp
	}
}

// TestFunctionCallRoundTrip asks the weather question under the required choice, wants one call
// of get_weather, sends its output back after the response's own output items, and wants an
// answer that uses it.
func TestFunctionCallRoundTrip(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		generate := generator(t, c)
		req := &protocol.Request{
			Model:           model,
			Input:           []protocol.Item{weatherQuestion},
			Tools:           []protocol.FunctionTool{getWeather},
			ToolChoice:      protocol.ToolChoiceRequired,
			MaxOutputTokens: agentBound,
		}
		resp := generate(req)
		answerWithWeather(t, req, resp, weatherCall(t, resp), generate)
	})
}

// TestToolChoice asks a question that needs no tool, with get_time and get_weather declared, and
// wants each choice honored: a named function, and an allowed_tools of get_weather alone under
// the required mode, each make one call of get_weather though the answer needs none, and none
// makes no call, the answer a message.
//
// The allowed_tools choice runs on Messages alone, whose format sends it as the one tool it
// allows under tool_choice any. Chat Completions and Open Responses send their own allowed_tools
// forms, as the unit tier proves, and vLLM 0.33.0 honors neither: it refuses Chat Completions'
// with a 400 and answers Open Responses' as auto, with no call.
func TestToolChoice(t *testing.T) {
	cases := []struct {
		name   string
		choice protocol.ToolChoice
		call   bool
		// formats are the formats the case runs through; nil runs it through every format.
		formats []testFormat
	}{
		{name: "named function", choice: protocol.FunctionToolChoice{Name: getWeather.Name}, call: true},
		{name: "allowed_tools required", choice: protocol.AllowedToolChoice{
			Tools: []protocol.FunctionToolChoice{{Name: getWeather.Name}},
			Mode:  protocol.ToolChoiceRequired,
		}, call: true, formats: []testFormat{messagesFormat}},
		{name: "none", choice: protocol.ToolChoiceNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.formats
			if fs == nil {
				fs = formats
			}
			eachFormatOf(t, agent, fs, func(t *testing.T, c *ai.Client, model string) {
				resp, err := c.Generate(requestContext(t), &protocol.Request{
					Model:           model,
					Input:           []protocol.Item{userText("What is 2 + 2? Answer with the number alone.")},
					Tools:           []protocol.FunctionTool{getTime, getWeather},
					ToolChoice:      tc.choice,
					MaxOutputTokens: agentBound,
				})
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if tc.call {
					weatherCall(t, resp)
					return
				}
				if calls := functionCalls(resp); len(calls) != 0 {
					t.Fatalf("Output has function calls %#v under none, want none", calls)
				}
				completedText(t, resp)
			})
		})
	}
}
