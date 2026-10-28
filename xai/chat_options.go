package xai

import (
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Message constructors. Each returns a Message ready for Chat.Append.

// System returns a system message with one text part.
func System(text string) *Message { return textMessage(v1.MessageRole_ROLE_SYSTEM, text) }

// User returns a user message. Parts are text or images; use Text and
// ImageURL to build them.
func User(parts ...*Content) *Message {
	return &Message{Role: v1.MessageRole_ROLE_USER, Content: parts}
}

// Assistant returns an assistant message with one text part.
func Assistant(text string) *Message { return textMessage(v1.MessageRole_ROLE_ASSISTANT, text) }

// ToolResult returns a tool message carrying the result of a tool call.
func ToolResult(toolCallID, result string) *Message {
	m := textMessage(v1.MessageRole_ROLE_TOOL, result)
	m.ToolCallId = proto.String(toolCallID)
	return m
}

func textMessage(role MessageRole, text string) *Message {
	return &Message{Role: role, Content: []*Content{Text(text)}}
}

// Text returns a text content part.
func Text(s string) *Content { return &Content{Content: &v1.Content_Text{Text: s}} }

// ImageDetail selects the resolution at which an image is read.
type ImageDetail string

// Accepted ImageDetail values.
const (
	ImageDetailAuto ImageDetail = "auto"
	ImageDetailLow  ImageDetail = "low"
	ImageDetailHigh ImageDetail = "high"
)

var imageDetailValues = map[ImageDetail]v1.ImageDetail{
	ImageDetailAuto: v1.ImageDetail_DETAIL_AUTO,
	ImageDetailLow:  v1.ImageDetail_DETAIL_LOW,
	ImageDetailHigh: v1.ImageDetail_DETAIL_HIGH,
}

// ImageURL returns an image content part from a URL or data URL. An empty
// detail means auto; any other value outside the accepted set is a
// ValidationError rather than a silent fallback.
func ImageURL(url string, detail ImageDetail) (*Content, error) {
	if detail == "" {
		detail = ImageDetailAuto
	}
	d, ok := imageDetailValues[detail]
	if !ok {
		return nil, &ValidationError{Param: "detail", Value: string(detail), Accepted: []string{"auto", "low", "high"}}
	}
	return &Content{Content: &v1.Content_ImageUrl{ImageUrl: &v1.ImageUrlContent{
		Source: &v1.ImageUrlContent_ImageUrl{ImageUrl: url},
		Detail: d,
	}}}, nil
}

// Request parameters.

// WithMaxTokens caps the number of generated tokens.
func WithMaxTokens(n int32) ChatOption { return func(c *Chat) { c.req.MaxTokens = proto.Int32(n) } }

// WithTemperature sets the sampling temperature.
func WithTemperature(t float32) ChatOption {
	return func(c *Chat) { c.req.Temperature = proto.Float32(t) }
}

// WithTopP sets nucleus sampling.
func WithTopP(p float32) ChatOption { return func(c *Chat) { c.req.TopP = proto.Float32(p) } }

// WithSeed makes sampling reproducible where the model supports it.
func WithSeed(seed int32) ChatOption { return func(c *Chat) { c.req.Seed = proto.Int32(seed) } }

// WithStop sets stop sequences.
func WithStop(stop ...string) ChatOption { return func(c *Chat) { c.req.Stop = stop } }

// WithUser sets the legacy end-user identifier.
func WithUser(user string) ChatOption { return func(c *Chat) { c.req.User = user } }

// WithSafetyIdentifier sets the stable, opaque end-user identifier used for
// policy attribution. Send a hash, never an email or display name.
func WithSafetyIdentifier(id string) ChatOption {
	return func(c *Chat) { c.req.SafetyIdentifier = proto.String(id) }
}

// WithTools declares the tools the model may call.
func WithTools(tools ...*Tool) ChatOption { return func(c *Chat) { c.req.Tools = tools } }

// WithParallelToolCalls allows or forbids several tool calls in one turn.
func WithParallelToolCalls(allowed bool) ChatOption {
	return func(c *Chat) { c.req.ParallelToolCalls = proto.Bool(allowed) }
}

// WithPreviousResponseID continues a stored conversation server-side.
func WithPreviousResponseID(id string) ChatOption {
	return func(c *Chat) { c.req.PreviousResponseId = proto.String(id) }
}

// WithStoreMessages asks the server to store the conversation.
func WithStoreMessages(store bool) ChatOption {
	return func(c *Chat) { c.req.StoreMessages = store }
}

// WithEncryptedContent asks for encrypted reasoning content in responses.
func WithEncryptedContent(use bool) ChatOption {
	return func(c *Chat) { c.req.UseEncryptedContent = use }
}

// WithMaxTurns caps agentic turns when server-side tools are used.
func WithMaxTurns(n int32) ChatOption { return func(c *Chat) { c.req.MaxTurns = proto.Int32(n) } }

// WithJSONSchema asks for a response that conforms to the given JSON schema.
func WithJSONSchema(schema string) ChatOption {
	return func(c *Chat) {
		c.req.ResponseFormat = &v1.ResponseFormat{FormatType: v1.FormatType_FORMAT_TYPE_JSON_SCHEMA, Schema: proto.String(schema)}
	}
}

// WithJSONObject asks for any JSON object as the response.
func WithJSONObject() ChatOption {
	return func(c *Chat) {
		c.req.ResponseFormat = &v1.ResponseFormat{FormatType: v1.FormatType_FORMAT_TYPE_JSON_OBJECT}
	}
}

// ReasoningEffort controls how much a reasoning model thinks.
type ReasoningEffort string

// Accepted ReasoningEffort values.
const (
	ReasoningEffortNone   ReasoningEffort = "none"
	ReasoningEffortLow    ReasoningEffort = "low"
	ReasoningEffortMedium ReasoningEffort = "medium"
	ReasoningEffortHigh   ReasoningEffort = "high"
	ReasoningEffortXHigh  ReasoningEffort = "xhigh"
)

var reasoningEffortValues = map[ReasoningEffort]v1.ReasoningEffort{
	ReasoningEffortNone:   v1.ReasoningEffort_EFFORT_NONE,
	ReasoningEffortLow:    v1.ReasoningEffort_EFFORT_LOW,
	ReasoningEffortMedium: v1.ReasoningEffort_EFFORT_MEDIUM,
	ReasoningEffortHigh:   v1.ReasoningEffort_EFFORT_HIGH,
	ReasoningEffortXHigh:  v1.ReasoningEffort_EFFORT_XHIGH,
}

// WithReasoningEffort sets the reasoning effort. An unknown value makes
// every call on the Chat return a ValidationError.
func WithReasoningEffort(e ReasoningEffort) ChatOption {
	return func(c *Chat) {
		v, ok := reasoningEffortValues[e]
		if !ok {
			c.fail(&ValidationError{Param: "reasoning_effort", Value: string(e), Accepted: []string{"none", "low", "medium", "high", "xhigh"}})
			return
		}
		c.req.ReasoningEffort = v.Enum()
	}
}

// FunctionTool declares a client-side function the model may call. The
// parameters are a JSON schema string.
func FunctionTool(name, description, parametersJSON string) *Tool {
	return &Tool{Tool: &v1.Tool_Function{Function: &v1.Function{
		Name:        name,
		Description: description,
		Parameters:  parametersJSON,
	}}}
}

// Server-side tools. The API runs these itself and the response then covers
// every output; see Response.

// WebSearchOption configures WebSearchTool.
type WebSearchOption func(*v1.WebSearch)

// WebSearchAllowedDomains restricts results to these domains.
func WebSearchAllowedDomains(domains ...string) WebSearchOption {
	return func(w *v1.WebSearch) { w.AllowedDomains = domains }
}

// WebSearchExcludedDomains drops results from these domains.
func WebSearchExcludedDomains(domains ...string) WebSearchOption {
	return func(w *v1.WebSearch) { w.ExcludedDomains = domains }
}

// WebSearchImageUnderstanding lets the model read images on pages.
func WebSearchImageUnderstanding(on bool) WebSearchOption {
	return func(w *v1.WebSearch) { w.EnableImageUnderstanding = proto.Bool(on) }
}

// WebSearchImageSearch lets the model search for images.
func WebSearchImageSearch(on bool) WebSearchOption {
	return func(w *v1.WebSearch) { w.EnableImageSearch = proto.Bool(on) }
}

// WebSearchLocation biases results toward a location. Empty strings are
// left unset.
func WebSearchLocation(country, region, city, timezone string) WebSearchOption {
	return func(w *v1.WebSearch) {
		loc := &v1.WebSearchUserLocation{}
		if country != "" {
			loc.Country = proto.String(country)
		}
		if region != "" {
			loc.Region = proto.String(region)
		}
		if city != "" {
			loc.City = proto.String(city)
		}
		if timezone != "" {
			loc.Timezone = proto.String(timezone)
		}
		w.UserLocation = loc
	}
}

// WebSearchTool lets the model search the web.
func WebSearchTool(opts ...WebSearchOption) *Tool {
	w := &v1.WebSearch{}
	for _, o := range opts {
		o(w)
	}
	return &Tool{Tool: &v1.Tool_WebSearch{WebSearch: w}}
}

// XSearchOption configures XSearchTool.
type XSearchOption func(*v1.XSearch)

// XSearchHandles restricts results to posts from these handles.
func XSearchHandles(handles ...string) XSearchOption {
	return func(x *v1.XSearch) { x.AllowedXHandles = handles }
}

// XSearchExcludedHandles drops posts from these handles.
func XSearchExcludedHandles(handles ...string) XSearchOption {
	return func(x *v1.XSearch) { x.ExcludedXHandles = handles }
}

// XSearchDateRange limits posts to [from, to]. A zero time leaves that
// bound open.
func XSearchDateRange(from, to time.Time) XSearchOption {
	return func(x *v1.XSearch) {
		if !from.IsZero() {
			x.FromDate = timestamppb.New(from)
		}
		if !to.IsZero() {
			x.ToDate = timestamppb.New(to)
		}
	}
}

// XSearchImageUnderstanding lets the model read images in posts.
func XSearchImageUnderstanding(on bool) XSearchOption {
	return func(x *v1.XSearch) { x.EnableImageUnderstanding = proto.Bool(on) }
}

// XSearchVideoUnderstanding lets the model watch videos in posts.
func XSearchVideoUnderstanding(on bool) XSearchOption {
	return func(x *v1.XSearch) { x.EnableVideoUnderstanding = proto.Bool(on) }
}

// XSearchTool lets the model search X.
func XSearchTool(opts ...XSearchOption) *Tool {
	x := &v1.XSearch{}
	for _, o := range opts {
		o(x)
	}
	return &Tool{Tool: &v1.Tool_XSearch{XSearch: x}}
}

// CodeExecutionTool lets the model run code in a sandbox.
func CodeExecutionTool() *Tool {
	return &Tool{Tool: &v1.Tool_CodeExecution{CodeExecution: &v1.CodeExecution{}}}
}

// CollectionsSearchTool lets the model search the given collections. The
// SearchOptions limit, instructions and retrieval mode apply; the API
// rejects anything else.
func CollectionsSearchTool(collectionIDs []string, opts ...SearchOption) *Tool {
	req := &v1.SearchRequest{}
	for _, o := range opts {
		o(req)
	}
	cs := &v1.CollectionsSearch{CollectionIds: collectionIDs, Limit: req.Limit, Instructions: req.Instructions}
	switch m := req.GetRetrievalMode().(type) {
	case *v1.SearchRequest_HybridRetrieval:
		cs.RetrievalMode = &v1.CollectionsSearch_HybridRetrieval{HybridRetrieval: m.HybridRetrieval}
	case *v1.SearchRequest_SemanticRetrieval:
		cs.RetrievalMode = &v1.CollectionsSearch_SemanticRetrieval{SemanticRetrieval: m.SemanticRetrieval}
	case *v1.SearchRequest_KeywordRetrieval:
		cs.RetrievalMode = &v1.CollectionsSearch_KeywordRetrieval{KeywordRetrieval: m.KeywordRetrieval}
	case nil:
	}
	return &Tool{Tool: &v1.Tool_CollectionsSearch{CollectionsSearch: cs}}
}

// MCPOption configures MCPTool.
type MCPOption func(*v1.MCP)

// MCPAllowedTools restricts the model to these tool names on the server.
func MCPAllowedTools(names ...string) MCPOption {
	return func(m *v1.MCP) { m.AllowedToolNames = names }
}

// MCPAuthorization sets the Authorization header value sent to the server.
func MCPAuthorization(value string) MCPOption {
	return func(m *v1.MCP) { m.Authorization = proto.String(value) }
}

// MCPHeaders adds extra HTTP headers for the server.
func MCPHeaders(headers map[string]string) MCPOption {
	return func(m *v1.MCP) { m.ExtraHeaders = headers }
}

// MCPTool lets the model call a remote MCP server.
func MCPTool(label, url, description string, opts ...MCPOption) *Tool {
	m := &v1.MCP{ServerLabel: label, ServerUrl: url, ServerDescription: description}
	for _, o := range opts {
		o(m)
	}
	return &Tool{Tool: &v1.Tool_Mcp{Mcp: m}}
}
