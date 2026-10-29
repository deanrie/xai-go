package xai

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	batch := newResponseBatch(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 2)
	r, second := batch[0], batch[1]
	r.processChunk(chunk("r1", "Hel", "Bon"))
	r.processChunk(chunk("r1", "lo", "jour"))
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
	if fi, err := ImageFile("file-1", ImageDetailHigh); err != nil || fi.GetImageUrl().GetFileId() != "file-1" || fi.GetImageUrl().GetDetail() != v1.ImageDetail_DETAIL_HIGH {
		t.Errorf("ImageFile: %v %v", fi, err)
	}
	if _, err := ImageFile("file-1", "medium"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ImageFile with an unknown detail: %v", err)
	}
	if _, err := c.Files().ListPage(context.Background(), 0, Ascending, "", WithFilesSortBy("colour")); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("files sort_by validation: %v", err)
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

func TestEmbedRejectsNoInputs(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Embed().Embed(t.Context(), "m"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Embed with no inputs: %v", err)
	}
	in, err := EmbedImage("https://x/a.png", ImageDetailHigh)
	if err != nil || in.GetImageUrl().GetDetail() != v1.ImageDetail_DETAIL_HIGH || in.GetImageUrl().GetImageUrl() != "https://x/a.png" {
		t.Errorf("EmbedImage: %v %v", in, err)
	}
	if EmbedText("hi").GetString_() != "hi" {
		t.Error("EmbedText")
	}
}

func TestFilesValidation(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Files().UploadBytes(t.Context(), "", []byte("x")); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty name: %v", err)
	}
	if _, err := c.Files().UploadBytes(t.Context(), "a", []byte("x"), WithUploadExpiresAfter(time.Minute)); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expires_after below 1h: %v", err)
	}
	if _, err := c.Files().ListPage(t.Context(), 0, ListOrder(7), ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad order: %v", err)
	}
	if _, err := c.Files().UploadFile(t.Context(), "/definitely/missing/file"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing path: %v", err)
	}
}

func TestImageAccessorsDistinguishFormatFromModeration(t *testing.T) {
	url := &Image{pb: &v1.GeneratedImage{Image: &v1.GeneratedImage_Url{Url: "https://x/i.png"}}}
	if _, err := url.Base64(); !errors.Is(err, ErrImageFormat) {
		t.Errorf("base64 on a URL image: %v", err)
	}
	withheld := &Image{pb: &v1.GeneratedImage{}}
	if _, err := withheld.URL(); !errors.Is(err, ErrImageModerated) {
		t.Errorf("withheld image: %v", err)
	}
	emptyOK := &Image{pb: &v1.GeneratedImage{RespectModeration: true}}
	if _, err := emptyOK.URL(); !errors.Is(err, ErrImageFormat) {
		t.Errorf("respecting moderation but empty: %v", err)
	}
	b64 := &Image{pb: &v1.GeneratedImage{Image: &v1.GeneratedImage_Base64{Base64: "data:image/png;base64,aGk="}}}
	if raw, err := b64.Bytes(); err != nil || string(raw) != "hi" {
		t.Errorf("Bytes: %q %v", raw, err)
	}
}

func TestVideoErrorMessage(t *testing.T) {
	err := error(&VideoError{Code: "CONTENT_POLICY", Message: "rejected"})
	if err.Error() != "xai: video generation failed: CONTENT_POLICY: rejected" {
		t.Error(err.Error())
	}
	var ve *VideoError
	if !errors.As(err, &ve) || ve.Code != "CONTENT_POLICY" {
		t.Error("errors.As")
	}
}

func TestBatchResultFromProto(t *testing.T) {
	st, _ := status.New(codes.ResourceExhausted, "quota").WithDetails()
	errRes := batchResultFromProto(&v1.BatchResult{BatchRequestId: "a", Result: &v1.BatchResult_Error{Error: st.Proto()}})
	if !errors.Is(errRes.Err, ErrResourceExhausted) || errRes.RequestID != "a" {
		t.Errorf("error result: %v", errRes.Err)
	}
	okRes := batchResultFromProto(&v1.BatchResult{BatchRequestId: "b", Result: &v1.BatchResult_Response{Response: &v1.BatchResultData{
		Response: &v1.BatchResultData_CompletionResponse{CompletionResponse: &v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{
			Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "done"},
		}}}},
	}}})
	if okRes.Err != nil || okRes.Chat == nil || okRes.Chat.Content() != "done" {
		t.Errorf("chat result: %+v", okRes)
	}
}

func TestServerSideToolConstructors(t *testing.T) {
	ws := WebSearchTool(WebSearchAllowedDomains("x.ai"), WebSearchLocation("GB", "", "London", "Europe/London"), WebSearchImageSearch(true)).GetWebSearch()
	if ws.GetAllowedDomains()[0] != "x.ai" || ws.GetUserLocation().GetCity() != "London" || ws.GetUserLocation().Region != nil || !ws.GetEnableImageSearch() {
		t.Errorf("web search: %v", ws)
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	xs := XSearchTool(XSearchHandles("xai"), XSearchDateRange(from, time.Time{})).GetXSearch()
	if xs.GetAllowedXHandles()[0] != "xai" || !xs.GetFromDate().AsTime().Equal(from) || xs.ToDate != nil {
		t.Errorf("x search: %v", xs)
	}
	cs := CollectionsSearchTool([]string{"c1"}, WithSearchLimit(5), WithSemanticRetrieval(Reranker("m", ""))).GetCollectionsSearch()
	if cs.GetCollectionIds()[0] != "c1" || cs.GetLimit() != 5 || cs.GetSemanticRetrieval().GetReranker().GetModel() != "m" {
		t.Errorf("collections search: %v", cs)
	}
	mcp := MCPTool("docs", "https://mcp.example", "Docs", MCPAllowedTools("search"), MCPAuthorization("Bearer t"), MCPHeaders(map[string]string{"X-A": "1"})).GetMcp()
	if mcp.GetServerLabel() != "docs" || mcp.GetAllowedToolNames()[0] != "search" || mcp.GetAuthorization() != "Bearer t" || mcp.GetExtraHeaders()["X-A"] != "1" {
		t.Errorf("mcp: %v", mcp)
	}
	c, _ := New(WithAPIKey("k"), WithInsecure())
	defer c.Close()
	if !c.Chat("m", WithTools(MCPTool("a", "u", "d"))).usesServerSideTools() {
		t.Error("MCP is a server-side tool")
	}
}

// fakeChunkStream feeds canned chunks to ChatStream.All.
type fakeChunkStream struct {
	v1.Chat_GetCompletionChunkClient
	chunks []*v1.GetChatCompletionChunk
	err    error
}

func (f *fakeChunkStream) Recv() (*v1.GetChatCompletionChunk, error) {
	if len(f.chunks) == 0 {
		if f.err != nil {
			return nil, f.err
		}
		return nil, io.EOF
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return c, nil
}

func TestChatStreamAccumulatesToolCallsAndSwitchesToMultiOutput(t *testing.T) {
	toolChunk := &v1.GetChatCompletionChunk{Id: "s1", Outputs: []*v1.CompletionOutputChunk{
		{Index: 0, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_ASSISTANT, ToolCalls: []*v1.ToolCall{{Id: "tc-1"}}}},
		{Index: 1, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_TOOL, Content: "tool said"}},
	}}
	textChunks := []*v1.GetChatCompletionChunk{
		{Id: "s1", Outputs: []*v1.CompletionOutputChunk{{Index: 2, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "Final "}}}},
		{Id: "s1", Outputs: []*v1.CompletionOutputChunk{{Index: 2, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "answer"}, FinishReason: v1.FinishReason_REASON_STOP}}},
	}
	s := &ChatStream{
		stream:   &fakeChunkStream{chunks: append([]*v1.GetChatCompletionChunk{toolChunk}, textChunks...)},
		cancel:   func() {},
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0),
	}
	var texts []string
	for resp, chunk := range s.All() {
		texts = append(texts, chunk.Content())
		if resp.index != allOutputs {
			t.Fatalf("index after a chunk with output 1 = %d", resp.index)
		}
	}
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	r := s.Response()
	if r.Content() != "Final answer" || strings.Join(texts, "") != "Final answer" {
		t.Errorf("content = %q, chunks = %q", r.Content(), texts)
	}
	if calls := r.ToolCalls(); len(calls) != 1 || calls[0].GetId() != "tc-1" {
		t.Errorf("tool calls = %v", calls)
	}
	if r.FinishReason() != v1.FinishReason_REASON_STOP {
		t.Errorf("finish reason = %v", r.FinishReason())
	}
}

func TestChatStreamReportsTransportErrors(t *testing.T) {
	s := &ChatStream{
		stream:   &fakeChunkStream{chunks: []*v1.GetChatCompletionChunk{chunk("x", "partial")}, err: status.Error(codes.Unavailable, "gone")},
		cancel:   func() {},
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0),
	}
	n := 0
	for range s.All() {
		n++
	}
	if n != 1 || !errors.Is(s.Err(), ErrUnavailable) {
		t.Fatalf("chunks=%d err=%v", n, s.Err())
	}
	if s.Response().Content() != "partial" {
		t.Error("partial content lost on error")
	}
	// A second range does nothing and keeps the error.
	for range s.All() {
		t.Fatal("stream yielded after it ended")
	}
}

// The stream_batch read-order case from xai-sdk-python#202 / #240: reading
// the second response first must not lose anything.
func TestChatBatchStreamSharesOneAccumulator(t *testing.T) {
	s := &ChatBatchStream{
		stream:    &fakeChunkStream{chunks: []*v1.GetChatCompletionChunk{chunk("b", "Hel", "Bon"), chunk("b", "lo", "jour")}},
		cancel:    func() {},
		responses: newResponseBatch(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{Index: 0}, {Index: 1}}}, 2),
	}
	steps := 0
	for resps, chunks := range s.All() {
		steps++
		if len(resps) != 2 || len(chunks) != 2 {
			t.Fatalf("shape: %d %d", len(resps), len(chunks))
		}
		_ = resps[1].Content() // read the second one first
	}
	if s.Err() != nil || steps != 2 {
		t.Fatalf("err=%v steps=%d", s.Err(), steps)
	}
	r := s.Responses()
	if r[1].Content() != "Bonjour" || r[0].Content() != "Hello" {
		t.Errorf("contents: %q %q", r[0].Content(), r[1].Content())
	}
}

func TestFilesPublicURLValidation(t *testing.T) {
	c, _ := New(WithAPIKey("k"), WithInsecure())
	defer c.Close()
	if _, err := c.Files().CreatePublicURL(t.Context(), "f", time.Minute); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("expires_after below 1h: %v", err)
	}
}

func TestBatchImageAndVideoRequests(t *testing.T) {
	img, err := BatchImageRequest("i1", "grok-imagine", "a cat", 2, WithImageAspectRatio("16:9"))
	if err != nil || img.GetImageRequest().GetN() != 2 || img.GetBatchRequestId() != "i1" {
		t.Errorf("image request: %v %v", img, err)
	}
	if _, err := BatchImageRequest("", "m", "p", 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("n=0: %v", err)
	}
	vid, err := BatchVideoRequest("", "grok-imagine-video", "waves", WithVideoDuration(6))
	if err != nil || vid.GetVideoRequest().GetDuration() != 6 || vid.BatchRequestId != nil {
		t.Errorf("video request: %v %v", vid, err)
	}
	if _, err := BatchVideoRequest("", "m", "p", WithVideoResolution("8k")); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad resolution: %v", err)
	}
}

func TestStorageOptionsValidation(t *testing.T) {
	if _, err := (StorageOptions{}).proto(); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty filename: %v", err)
	}
	if _, err := (StorageOptions{Filename: "a.png", ExpiresAfter: time.Minute}).proto(); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("short expiry: %v", err)
	}
	pb, err := (StorageOptions{Filename: "a.png", ExpiresAfter: 2 * time.Hour, PublicURL: true}).proto()
	if err != nil || pb.GetFilename() != "a.png" || pb.GetExpiresAfter() != 7200 || pb.GetPublicUrl() == nil || pb.GetPublicUrl().ExpiresAfter != nil {
		t.Errorf("proto: %v %v", pb, err)
	}
	c, _ := New(WithAPIKey("k"), WithInsecure())
	defer c.Close()
	if _, err := c.Images().Generate(t.Context(), "m", "p", WithImageStorage(StorageOptions{})); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("image storage without filename: %v", err)
	}
	if _, err := c.Videos().Start(t.Context(), "m", "p", WithVideoStorage(StorageOptions{})); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("video storage without filename: %v", err)
	}
}

func TestErrorStatusAndRetryable(t *testing.T) {
	err := wrapErr(status.Error(codes.ResourceExhausted, "slow down"))
	var e *Error
	if !errors.As(err, &e) || !e.Retryable() || e.Status() == nil || e.Status().Code() != codes.ResourceExhausted {
		t.Errorf("wrapped: %+v", err)
	}
	if ErrNotFound.Retryable() || ErrNotFound.Status() != nil {
		t.Error("sentinel")
	}
}

// cancelingChunkStream yields one chunk, then reports the context cancel
// the way a gRPC stream does once its context is done.
type cancelingChunkStream struct {
	v1.Chat_GetCompletionChunkClient
	cancel context.CancelFunc
	sent   bool
}

func (s *cancelingChunkStream) Recv() (*v1.GetChatCompletionChunk, error) {
	if !s.sent {
		s.sent = true
		return chunk("c", "first"), nil
	}
	s.cancel()
	return nil, status.Error(codes.Canceled, "context canceled")
}

func TestChatStreamStopsWhenTheContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &ChatStream{
		stream:   &cancelingChunkStream{cancel: cancel},
		cancel:   cancel,
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0),
	}
	n := 0
	for range s.All() {
		n++
		if n > 5 {
			t.Fatal("stream did not stop after the cancel")
		}
	}
	if n != 1 || !errors.Is(s.Err(), ErrCanceled) || ctx.Err() == nil {
		t.Fatalf("chunks=%d err=%v ctx=%v", n, s.Err(), ctx.Err())
	}
	if s.Response().Content() != "first" {
		t.Error("partial content lost on cancel")
	}
	// Breaking out of the range early also runs the cancel.
	s2 := &ChatStream{
		stream:   &fakeChunkStream{chunks: []*v1.GetChatCompletionChunk{chunk("d", "a"), chunk("d", "b"), chunk("d", "c")}},
		cancel:   func() { n = -1 },
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0),
	}
	for range s2.All() {
		break
	}
	if n != -1 || s2.Err() != nil {
		t.Errorf("early break: cancel ran=%v err=%v", n == -1, s2.Err())
	}
	for range s2.All() {
		t.Fatal("stream yielded after an early break")
	}
}

func TestFileContentAndNewTools(t *testing.T) {
	if FileRef("f1").GetFile().GetFileId() != "f1" || FileURL("https://x/a.pdf").GetFile().GetUrl() != "https://x/a.pdf" {
		t.Error("file ref / url")
	}
	fb, err := FileBytes("a.txt", []byte("hi"), "text/plain")
	if err != nil || string(fb.GetFile().GetData()) != "hi" || fb.GetFile().GetFilename() != "a.txt" || fb.GetFile().GetMimeType() != "text/plain" {
		t.Errorf("file bytes: %v %v", fb, err)
	}
	if _, err := FileBytes("a.txt", nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty bytes: %v", err)
	}
	ig, err := ImageGenerationTool(ImageGenerationEdit)
	if err != nil || ig.GetImageGeneration().GetAction() != "edit" {
		t.Errorf("image generation tool: %v %v", ig, err)
	}
	if ig, err := ImageGenerationTool(""); err != nil || ig.GetImageGeneration().Action != nil {
		t.Errorf("default action: %v %v", ig, err)
	}
	if _, err := ImageGenerationTool("paint"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad action: %v", err)
	}
	if AttachmentSearchTool(5).GetAttachmentSearch().GetLimit() != 5 || AttachmentSearchTool(0).GetAttachmentSearch().Limit != nil {
		t.Error("attachment search limit")
	}
	c, _ := New(WithAPIKey("k"), WithInsecure())
	defer c.Close()
	ch := c.Chat("m", WithTools(AttachmentSearchTool(0)), WithSearchParameters(&SearchParameters{Mode: SearchAuto, ReturnCitations: true}))
	if !ch.usesServerSideTools() || ch.Request().GetSearchParameters().GetMode() != SearchAuto {
		t.Error("attachment search is server-side; search parameters attached")
	}
}

func TestHostsFromEnvironmentAndUserAgent(t *testing.T) {
	t.Setenv("XAI_API_HOST", "api.example.test:8443")
	t.Setenv("XAI_MANAGEMENT_API_HOST", "mgmt.example.test")
	c, err := New(WithAPIKey("k"), WithManagementAPIKey("m"), WithInsecure(), WithUserAgent("my-app/1.2"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiHost != "api.example.test:8443" || c.mgmt.target != "mgmt.example.test:443" {
		t.Errorf("hosts: %q %q", c.apiHost, c.mgmt.target)
	}
	// An explicit option still wins over the environment.
	c2, _ := New(WithAPIKey("k"), WithAPIHost("other.test"), WithInsecure())
	defer c2.Close()
	if c2.apiHost != "other.test:443" {
		t.Errorf("explicit host lost: %q", c2.apiHost)
	}
}

func TestImageOutputsParseTheEnvelope(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	env := `{"__type":"image_generation_result","result":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(png) + `","image_uuid":"img-1"}`
	pb := &v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{
		{Index: 0, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, ToolCalls: []*v1.ToolCall{{Id: "c1", Type: v1.ToolCallType_TOOL_CALL_TYPE_IMAGE_GENERATION_TOOL}}}},
		{Index: 1, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_TOOL, Content: env, ToolCalls: []*v1.ToolCall{{Id: "c1", Type: v1.ToolCallType_TOOL_CALL_TYPE_IMAGE_GENERATION_TOOL, Status: v1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED}}}},
		{Index: 2, Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "Here is your image."}},
	}}
	r := newResponse(pb, allOutputs)
	outs, err := r.ImageOutputs()
	if err != nil || len(outs) != 1 {
		t.Fatalf("outputs: %d %v", len(outs), err)
	}
	o := outs[0]
	if string(o.Image) != string(png) || o.MIMEType != "image/png" || o.ImageUUID != "img-1" || o.ToolCall.GetId() != "c1" {
		t.Errorf("parsed: %+v", o)
	}
	if r.Content() != "Here is your image." {
		t.Errorf("content: %q", r.Content())
	}
	bad := newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{
		{Message: &v1.CompletionMessage{Role: v1.MessageRole_ROLE_TOOL, Content: "not json", ToolCalls: []*v1.ToolCall{{Type: v1.ToolCallType_TOOL_CALL_TYPE_IMAGE_GENERATION_TOOL, Status: v1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED}}}},
	}}, allOutputs)
	var jerr *JSONError
	if _, err := bad.ImageOutputs(); !errors.As(err, &jerr) {
		t.Errorf("malformed envelope: %v", err)
	}
}

func TestRequestOptionsForToolChoiceLogprobsPenaltiesTierInclude(t *testing.T) {
	c, _ := New(WithAPIKey("k"), WithInsecure())
	defer c.Close()
	ch := c.Chat("m",
		WithLogprobs(3),
		WithFrequencyPenalty(0.5),
		WithPresencePenalty(-0.25),
		WithToolChoice(ToolModeRequired),
		WithServiceTier(ServiceTierPriority),
		WithInclude(IncludeInlineCitations, IncludeWebSearchCallOutput),
		WithConversationID("conv-1"),
	)
	req := ch.Request()
	if !req.GetLogprobs() || req.GetTopLogprobs() != 3 {
		t.Errorf("logprobs: %v %d", req.GetLogprobs(), req.GetTopLogprobs())
	}
	if req.GetFrequencyPenalty() != 0.5 || req.GetPresencePenalty() != -0.25 {
		t.Errorf("penalties: %v %v", req.GetFrequencyPenalty(), req.GetPresencePenalty())
	}
	if req.GetToolChoice().GetMode() != v1.ToolMode_TOOL_MODE_REQUIRED {
		t.Errorf("tool choice: %v", req.GetToolChoice())
	}
	if req.GetServiceTier() != ServiceTierPriority || len(req.GetInclude()) != 2 || req.GetInclude()[0] != IncludeInlineCitations {
		t.Errorf("tier/include: %v %v", req.GetServiceTier(), req.GetInclude())
	}
	if ch.conversationID != "conv-1" {
		t.Errorf("conversation id: %q", ch.conversationID)
	}
	var seen bool
	for _, a := range ch.chatRequestAttrs("sample") {
		if string(a.Key) == "gen_ai.conversation.id" {
			seen = true
			if a.Value.AsString() != "conv-1" {
				t.Errorf("conversation attr: %v", a.Value.AsString())
			}
		}
	}
	if !seen {
		t.Error("gen_ai.conversation.id not set")
	}

	if c.Chat("m", WithRequiredTool("get_weather")).Request().GetToolChoice().GetFunctionName() != "get_weather" {
		t.Error("required tool")
	}
	if _, err := c.Chat("m", WithToolChoice("maybe")).Append(User(Text("x"))).Sample(context.Background()); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad tool mode: %v", err)
	}
	if _, err := c.Chat("m", WithLogprobs(9)).Append(User(Text("x"))).Sample(context.Background()); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("top_logprobs out of range: %v", err)
	}
}

func TestResponseCostTierFingerprintAndToolUsage(t *testing.T) {
	pb := &v1.GetChatCompletionResponse{
		SystemFingerprint: "fp-1",
		ServiceTier:       v1.ServiceTier_SERVICE_TIER_PRIORITY,
		Created:           timestamppb.New(time.Unix(1_700_000_000, 0)),
		Usage: &v1.SamplingUsage{
			CostInUsdTicks: proto.Int64(12_500_000_000),
			ServerSideToolsUsed: []v1.ServerSideTool{
				v1.ServerSideTool_SERVER_SIDE_TOOL_WEB_SEARCH,
				v1.ServerSideTool_SERVER_SIDE_TOOL_WEB_SEARCH,
				v1.ServerSideTool_SERVER_SIDE_TOOL_X_SEARCH,
			},
		},
		Outputs: []*v1.CompletionOutput{{
			Index:    0,
			Message:  &v1.CompletionMessage{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "hi"},
			Logprobs: &v1.LogProbs{Content: []*v1.LogProb{{Token: "hi", Logprob: -0.1}}},
		}},
	}
	r := newResponse(pb, allOutputs)
	if cost, ok := r.CostUSD(); !ok || cost != 1.25 {
		t.Errorf("cost: %v %v", cost, ok)
	}
	if r.ServiceTier() != ServiceTierPriority || r.SystemFingerprint() != "fp-1" || r.Created().Unix() != 1_700_000_000 {
		t.Errorf("tier/fingerprint/created: %v %q %v", r.ServiceTier(), r.SystemFingerprint(), r.Created())
	}
	usage := r.ServerSideToolUsage()
	if usage["SERVER_SIDE_TOOL_WEB_SEARCH"] != 2 || usage["SERVER_SIDE_TOOL_X_SEARCH"] != 1 || len(usage) != 2 {
		t.Errorf("tool usage: %v", usage)
	}
	if r.Logprobs().GetContent()[0].GetToken() != "hi" {
		t.Errorf("logprobs: %v", r.Logprobs())
	}

	empty := newResponse(&v1.GetChatCompletionResponse{Usage: &v1.SamplingUsage{}}, allOutputs)
	if _, ok := empty.CostUSD(); ok {
		t.Error("cost reported without the field")
	}
	if empty.ServiceTier() != ServiceTierDefault || len(empty.ServerSideToolUsage()) != 0 || empty.Logprobs() != nil {
		t.Errorf("defaults: %v %v %v", empty.ServiceTier(), empty.ServerSideToolUsage(), empty.Logprobs())
	}
}

func TestImageAndVideoAccessors(t *testing.T) {
	img := &Image{
		pb: &v1.GeneratedImage{
			Image:      &v1.GeneratedImage_Url{Url: "https://i/1.jpg"},
			FileOutput: &v1.FileOutput{PublicUrl: proto.String("https://p/1")},
		},
		resp: &ImageResponse{Model: "grok-imagine-image", Usage: &v1.SamplingUsage{CostInUsdTicks: proto.Int64(700_000_000)}},
	}
	if img.PublicURL() != "https://p/1" || img.PublicURLError() != "" || img.RespectModeration() || img.Model() != "grok-imagine-image" {
		t.Errorf("image accessors: %q %q %v %q", img.PublicURL(), img.PublicURLError(), img.RespectModeration(), img.Model())
	}
	if cost, ok := img.CostUSD(); !ok || cost != 0.07 {
		t.Errorf("image cost: %v %v", cost, ok)
	}

	vid := &Video{pb: &VideoResponse{Model: "grok-imagine-video", Video: &v1.GeneratedVideo{Url: "https://v/1.mp4", Duration: 6}}}
	if u, err := vid.URL(); err != nil || u != "https://v/1.mp4" || vid.Duration() != 6 || vid.PublicURL() != "" {
		t.Errorf("video accessors: %q %v %d", u, err, vid.Duration())
	}
	if _, ok := vid.CostUSD(); ok {
		t.Error("video cost without the field")
	}
	moderated := &Video{pb: &VideoResponse{Video: &v1.GeneratedVideo{RespectModeration: true}}}
	if _, err := moderated.URL(); !errors.Is(err, ErrVideoModerated) || !moderated.RespectModeration() {
		t.Errorf("moderated video: %v", err)
	}

	br := batchResultFromProto(&v1.BatchResult{BatchRequestId: "r1", Result: &v1.BatchResult_Response{Response: &v1.BatchResultData{Response: &v1.BatchResultData_VideoResponse{VideoResponse: vid.pb}}}})
	if br.VideoResult() == nil || br.VideoResult().Duration() != 6 || br.Images() != nil {
		t.Errorf("batch video result: %v", br)
	}
}

func TestChunkAccessors(t *testing.T) {
	pb := &v1.GetChatCompletionChunk{
		Id:        "c",
		Citations: []string{"https://a"},
		Created:   timestamppb.New(time.Unix(1_700_000_000, 0)),
		Usage:     &v1.SamplingUsage{ServerSideToolsUsed: []v1.ServerSideTool{v1.ServerSideTool_SERVER_SIDE_TOOL_WEB_SEARCH}},
		Outputs: []*v1.CompletionOutputChunk{
			{Index: 0, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_ASSISTANT, Content: "Hi", ReasoningContent: " (thinking)", Citations: []*v1.InlineCitation{{Id: "1"}}}},
			{Index: 1, Delta: &v1.Delta{Role: v1.MessageRole_ROLE_TOOL, Content: "{\"r\":1}"}},
		},
	}
	k := &Chunk{pb: pb, index: allOutputs}
	if k.Output() != "Hi (thinking)" || k.Content() != "Hi" || len(k.Outputs()) != 1 || len(k.ToolOutputs()) != 1 {
		t.Errorf("chunk text/outputs: %q %q %d %d", k.Output(), k.Content(), len(k.Outputs()), len(k.ToolOutputs()))
	}
	if len(k.Citations()) != 1 || len(k.InlineCitations()) != 1 || k.InlineCitations()[0].GetId() != "1" {
		t.Errorf("citations: %v %v", k.Citations(), k.InlineCitations())
	}
	if k.ServerSideToolUsage()["SERVER_SIDE_TOOL_WEB_SEARCH"] != 1 || k.Created().Unix() != 1_700_000_000 || k.DebugOutput() != nil {
		t.Errorf("usage/created/debug: %v %v %v", k.ServerSideToolUsage(), k.Created(), k.DebugOutput())
	}
	only1 := &Chunk{pb: pb, index: 1}
	if only1.Output() != "" || len(only1.ToolOutputs()) != 1 {
		t.Errorf("index filter: %q %d", only1.Output(), len(only1.ToolOutputs()))
	}
}
