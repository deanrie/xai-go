package xai

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

func recordingClient(t *testing.T, opts ...Option) (*Client, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	c, err := New(append([]Option{WithAPIKey("k"), WithInsecure(), WithTracerProvider(tp)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, rec
}

func attrMap(kvs []attribute.KeyValue) map[string]attribute.Value {
	m := map[string]attribute.Value{}
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value
	}
	return m
}

func TestChatStreamSpanCarriesRequestAndResponseAttributes(t *testing.T) {
	c, rec := recordingClient(t, WithAPIHost("api.example:8443"))
	ch := c.Chat("grok-4.6", WithMaxTokens(7), WithTemperature(0.5), WithStop("END"), WithReasoningEffort(ReasoningEffortLow), WithJSONObject()).Append(User(Text("hi")))
	ctx, end := c.startSpan(t.Context(), "chat.stream grok-4.6", ch.chatRequestAttrs("stream")...)
	s := &ChatStream{
		stream:   &fakeChunkStream{chunks: []*v1.GetChatCompletionChunk{chunk("s1", "a"), chunk("s1", "b")}},
		cancel:   func() {},
		span:     trace.SpanFromContext(ctx),
		end:      end,
		response: newResponse(&v1.GetChatCompletionResponse{Outputs: []*v1.CompletionOutput{{}}}, 0),
	}
	for range s.All() {
	}
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	sp := spans[0]
	if sp.Name() != "chat.stream grok-4.6" || sp.Status().Code != otelcodes.Unset {
		t.Errorf("name=%q status=%v", sp.Name(), sp.Status())
	}
	a := attrMap(sp.Attributes())
	for k, want := range map[string]string{
		"gen_ai.operation.name": "chat", "gen_ai.provider.name": "xai", "gen_ai.output.type": "json",
		"gen_ai.request.model": "grok-4.6", "server.address": "api.example", "gen_ai.request.reasoning_effort": "low",
		"gen_ai.response.id": "s1",
	} {
		if got := a[k].AsString(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if a["server.port"].AsInt64() != 8443 || a["gen_ai.request.max_tokens"].AsInt64() != 7 || a["gen_ai.request.temperature"].AsFloat64() != 0.5 {
		t.Errorf("numeric attrs: %v", a)
	}
	if got := a["gen_ai.request.stop_sequences"].AsStringSlice(); len(got) != 1 || got[0] != "END" {
		t.Errorf("stop = %v", got)
	}
}

func TestSpanRecordsErrorsAndHidesSensitiveAttributes(t *testing.T) {
	c, rec := recordingClient(t, WithoutSensitiveAttributes())
	ch := c.Chat("grok-4.6", WithTemperature(0.2), WithUser("u1")).Append(User(Text("hi")))
	_, end := c.startSpan(t.Context(), "chat.sample grok-4.6", ch.chatRequestAttrs("sample")...)
	end(wrapErr(status.Error(codes.ResourceExhausted, "slow down")))
	sp := rec.Ended()[0]
	if sp.Status().Code != otelcodes.Error || len(sp.Events()) != 1 {
		t.Errorf("status=%v events=%d", sp.Status(), len(sp.Events()))
	}
	a := attrMap(sp.Attributes())
	if _, ok := a["gen_ai.request.temperature"]; ok {
		t.Error("temperature recorded with sensitive attributes off")
	}
	if _, ok := a["user_id"]; ok {
		t.Error("user_id recorded with sensitive attributes off")
	}
	if a["gen_ai.request.model"].AsString() != "grok-4.6" {
		t.Error("model missing")
	}
}

func TestDefaultTracerIsTheGlobalNoop(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, end := c.startSpan(t.Context(), "models.list")
	end(errors.New("x")) // must not panic without a provider
}
