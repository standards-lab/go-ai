package responses_test

import (
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// foundryEncrypted returns the encrypted_content of each reasoning item in the output of reply,
// an unstreamed reply recorded in testdata/foundry. Foundry's are some thousands of characters,
// too long to pin by hand.
func foundryEncrypted(t *testing.T, reply []byte) []string {
	t.Helper()
	var r struct {
		Output []struct {
			Type             string `json:"type"`
			EncryptedContent string `json:"encrypted_content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(reply, &r); err != nil {
		t.Fatal(err)
	}
	var enc []string
	for _, it := range r.Output {
		if it.Type == "reasoning" {
			enc = append(enc, it.EncryptedContent)
		}
	}
	return enc
}

// TestDecodeFoundry decodes the Azure AI Foundry replies recorded in testdata/foundry. gpt-5-mini
// reasons on every request, and gives its reasoning item no text, no summary, and a null status,
// which decodes as empty, and the envelope holds the server's encrypted_content alone.
func TestDecodeFoundry(t *testing.T) {
	message := func(id, s string) protocol.Message {
		return protocol.Message{
			ID:      id,
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: s}},
		}
	}
	cases := []struct {
		file string
		// want is the response but its reasoning item's envelope, which holds the reply's own
		// encrypted_content.
		want *protocol.Response
	}{
		{
			// No reasoning asked for: the deployment's default effort, medium.
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{ID: "rs_0ed6014bde04d50e016afc4f6a824881958b5e07baea04f688"},
					message("msg_0ed6014bde04d50e016afc4f6bdf6081958ef937be11a8c82e", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 188, TotalTokens: 205,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 128}},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					protocol.Reasoning{ID: "rs_09fe9f588dbdce2a016afc4f6efd2481948e7b488124601fb2"},
					message("msg_09fe9f588dbdce2a016afc4f6f78a88194aa0c2a08ab7d1e55", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 76, TotalTokens: 93,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 64}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			reply := aitest.ReadTestdata(t, "foundry/"+tc.file)
			enc := foundryEncrypted(t, reply)
			if len(enc) != 1 || enc[0] == "" {
				t.Fatalf("the reply's reasoning items' encrypted_content = %d values, want one", len(enc))
			}
			rs := tc.want.Output[0].(protocol.Reasoning)
			rs.EncryptedContent = seal(t, enc[0])
			tc.want.Output[0] = rs
			resp, err := decode(string(reply))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

// TestStreamFoundry translates the Azure AI Foundry streams recorded in testdata/foundry and
// folds them to the Response DecodeResponse gives for the terminal event's response, its
// reasoning item, of null status there, completed as its output_item.done gives it. The
// unstreamed .json beside each is another reply, of other token counts and encrypted content.
//
// Foundry adds gpt-5-mini's reasoning item with an encrypted_content of its own, which the done
// item replaces, and the decoder refuses a reasoning item added with content. Until it accepts
// that one, the cases assert the error, and the fix turns them into the comparison.
func TestStreamFoundry(t *testing.T) {
	cases := []struct {
		name string
		// err is the error the stream fails with, empty when it decodes.
		err string
	}{
		{"text", `responses: stream: reasoning item "rs_05cbb2e980ddadaa016afc4f6cd71c8195885e0561d62bb4fb" is added with content`},
		{"reasoning", `responses: stream: reasoning item "rs_0b0d1234b4344875016afc4f705fa08190b296b0752833e00c" is added with content`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := string(aitest.ReadTestdata(t, "foundry/"+tc.name+".sse"))
			r := decodeStream(t, body)
			if tc.err != "" {
				if r.err == nil || r.err.Error() != tc.err {
					t.Fatalf("stream error = %v, want %q", r.err, tc.err)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			want := terminalResponse(t, body)
			aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
			assertDoneItems(t, r.events, want)
		})
	}
}
