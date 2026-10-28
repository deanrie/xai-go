package xai

import (
	"context"
	"errors"
	"io"
	"iter"
	"strings"

	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Re-exported proto types used by the chat surface.
type (
	Message          = v1.Message
	Content          = v1.Content
	Tool             = v1.Tool
	ToolCall         = v1.ToolCall
	FinishReason     = v1.FinishReason
	MessageRole      = v1.MessageRole
	SamplingUsage    = v1.SamplingUsage
	InlineCitation   = v1.InlineCitation
	CompletionOutput = v1.CompletionOutput
)

// Chat is a conversation plus request settings. Build it with
// Client.Chat, add messages with Append, then call Sample, Stream or Defer.
// A Chat is not safe for concurrent use.
type Chat struct {
	c   *Client
	req *v1.GetCompletionsRequest
	err error
}

// ChatOption sets a request parameter.
type ChatOption func(*Chat)

// Chat starts a conversation with the given model.
func (c *Client) Chat(model string, opts ...ChatOption) *Chat {
	ch := &Chat{c: c, req: &v1.GetCompletionsRequest{Model: model}}
	for _, o := range opts {
		o(ch)
	}
	return ch
}

// fail records the first option error; every call then returns it.
func (ch *Chat) fail(err error) {
	if ch.err == nil {
		ch.err = err
	}
}

// Err returns the first invalid option given to this Chat, or nil.
func (ch *Chat) Err() error { return ch.err }

// Append adds a message to the conversation and returns the Chat for chaining.
func (ch *Chat) Append(m *Message) *Chat {
	ch.req.Messages = append(ch.req.Messages, m)
	return ch
}

// AppendResponse adds the assistant output of a previous response, including
// its tool calls, so the conversation can continue from it.
func (ch *Chat) AppendResponse(r *Response) *Chat {
	out := r.output()
	msg := &Message{
		Role:             out.GetMessage().GetRole(),
		ToolCalls:        out.GetMessage().GetToolCalls(),
		EncryptedContent: out.GetMessage().GetEncryptedContent(),
	}
	if text := out.GetMessage().GetContent(); text != "" {
		msg.Content = []*Content{{Content: &v1.Content_Text{Text: text}}}
	}
	if rc := out.GetMessage().GetReasoningContent(); rc != "" {
		msg.ReasoningContent = proto.String(rc)
	}
	return ch.Append(msg)
}

// Messages returns the conversation so far. The slice is the Chat's own;
// do not modify it.
func (ch *Chat) Messages() []*Message { return ch.req.Messages }

// Request returns a copy of the request proto that Sample and Stream would
// send with n=1, for callers who want to inspect or log it.
func (ch *Chat) Request() *v1.GetCompletionsRequest { return ch.request(1) }

func (ch *Chat) request(n int32) *v1.GetCompletionsRequest {
	req := proto.Clone(ch.req).(*v1.GetCompletionsRequest)
	req.N = proto.Int32(n)
	return req
}

// usesServerSideTools reports whether any tool is not a client function,
// which makes the server answer with several outputs (tool call, tool
// result, final answer). Same rule as the Python SDK.
func (ch *Chat) usesServerSideTools() bool {
	for _, t := range ch.req.GetTools() {
		if _, isFunction := t.GetTool().(*v1.Tool_Function); !isFunction {
			return true
		}
	}
	return false
}

// initialIndex is 0 for a single answer and -1 (all assistant outputs) when
// server-side tools are in play.
func (ch *Chat) initialIndex() int32 {
	if ch.usesServerSideTools() {
		return allOutputs
	}
	return 0
}

// Sample requests one completion and returns it.
func (ch *Chat) Sample(ctx context.Context) (*Response, error) {
	if ch.err != nil {
		return nil, ch.err
	}
	ctx, cancel := ch.c.CallContext(ctx)
	defer cancel()
	pb, err := v1.NewChatClient(ch.c.conn).GetCompletion(ctx, ch.request(1))
	if err != nil {
		return nil, wrapErr(err)
	}
	index := autoDetectMultiOutput(ch.initialIndex(), outputIndexes(pb.GetOutputs()))
	return newResponse(pb, index), nil
}

// SampleBatch requests n completions in one call and returns one Response
// per answer.
func (ch *Chat) SampleBatch(ctx context.Context, n int) ([]*Response, error) {
	if ch.err != nil {
		return nil, ch.err
	}
	if n < 1 {
		return nil, &ValidationError{Param: "n", Value: n, Accepted: []string{"1 or more"}}
	}
	ctx, cancel := ch.c.CallContext(ctx)
	defer cancel()
	pb, err := v1.NewChatClient(ch.c.conn).GetCompletion(ctx, ch.request(int32(n)))
	if err != nil {
		return nil, wrapErr(err)
	}
	out := make([]*Response, n)
	for i := range out {
		out[i] = newResponse(pb, int32(i))
	}
	return out, nil
}

// Stream requests one completion as a stream. The iterator yields the
// accumulating Response (the same object every time, growing) and the
// current Chunk. After the loop, check Err for a transport or API error;
// breaking out of the loop cancels the stream.
func (ch *Chat) Stream(ctx context.Context) *ChatStream {
	if ch.err != nil {
		return &ChatStream{err: ch.err, cancel: func() {}}
	}
	ctx, cancel := ch.c.CallContext(ctx)
	stream, err := v1.NewChatClient(ch.c.conn).GetCompletionChunk(ctx, ch.request(1))
	if err != nil {
		cancel()
		return &ChatStream{err: wrapErr(err), cancel: func() {}}
	}
	return &ChatStream{
		stream:   stream,
		cancel:   cancel,
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, ch.initialIndex()),
	}
}

// ChatStream is the result of Chat.Stream.
type ChatStream struct {
	stream   v1.Chat_GetCompletionChunkClient
	cancel   context.CancelFunc
	response *Response
	err      error
	done     bool
}

// All iterates over the chunks. Use it with range.
func (s *ChatStream) All() iter.Seq2[*Response, *Chunk] {
	return func(yield func(*Response, *Chunk) bool) {
		if s.err != nil || s.done {
			return
		}
		defer s.cancel()
		for {
			pb, err := s.stream.Recv()
			if errors.Is(err, io.EOF) {
				s.done = true
				return
			}
			if err != nil {
				s.err = wrapErr(err)
				s.done = true
				return
			}
			s.response.index = autoDetectMultiOutput(s.response.index, chunkIndexes(pb.GetOutputs()))
			s.response.processChunk(pb)
			if !yield(s.response, &Chunk{pb: pb, index: s.response.index}) {
				s.done = true
				return
			}
		}
	}
}

// Err returns the error that ended the stream, or nil after a clean end.
func (s *ChatStream) Err() error { return s.err }

// Response returns the accumulated response; complete once All has ended.
func (s *ChatStream) Response() *Response { return s.response }

// allOutputs as an index means "every assistant output", the multi-output
// mode the server uses with server-side tools.
const allOutputs int32 = -1

func outputIndexes(outs []*v1.CompletionOutput) []int32 {
	idx := make([]int32, len(outs))
	for i, o := range outs {
		idx[i] = o.GetIndex()
	}
	return idx
}

func chunkIndexes(outs []*v1.CompletionOutputChunk) []int32 {
	idx := make([]int32, len(outs))
	for i, o := range outs {
		idx[i] = o.GetIndex()
	}
	return idx
}

// autoDetectMultiOutput switches from a single answer (index 0) to all
// outputs when the server returned more than one, which happens when it
// added tools on its own. Other indexes (a batch of n) are left alone.
func autoDetectMultiOutput(index int32, indexes []int32) int32 {
	if index != 0 || len(indexes) == 0 {
		return index
	}
	for _, i := range indexes {
		if i > 0 {
			return allOutputs
		}
	}
	return index
}

// Response is one answer of a chat request. For a streamed request it grows
// as chunks arrive. Accessors read the last assistant output at the
// response's index, or across all outputs in multi-output mode.
type Response struct {
	pb    *v1.GetChatCompletionResponse
	index int32

	content   map[int32]*strings.Builder
	reasoning map[int32]*strings.Builder
	encrypted map[int32]*strings.Builder
	dirty     bool
}

func newResponse(pb *v1.GetChatCompletionResponse, index int32) *Response {
	return &Response{
		pb:        pb,
		index:     index,
		content:   map[int32]*strings.Builder{},
		reasoning: map[int32]*strings.Builder{},
		encrypted: map[int32]*strings.Builder{},
	}
}

// processChunk folds a streamed chunk into the response. Text deltas go to
// per-output builders and are written back to the proto when read, so a
// long answer is not re-concatenated on every chunk.
func (r *Response) processChunk(c *v1.GetChatCompletionChunk) {
	r.pb.Usage = c.GetUsage()
	r.pb.Created = c.GetCreated()
	r.pb.Id = c.GetId()
	r.pb.Model = c.GetModel()
	r.pb.SystemFingerprint = c.GetSystemFingerprint()
	if c.GetServiceTier() != v1.ServiceTier_SERVICE_TIER_UNSPECIFIED {
		r.pb.ServiceTier = c.GetServiceTier()
	}
	r.pb.Citations = append(r.pb.Citations, c.GetCitations()...)

	for _, oc := range c.GetOutputs() {
		for int(oc.GetIndex()) >= len(r.pb.Outputs) {
			r.pb.Outputs = append(r.pb.Outputs, &v1.CompletionOutput{})
		}
		out := r.pb.Outputs[oc.GetIndex()]
		out.Index = oc.GetIndex()
		if out.Message == nil {
			out.Message = &v1.CompletionMessage{}
		}
		out.Message.Role = oc.GetDelta().GetRole()
		out.Message.ToolCalls = append(out.Message.ToolCalls, oc.GetDelta().GetToolCalls()...)
		out.Message.Citations = append(out.Message.Citations, oc.GetDelta().GetCitations()...)
		out.FinishReason = oc.GetFinishReason()
		appendDelta(r.content, oc.GetIndex(), oc.GetDelta().GetContent(), &r.dirty)
		appendDelta(r.reasoning, oc.GetIndex(), oc.GetDelta().GetReasoningContent(), &r.dirty)
		appendDelta(r.encrypted, oc.GetIndex(), oc.GetDelta().GetEncryptedContent(), &r.dirty)
	}
}

func appendDelta(m map[int32]*strings.Builder, i int32, s string, dirty *bool) {
	if s == "" {
		return
	}
	b, ok := m[i]
	if !ok {
		b = &strings.Builder{}
		m[i] = b
	}
	b.WriteString(s)
	*dirty = true
}

// sync writes the buffered text into the proto.
func (r *Response) sync() {
	if !r.dirty {
		return
	}
	for i, b := range r.content {
		if int(i) < len(r.pb.Outputs) {
			r.pb.Outputs[i].Message.Content = b.String()
		}
	}
	for i, b := range r.reasoning {
		if int(i) < len(r.pb.Outputs) {
			r.pb.Outputs[i].Message.ReasoningContent = b.String()
		}
	}
	for i, b := range r.encrypted {
		if int(i) < len(r.pb.Outputs) {
			r.pb.Outputs[i].Message.EncryptedContent = b.String()
		}
	}
	r.dirty = false
}

// output returns the last assistant output this response exposes.
func (r *Response) output() *v1.CompletionOutput {
	r.sync()
	var last *v1.CompletionOutput
	for _, o := range r.pb.GetOutputs() {
		if o.GetMessage().GetRole() != v1.MessageRole_ROLE_ASSISTANT {
			continue
		}
		if r.index == allOutputs || o.GetIndex() == r.index {
			last = o
		}
	}
	if last == nil {
		return &v1.CompletionOutput{}
	}
	return last
}

// Proto returns the underlying response with buffered text written back.
func (r *Response) Proto() *v1.GetChatCompletionResponse { r.sync(); return r.pb }

// ID returns the response id.
func (r *Response) ID() string { return r.pb.GetId() }

// Content returns the answer text.
func (r *Response) Content() string { return r.output().GetMessage().GetContent() }

// ReasoningContent returns the model's reasoning text, for models that expose it.
func (r *Response) ReasoningContent() string { return r.output().GetMessage().GetReasoningContent() }

// EncryptedContent returns the encrypted reasoning content.
func (r *Response) EncryptedContent() string { return r.output().GetMessage().GetEncryptedContent() }

// Role returns the role of the answer.
func (r *Response) Role() MessageRole { return r.output().GetMessage().GetRole() }

// FinishReason returns why generation stopped.
func (r *Response) FinishReason() FinishReason { return r.output().GetFinishReason() }

// Usage returns token usage for the whole request.
func (r *Response) Usage() *SamplingUsage { return r.pb.GetUsage() }

// Citations returns the URL citations for the whole response.
func (r *Response) Citations() []string { return r.pb.GetCitations() }

// InlineCitations returns positioned citations of the exposed output.
func (r *Response) InlineCitations() []*InlineCitation { return r.output().GetMessage().GetCitations() }

// ToolCalls returns every tool call across all assistant outputs, in order.
func (r *Response) ToolCalls() []*ToolCall {
	r.sync()
	var calls []*ToolCall
	for _, o := range r.pb.GetOutputs() {
		if o.GetMessage().GetRole() == v1.MessageRole_ROLE_ASSISTANT {
			calls = append(calls, o.GetMessage().GetToolCalls()...)
		}
	}
	return calls
}

// Chunk is one streamed piece of a response.
type Chunk struct {
	pb    *v1.GetChatCompletionChunk
	index int32
}

// Proto returns the chunk proto.
func (c *Chunk) Proto() *v1.GetChatCompletionChunk { return c.pb }

// Content returns the text delta of this chunk, concatenated across the
// assistant outputs the parent Response exposes.
func (c *Chunk) Content() string {
	var b strings.Builder
	for _, o := range c.outputs() {
		b.WriteString(o.GetDelta().GetContent())
	}
	return b.String()
}

// ReasoningContent returns the reasoning text delta of this chunk.
func (c *Chunk) ReasoningContent() string {
	var b strings.Builder
	for _, o := range c.outputs() {
		b.WriteString(o.GetDelta().GetReasoningContent())
	}
	return b.String()
}

// ToolCalls returns the tool calls carried by this chunk.
func (c *Chunk) ToolCalls() []*ToolCall {
	var calls []*ToolCall
	for _, o := range c.outputs() {
		calls = append(calls, o.GetDelta().GetToolCalls()...)
	}
	return calls
}

func (c *Chunk) outputs() []*v1.CompletionOutputChunk {
	var outs []*v1.CompletionOutputChunk
	for _, o := range c.pb.GetOutputs() {
		if o.GetDelta().GetRole() != v1.MessageRole_ROLE_ASSISTANT {
			continue
		}
		if c.index == allOutputs || o.GetIndex() == c.index {
			outs = append(outs, o)
		}
	}
	return outs
}
