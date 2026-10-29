package xai

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// tracerName is the instrumentation scope reported on every span.
const tracerName = "github.com/deanrie/xai-go/xai"

// WithTracerProvider sets the OpenTelemetry provider the client records
// spans with. Without it the client uses the global provider
// (otel.GetTracerProvider), which is a no-op until an application
// installs one, so tracing costs nothing unless it is turned on. Span and
// attribute names follow the Python SDK (`chat.sample <model>`,
// `gen_ai.*`), so dashboards built for one SDK read the other.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tracerProvider = tp }
}

// WithoutSensitiveAttributes leaves request parameters, stop sequences and
// user ids off the spans; only operation, model, server and usage counts
// stay. The Python SDK reads XAI_SDK_DISABLE_TRACING_SENSITIVE_ATTRIBUTES
// for the same switch.
func WithoutSensitiveAttributes() Option {
	return func(c *config) { c.noSensitiveAttributes = true }
}

func (c *Client) tracer() trace.Tracer {
	if c.tracerProvider == nil {
		return otel.GetTracerProvider().Tracer(tracerName, trace.WithInstrumentationVersion(Version))
	}
	return c.tracerProvider.Tracer(tracerName, trace.WithInstrumentationVersion(Version))
}

// startSpan opens a client span. The returned end function records err as
// the span status.
func (c *Client) startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	ctx, span := c.tracer().Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// serverAttrs name the API host the way the Python SDK does.
func (c *Client) serverAttrs() []attribute.KeyValue {
	host, port := c.apiHost, 443
	if i := strings.LastIndex(host, ":"); i >= 0 {
		if p, ok := parsePort(host[i+1:]); ok {
			port = p
			host = host[:i]
		}
	}
	return []attribute.KeyValue{attribute.String("server.address", host), attribute.Int("server.port", port)}
}

func parsePort(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
		n = n*10 + int(ch-'0')
	}
	return n, true
}

// chatRequestAttrs mirrors xai_sdk.chat._make_span_request_attributes.
func (ch *Chat) chatRequestAttrs(operation string) []attribute.KeyValue {
	req := ch.req
	attrs := append([]attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.provider.name", "xai"),
		attribute.String("gen_ai.output.type", outputType(req)),
		attribute.String("gen_ai.request.model", req.GetModel()),
		attribute.String("xai.operation", operation),
	}, ch.c.serverAttrs()...)
	if ch.c.noSensitiveAttributes {
		return attrs
	}
	attrs = append(attrs,
		attribute.Float64("gen_ai.request.frequency_penalty", float64(req.GetFrequencyPenalty())),
		attribute.Float64("gen_ai.request.presence_penalty", float64(req.GetPresencePenalty())),
		attribute.Bool("gen_ai.request.store_messages", req.GetStoreMessages()),
		attribute.Bool("gen_ai.request.use_encrypted_content", req.GetUseEncryptedContent()),
		attribute.Bool("gen_ai.request.logprobs", req.GetLogprobs()),
	)
	temperature := 1.0
	if req.Temperature != nil {
		temperature = float64(req.GetTemperature())
	}
	attrs = append(attrs, attribute.Float64("gen_ai.request.temperature", temperature))
	parallel := true
	if req.ParallelToolCalls != nil {
		parallel = req.GetParallelToolCalls()
	}
	attrs = append(attrs, attribute.Bool("gen_ai.request.parallel_tool_calls", parallel))
	if req.TopP != nil {
		attrs = append(attrs, attribute.Float64("gen_ai.request.top_p", float64(req.GetTopP())))
	}
	if req.MaxTokens != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.max_tokens", int(req.GetMaxTokens())))
	}
	if req.Seed != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.seed", int(req.GetSeed())))
	}
	if len(req.GetStop()) > 0 {
		attrs = append(attrs, attribute.StringSlice("gen_ai.request.stop_sequences", req.GetStop()))
	}
	if req.ReasoningEffort != nil {
		attrs = append(attrs, attribute.String("gen_ai.request.reasoning_effort", strings.ToLower(strings.TrimPrefix(req.GetReasoningEffort().String(), "EFFORT_"))))
	}
	if req.GetUser() != "" {
		attrs = append(attrs, attribute.String("user_id", req.GetUser()))
	}
	if req.TopLogprobs != nil {
		attrs = append(attrs, attribute.Int("gen_ai.request.top_logprobs", int(req.GetTopLogprobs())))
	}
	switch {
	case ch.conversationID != "":
		attrs = append(attrs, attribute.String("gen_ai.conversation.id", ch.conversationID))
	case req.PreviousResponseId != nil:
		attrs = append(attrs, attribute.String("gen_ai.conversation.id", req.GetPreviousResponseId()))
	}
	return attrs
}

func outputType(req *v1.GetCompletionsRequest) string {
	switch req.GetResponseFormat().GetFormatType() {
	case v1.FormatType_FORMAT_TYPE_JSON_OBJECT, v1.FormatType_FORMAT_TYPE_JSON_SCHEMA:
		return "json"
	default:
		return "text"
	}
}

// chatResponseAttrs mirrors xai_sdk.chat._make_span_response_attributes.
func chatResponseAttrs(pb *v1.GetChatCompletionResponse) []attribute.KeyValue {
	if pb == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.response.id", pb.GetId()),
		attribute.String("gen_ai.response.model", pb.GetModel()),
		attribute.String("gen_ai.response.system_fingerprint", pb.GetSystemFingerprint()),
	}
	reasons := make([]string, 0, len(pb.GetOutputs()))
	for _, o := range pb.GetOutputs() {
		reasons = append(reasons, strings.ToLower(strings.TrimPrefix(o.GetFinishReason().String(), "REASON_")))
	}
	attrs = append(attrs, attribute.StringSlice("gen_ai.response.finish_reasons", reasons))
	if u := pb.GetUsage(); u != nil {
		attrs = append(attrs,
			attribute.Int("gen_ai.usage.input_tokens", int(u.GetPromptTokens())),
			attribute.Int("gen_ai.usage.output_tokens", int(u.GetCompletionTokens())),
			attribute.Int("gen_ai.usage.total_tokens", int(u.GetTotalTokens())),
			attribute.Int("gen_ai.usage.prompt_text_tokens", int(u.GetPromptTextTokens())),
			attribute.Int("gen_ai.usage.prompt_image_tokens", int(u.GetPromptImageTokens())),
			attribute.Int("gen_ai.usage.reasoning_tokens", int(u.GetReasoningTokens())),
			attribute.Int("gen_ai.usage.cached_prompt_text_tokens", int(u.GetCachedPromptTextTokens())),
		)
	}
	return attrs
}
