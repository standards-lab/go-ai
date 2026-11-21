//go:build integration

package integration_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

var getWeather = protocol.FunctionTool{
	Name:        "get_weather",
	Description: "Get the current temperature in a city, in degrees Celsius.",
	Parameters: jsontext.Value(`{"type":"object",
		"properties":{"city":{"type":"string","description":"The city's name."}},
		"required":["city"]}`),
}

var getTime = protocol.FunctionTool{
	Name:        "get_time",
	Description: "Get the current local time in a city.",
	Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
}

var weatherQuestion = userText("What is the weather in Paris right now?")

const weatherOutput = `{"city":"Paris","celsius":17}`

func functionCalls(resp *protocol.Response) []protocol.FunctionCall {
	var calls []protocol.FunctionCall
	for _, it := range resp.Output {
		if c, ok := it.(protocol.FunctionCall); ok {
			calls = append(calls, c)
		}
	}
	return calls
}

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

// See docs/design.md, "Implementation notes".
func TestToolChoice(t *testing.T) {
	cases := []struct {
		name    string
		choice  protocol.ToolChoice
		call    bool
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
