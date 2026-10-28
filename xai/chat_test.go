package xai

import (
	"errors"
	"testing"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

func chunk(id string, deltas ...string) *v1.GetChatCompletionChunk {
	c := &v1.GetChatCompletionChunk{Id: id}
	for i, d := range deltas {
		c.Outputs = append(c.Outputs, &v1.CompletionOutputChunk{
			Index: int32(i),
			Delta: &v1.Delta{Role: v1.MessageRole_ROLE_ASSISTANT, Content: d},
		})
	}
	return c
}

// Mirrors tests/sync/chat_test.py::test_streaming and the stream_batch
// read-order case from xai-sdk-python#202 / #240: every exposed output reads
// the stream regardless of which Response is read first.
func TestResponseAccumulatesChunksForEveryOutput(t *testing.T) {
	r := newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0)
	r.processChunk(chunk("r1", "Hel", "Bon"))
	r.processChunk(chunk("r1", "lo", "jour"))

	second := newResponse(r.pb, 1)
	second.content, second.reasoning, second.encrypted, second.dirty = r.content, r.reasoning, r.encrypted, true
	if got := second.Content(); got != "Bonjour" {
		t.Fatalf("second output read before the first: %q", got)
	}
	if got := r.Content(); got != "Hello" {
		t.Fatalf("first output: %q", got)
	}
	if r.ID() != "r1" {
		t.Errorf("id: %q", r.ID())
	}
}

func TestAutoDetectMultiOutput(t *testing.T) {
	cases := []struct {
		name  string
		index int32
		idx   []int32
		want  int32
	}{
		{"single output stays 0", 0, []int32{0}, 0},
		{"server added outputs switches to all", 0, []int32{0, 1, 2}, allOutputs},
		{"already all stays all", allOutputs, []int32{0, 1}, allOutputs},
		{"batch index 1 is left alone", 1, []int32{0, 1, 2}, 1},
		{"no outputs yet", 0, nil, 0},
	}
	for _, tc := range cases {
		if got := autoDetectMultiOutput(tc.index, tc.idx); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

// Mirrors the multi-output case of xai-sdk-python#243: with server-side
// tools the final assistant answer is the last assistant output, and tool
// calls come from every assistant output.
func TestResponseWithServerSideToolsExposesTheLastAssistantOutput(t *testing.T) {
	pb := &v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{
		{Index: 0, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, ToolCalls: []*v1.ToolCall{{Id: "call-1"}}}},
		{Index: 1, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_TOOL, Content: "tool output"}},
		{Index: 2, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "final answer"}},
	}}
	r := newResponse(pb, autoDetectMultiOutput(0, outputIndexes(pb.Outputs)))
	if r.index != allOutputs {
		t.Fatalf("index = %d, want allOutputs", r.index)
	}
	if r.Content() != "final answer" {
		t.Errorf("content = %q", r.Content())
	}
	if calls := r.ToolCalls(); len(calls) != 1 || calls[0].GetId() != "call-1" {
		t.Errorf("tool calls = %v", calls)
	}
	// Index 0 alone would see the tool-call output, not the answer.
	if got := newResponse(pb, 0).Content(); got != "" {
		t.Errorf("index 0 content = %q, want empty", got)
	}
}

func TestChatOptionsValidateLiterals(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ch := c.Chat("grok-4", WithReasoningEffort("hgh"))
	if !errors.Is(ch.Err(), ErrInvalidArgument) {
		t.Fatalf("bad reasoning effort not reported: %v", ch.Err())
	}
	if _, err := ch.Sample(t.Context()); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Sample on an invalid Chat: %v", err)
	}
	if s := ch.Stream(t.Context()); !errors.Is(s.Err(), ErrInvalidArgument) {
		t.Fatalf("Stream on an invalid Chat: %v", s.Err())
	}

	if _, err := ImageURL("https://x/img.png", "medium"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ImageURL with an unknown detail: %v", err)
	}
	img, err := ImageURL("https://x/img.png", "")
	if err != nil || img.GetImageUrl().GetDetail() != v1.ImageDetail_DETAIL_AUTO {
		t.Errorf("ImageURL default detail: %v %v", img, err)
	}

	good := c.Chat("grok-4", WithMaxTokens(10), WithTemperature(0.2), WithReasoningEffort(ReasoningEffortLow), WithTools(FunctionTool("f", "d", `{}`)))
	good.Append(System("be brief")).Append(User(Text("hi")))
	req := good.Request()
	if req.GetMaxTokens() != 10 || req.GetN() != 1 || len(req.GetMessages()) != 2 || req.GetReasoningEffort() != v1.ReasoningEffort_EFFORT_LOW {
		t.Errorf("request: %v", req)
	}
	if good.usesServerSideTools() {
		t.Error("a function tool is not a server-side tool")
	}
	good.req.Tools = append(good.req.Tools, &v1.Tool{Tool: &v1.Tool_WebSearch{WebSearch: &v1.WebSearch{}}})
	if !good.usesServerSideTools() || good.initialIndex() != allOutputs {
		t.Error("web_search should switch to multi-output mode")
	}
}

func TestAppendResponseCarriesToolCallsAndText(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pb := &v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{Message: &v1.CompletionMessage{
		Role: v1.MessageRole_ROLE_ASSISTANT, Content: "calling", ToolCalls: []*v1.ToolCall{{Id: "c1"}},
	}}}}
	ch := c.Chat("grok-4").AppendResponse(newResponse(pb, 0)).Append(ToolResult("c1", "42"))
	msgs := ch.Messages()
	if len(msgs) != 2 || msgs[0].GetRole() != v1.MessageRole_ROLE_ASSISTANT || len(msgs[0].GetToolCalls()) != 1 || msgs[0].GetContent()[0].GetText() != "calling" {
		t.Errorf("assistant message: %v", msgs[0])
	}
	if msgs[1].GetRole() != v1.MessageRole_ROLE_TOOL || msgs[1].GetToolCallId() != "c1" {
		t.Errorf("tool message: %v", msgs[1])
	}
}
