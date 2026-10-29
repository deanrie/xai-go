// Package conformance checks the Go client against the gRPC test server that
// ships with the official Python SDK (xai-sdk-python/tests/server.py), so the
// two SDKs are held to the same wire behaviour.
//
// The tests are skipped unless XAI_SDK_PYTHON_DIR points at a checkout of
// xai-sdk-python and uv is on PATH:
//
//	XAI_SDK_PYTHON_DIR=~/src/xai-sdk-python go test ./tests/conformance/
package conformance

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"

	"github.com/deanrie/xai-go/xai"
)

// Constants from tests/server.py.
const (
	testAPIKey           = "123"
	testManagementAPIKey = "456"
	serverStartup        = 60 * time.Second
)

var serverAddr, managementAddr, flakyAddr, flakyAddr2, slowAddr string

func TestMain(m *testing.M) {
	dir := os.Getenv("XAI_SDK_PYTHON_DIR")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "conformance: XAI_SDK_PYTHON_DIR not set, skipping")
		os.Exit(0)
	}
	if _, err := exec.LookPath("uv"); err != nil {
		fmt.Fprintln(os.Stderr, "conformance: uv not on PATH, skipping")
		os.Exit(0)
	}

	cmd := exec.Command("uv", "run", "--project", dir, "python", "run_server.py")
	cmd.Env = append(os.Environ(), "XAI_SDK_PYTHON_DIR="+dir)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	if err := cmd.Start(); err != nil {
		panic(err)
	}

	portCh := make(chan map[string]string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		ports := map[string]string{}
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if ok && (strings.HasSuffix(k, "port") || strings.HasSuffix(k, "port_2")) {
				ports[k] = v
			}
			if len(ports) == 5 {
				portCh <- ports
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	select {
	case ports := <-portCh:
		serverAddr = "127.0.0.1:" + ports["port"]
		managementAddr = "127.0.0.1:" + ports["mgmt_port"]
		flakyAddr = "127.0.0.1:" + ports["flaky_port"]
		flakyAddr2 = "127.0.0.1:" + ports["flaky_port_2"]
		slowAddr = "127.0.0.1:" + ports["slow_port"]
	case <-time.After(serverStartup):
		_ = cmd.Process.Kill()
		panic("conformance: test server did not report its ports")
	}

	code := m.Run()

	_ = stdin.Close() // run_server.py exits when stdin closes
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
	}
	os.Exit(code)
}

func newClient(t *testing.T, opts ...xai.Option) *xai.Client {
	t.Helper()
	c, err := xai.New(append([]xai.Option{
		xai.WithAPIKey(testAPIKey),
		xai.WithAPIHost(serverAddr),
		xai.WithManagementAPIKey(testManagementAPIKey),
		xai.WithManagementAPIHost(managementAddr),
		xai.WithInsecure(),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// tests/sync/auth_test.py: a wrong key is UNAUTHENTICATED.
func TestAuthRejectsWrongKey(t *testing.T) {
	c := newClient(t, xai.WithAPIKey("wrong"))
	_, err := c.Tokenize().TokenizeText(t.Context(), "hi", "grok-4")
	if !errors.Is(err, xai.ErrUnauthenticated) {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
	var e *xai.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *xai.Error, got %T", err)
	}
}

// tests/sync/tokenize_test.py::test_tokenize_text
func TestTokenizeText(t *testing.T) {
	c := newClient(t)
	tokens, err := c.Tokenize().TokenizeText(t.Context(), "Hello world!", "grok-4")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tok := range tokens {
		got = append(got, tok.GetStringToken())
	}
	if strings.Join(got, "") != "Hello world!" || len(tokens) != 3 {
		t.Fatalf("tokens = %v", got)
	}
}

// tests/sync/models_test.py: the default library is empty and an unknown
// model is NOT_FOUND.
func TestModels(t *testing.T) {
	c := newClient(t)
	models, err := c.Models().ListLanguageModels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("default library should be empty, got %d", len(models))
	}
	_, err = c.Models().GetLanguageModel(t.Context(), "nope")
	if !errors.Is(err, xai.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// tests/sync/chat_test.py::test_sample
func TestChatSample(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4").Append(xai.User(xai.Text("hi")))
	resp, err := chat.Sample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.ID(), "test-completion-") {
		t.Errorf("id = %q", resp.ID())
	}
	if resp.Content() == "" {
		t.Error("empty content")
	}
	if u := resp.Usage(); u.GetTotalTokens() != 15 {
		t.Errorf("usage = %v", u)
	}
}

// tests/sync/chat_test.py::test_sample_batch: n responses, each at its own
// index.
func TestChatSampleBatch(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4").Append(xai.User(xai.Text("hi")))
	resps, err := chat.SampleBatch(t.Context(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resps) != 3 {
		t.Fatalf("got %d responses", len(resps))
	}
	for i, r := range resps {
		if r.Content() == "" {
			t.Errorf("response %d empty", i)
		}
	}
}

// tests/sync/chat_test.py::test_stream: four chunks concatenate to the full
// text and the accumulated response matches.
func TestChatStream(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4").Append(xai.User(xai.Text("hi")))
	stream := chat.Stream(t.Context())
	var chunks []string
	var last *xai.Response
	for resp, chunk := range stream.All() {
		chunks = append(chunks, chunk.Content())
		last = resp
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	const want = "Hello, this is a test response!"
	if got := strings.Join(chunks, ""); got != want {
		t.Errorf("chunks = %q", got)
	}
	if last == nil || last.Content() != want {
		t.Errorf("accumulated = %q", last.Content())
	}
	if last.ID() != "test-chunk-456" {
		t.Errorf("id = %q", last.ID())
	}
}

// tests/sync/chat_test.py::test_function_tool_call: a function tool yields a
// tool call named after the declared function.
func TestChatFunctionToolCall(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4",
		xai.WithTools(xai.FunctionTool("get_weather", "Weather", `{"type":"object"}`)),
	).Append(xai.User(xai.Text("weather in London?")))
	resp, err := chat.Sample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.ToolCalls()
	if len(calls) != 1 || calls[0].GetFunction().GetName() != "get_weather" {
		t.Fatalf("tool calls = %v", calls)
	}
	if resp.FinishReason().String() != "REASON_TOOL_CALLS" {
		t.Errorf("finish reason = %v", resp.FinishReason())
	}

	// Feed the result back the way the Python tests do.
	chat.AppendResponse(resp).Append(xai.ToolResult(calls[0].GetId(), `{"temp":12}`))
	if n := len(chat.Messages()); n != 3 {
		t.Errorf("messages = %d", n)
	}
}

// Server-side tools switch the response to multi-output mode: the content is
// the final assistant answer, tool calls are gathered from every output.
func TestChatServerSideToolsStream(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4", xai.WithTools(xai.WebSearchTool())).
		Append(xai.User(xai.Text("weather in London?")))
	stream := chat.Stream(t.Context())
	var last *xai.Response
	for resp := range stream.All() {
		last = resp
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if last == nil {
		t.Fatal("no chunks")
	}
	if len(last.ToolCalls()) == 0 {
		t.Error("expected server-side tool calls")
	}
	if last.Content() == "" {
		t.Error("expected final assistant content")
	}
}

// A caller deadline is honoured and surfaces as ErrDeadlineExceeded.
func TestDeadline(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	_, err := c.Tokenize().TokenizeText(ctx, "hi", "grok-4")
	if !errors.Is(err, xai.ErrDeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

// tests/sync/auth_test.py::test_get_api_key_info
func TestAuthGetAPIKeyInfo(t *testing.T) {
	c := newClient(t)
	key, err := c.Auth().GetAPIKeyInfo(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if key.GetRedactedApiKey() != "1**" || key.GetName() != "api key 0" {
		t.Errorf("key = %v", key)
	}
}

// tests/sync/chat_test.py::test_defer: the server answers PENDING twice,
// then DONE.
func TestChatDefer(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4").Append(xai.User(xai.Text("hi")))
	resp, err := chat.Defer(t.Context(), xai.PollOptions{Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content() == "" {
		t.Error("empty deferred content")
	}
	batch, err := chat.DeferBatch(t.Context(), 2, xai.PollOptions{Interval: 10 * time.Millisecond})
	if err != nil || len(batch) != 2 {
		t.Fatalf("DeferBatch: %d %v", len(batch), err)
	}
	// Too short a timeout surfaces as ErrPollTimeout, not as a hang.
	_, err = chat.Defer(t.Context(), xai.PollOptions{Interval: 50 * time.Millisecond, Timeout: 20 * time.Millisecond})
	if !errors.Is(err, xai.ErrPollTimeout) {
		t.Errorf("want ErrPollTimeout, got %v", err)
	}
}

// tests/sync/chat_test.py::test_stored_completion: stored with
// store_messages, retrievable by id, NOT_FOUND after delete.
func TestStoredCompletion(t *testing.T) {
	c := newClient(t)
	resp, err := c.Chat("grok-4", xai.WithStoreMessages(true)).Append(xai.User(xai.Text("hi"))).Sample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.GetStoredCompletion(t.Context(), resp.ID())
	if err != nil || got.ID() != resp.ID() {
		t.Fatalf("GetStoredCompletion: %v %v", got, err)
	}
	if err := c.DeleteStoredCompletion(t.Context(), resp.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetStoredCompletion(t.Context(), resp.ID()); !errors.Is(err, xai.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

// tests/sync/files_test.py::test_upload: the test server implements
// UploadFile only, and echoes the name and the total size.
func TestFilesUpload(t *testing.T) {
	c := newClient(t)
	data := bytes.Repeat([]byte("x"), 4<<20) // two stream chunks
	var calls int
	var last int64
	file, err := c.Files().UploadBytes(t.Context(), "big.bin", data, xai.WithUploadProgress(func(up, total int64) {
		calls++
		last = up
		if total != int64(len(data)) {
			t.Errorf("total = %d", total)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if file.GetFilename() != "big.bin" || file.GetSize() != int64(len(data)) || file.GetId() == "" {
		t.Errorf("file = %v", file)
	}
	if calls != 2 || last != int64(len(data)) {
		t.Errorf("progress calls=%d last=%d", calls, last)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err = c.Files().UploadFile(t.Context(), path)
	if err != nil || file.GetFilename() != "note.txt" || file.GetSize() != 5 {
		t.Errorf("UploadFile: %v %v", file, err)
	}
}

// tests/sync/image_test.py: URL mode returns the server's URL, base64 mode
// returns a data URL that decodes to the JPEG the server serves.
func TestImagesGenerate(t *testing.T) {
	c := newClient(t)
	img, err := c.Images().Generate(t.Context(), "grok-imagine", "a cat")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := img.URL(); err != nil || !strings.HasPrefix(u, "http://localhost:") {
		t.Errorf("URL: %q %v", u, err)
	}
	if _, err := img.Base64(); !errors.Is(err, xai.ErrImageFormat) {
		t.Errorf("Base64 in URL mode: %v", err)
	}

	batch, err := c.Images().GenerateBatch(t.Context(), "grok-imagine", "a cat", 3, xai.WithImageFormat(xai.ImageFormatBase64), xai.WithImageAspectRatio("16:9"))
	if err != nil || len(batch) != 3 {
		t.Fatalf("GenerateBatch: %d %v", len(batch), err)
	}
	raw, err := batch[0].Bytes()
	if err != nil || len(raw) < 4 || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Errorf("Bytes: len=%d err=%v (want a JPEG)", len(raw), err)
	}
	if _, err := batch[0].URL(); !errors.Is(err, xai.ErrImageFormat) {
		t.Errorf("URL in base64 mode: %v", err)
	}
	if _, err := c.Images().GenerateBatch(t.Context(), "m", "p", 11); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("n=11: %v", err)
	}
	if _, err := c.Images().Generate(t.Context(), "m", "p", xai.WithImageAspectRatio("5:4")); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("bad aspect ratio: %v", err)
	}
}

// tests/sync/video_test.py: two PENDING polls, then DONE with the server's
// URL and the requested duration; extend goes through the same path.
func TestVideosGenerate(t *testing.T) {
	c := newClient(t)
	pollOpts := xai.PollOptions{Interval: 10 * time.Millisecond}
	var polls int
	id, err := c.Videos().Start(t.Context(), "grok-imagine-video", "a cat", xai.WithVideoDuration(8), xai.WithVideoAspectRatio("16:9"))
	if err != nil || id == "" {
		t.Fatalf("Start: %q %v", id, err)
	}
	resp, err := c.Videos().Wait(t.Context(), id, pollOpts, func(int32) { polls++ })
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetVideo().GetUrl() != "https://example.com/foo.mp4" || resp.GetVideo().GetDuration() != 8 {
		t.Errorf("video = %v", resp.GetVideo())
	}
	if polls < 3 {
		t.Errorf("polls = %d, want the two PENDING answers plus DONE", polls)
	}

	ext, err := c.Videos().Extend(t.Context(), "grok-imagine-video", "more", xai.VideoURL(resp.GetVideo().GetUrl()), 4, pollOpts)
	if err != nil || ext.GetVideo().GetUrl() == "" {
		t.Errorf("Extend: %v %v", ext, err)
	}
	if _, err := c.Videos().Get(t.Context(), "missing"); !errors.Is(err, xai.ErrNotFound) {
		t.Errorf("Get unknown id: %v", err)
	}
	if _, err := c.Videos().Start(t.Context(), "m", "p", xai.WithVideoResolution("4k")); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("bad resolution: %v", err)
	}
}

// tests/sync/batch_test.py: create, add 12 chat requests (two result
// pages at the server's default limit of 10), list, cancel, read results
// including the cancelled-as-error ones.
func TestBatches(t *testing.T) {
	c := newClient(t)
	batch, err := c.Batches().Create(t.Context(), "go-conformance", "")
	if err != nil || batch.GetBatchId() == "" {
		t.Fatalf("Create: %v %v", batch, err)
	}
	chat := c.Chat("grok-4").Append(xai.User(xai.Text("hi")))
	var reqs []*xai.BatchRequest
	for i := range 12 {
		r, err := xai.BatchChatRequest(fmt.Sprintf("req-%d", i), chat)
		if err != nil {
			t.Fatal(err)
		}
		reqs = append(reqs, r)
	}
	if err := c.Batches().Add(t.Context(), batch.GetBatchId(), reqs...); err != nil {
		t.Fatal(err)
	}
	got, err := c.Batches().Get(t.Context(), batch.GetBatchId())
	if err != nil || got.GetState().GetNumRequests() != 12 || got.GetState().GetNumPending() != 12 {
		t.Fatalf("Get: %v %v", got.GetState(), err)
	}

	var ids []string
	for m, err := range c.Batches().Requests(t.Context(), batch.GetBatchId()) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.GetBatchRequestId())
	}
	if len(ids) != 12 || ids[0] != "req-0" {
		t.Errorf("request metadata = %v", ids)
	}

	var seen int
	for b, err := range c.Batches().All(t.Context()) {
		if err != nil {
			t.Fatal(err)
		}
		if b.GetBatchId() == batch.GetBatchId() {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("batch listed %d times", seen)
	}

	var results, errs int
	for r, err := range c.Batches().Results(t.Context(), batch.GetBatchId()) {
		if err != nil {
			t.Fatal(err)
		}
		results++
		if r.Err != nil {
			errs++
		} else if r.Chat == nil || r.Chat.Content() == "" {
			t.Errorf("result %s without chat content", r.RequestID)
		}
	}
	if results != 12 || errs != 0 {
		t.Errorf("results=%d errs=%d", results, errs)
	}

	if err := c.Batches().Add(t.Context(), "missing", reqs[0]); !errors.Is(err, xai.ErrNotFound) {
		t.Errorf("Add to unknown batch: %v", err)
	}
	if err := c.Batches().Add(t.Context(), batch.GetBatchId()); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("Add with no requests: %v", err)
	}
}

// tests/sync/documents_test.py: the server keys matches on the query and
// honours the limit.
func TestDocumentsSearch(t *testing.T) {
	c := newClient(t)
	matches, err := c.Documents().Search(t.Context(), "test-query-1", []string{"col-1"})
	if err != nil || len(matches) != 2 || matches[0].GetChunkId() != "test-chunk-1" {
		t.Fatalf("Search: %v %v", matches, err)
	}
	matches, err = c.Documents().Search(t.Context(), "anything", []string{"col-1"}, xai.WithSearchLimit(1), xai.WithHybridRetrieval(xai.Reranker("", "prefer recent"), 3))
	if err != nil || len(matches) != 1 {
		t.Fatalf("Search with limit: %v %v", matches, err)
	}
	if _, err := c.Documents().Search(t.Context(), "q", nil); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("no collections: %v", err)
	}
}

// tests/sync/chat_test.py::test_compact: the server drops all but two
// messages; the chat is left with one encrypted user message.
func TestChatCompact(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4").Append(xai.System("s")).Append(xai.User(xai.Text("a"))).Append(xai.Assistant("b")).Append(xai.User(xai.Text("c")))
	resp, err := chat.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetDroppedMessageCount() != 2 || resp.GetEncryptedContent() == "" {
		t.Errorf("compact = %v", resp)
	}
	msgs := chat.Messages()
	if len(msgs) != 1 || msgs[0].GetEncryptedContent() != resp.GetEncryptedContent() || len(msgs[0].GetContent()) != 0 {
		t.Errorf("messages after compact = %v", msgs)
	}
}

// tests/sync/collections_test.py: the management server shares the store
// with the files server; "test-processing-<s>" names index for s seconds,
// "test-failed" fails.
func TestCollections(t *testing.T) {
	c := newClient(t)
	cs := c.Collections()
	col, err := cs.Create(t.Context(), "go-conformance", xai.WithCollectionDescription("docs"), xai.WithCollectionChunking(xai.ChunkByChars(500, 50)), xai.WithCollectionMetric(xai.MetricCosine))
	if err != nil || col.GetCollectionId() == "" {
		t.Fatalf("Create: %v %v", col, err)
	}
	got, err := cs.Get(t.Context(), col.GetCollectionId())
	if err != nil || got.GetCollectionName() != "go-conformance" {
		t.Fatalf("Get: %v %v", got, err)
	}
	var seen int
	for m, err := range cs.All(t.Context(), xai.Ascending) {
		if err != nil {
			t.Fatal(err)
		}
		if m.GetCollectionId() == col.GetCollectionId() {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("collection listed %d times", seen)
	}

	fast, err := c.Files().UploadBytes(t.Context(), "notes.txt", []byte("hello collections"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.AddDocument(t.Context(), col.GetCollectionId(), fast.GetId(), map[string]string{"source": "test"}); err != nil {
		t.Fatal(err)
	}
	doc, err := cs.GetDocument(t.Context(), col.GetCollectionId(), fast.GetId())
	if err != nil || doc.GetStatus() != xai.DocumentProcessed || doc.GetFields()["source"] != "test" {
		t.Errorf("GetDocument: %v %v", doc, err)
	}

	slow, err := c.Files().UploadBytes(t.Context(), "test-processing-0.3", []byte("slow"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.AddDocument(t.Context(), col.GetCollectionId(), slow.GetId(), nil); err != nil {
		t.Fatal(err)
	}
	if d, err := cs.GetDocument(t.Context(), col.GetCollectionId(), slow.GetId()); err != nil || d.GetStatus() != xai.DocumentProcessing {
		t.Errorf("slow document right after add: %v %v", d.GetStatus(), err)
	}
	waited, err := cs.WaitForDocument(t.Context(), col.GetCollectionId(), slow.GetId(), xai.PollOptions{Interval: 50 * time.Millisecond, Timeout: 5 * time.Second})
	if err != nil || waited.GetStatus() != xai.DocumentProcessed {
		t.Errorf("WaitForDocument: %v %v", waited.GetStatus(), err)
	}

	failed, err := c.Files().UploadBytes(t.Context(), "test-failed", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cs.AddDocument(t.Context(), col.GetCollectionId(), failed.GetId(), nil); err != nil {
		t.Fatal(err)
	}
	var indexErr *xai.DocumentIndexingError
	if _, err := cs.WaitForDocument(t.Context(), col.GetCollectionId(), failed.GetId(), xai.PollOptions{Interval: 10 * time.Millisecond}); !errors.As(err, &indexErr) || indexErr.Message != "Processing failed" {
		t.Errorf("failed document: %v", err)
	}

	var docs int
	for _, err := range cs.Documents(t.Context(), col.GetCollectionId(), xai.Descending) {
		if err != nil {
			t.Fatal(err)
		}
		docs++
	}
	if docs != 3 {
		t.Errorf("documents = %d", docs)
	}
	if err := cs.RemoveDocument(t.Context(), col.GetCollectionId(), failed.GetId()); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.GetDocument(t.Context(), col.GetCollectionId(), failed.GetId()); !errors.Is(err, xai.ErrNotFound) {
		t.Errorf("after remove: %v", err)
	}
	if err := cs.Delete(t.Context(), col.GetCollectionId()); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Get(t.Context(), col.GetCollectionId()); !errors.Is(err, xai.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}

	col2, err := cs.Create(t.Context(), "go-conformance-2")
	if err != nil {
		t.Fatal(err)
	}
	upd, err := cs.Update(t.Context(), col2.GetCollectionId(), xai.RenameCollection("renamed"), xai.UpdateDescription("d2"), xai.AddFieldDefinition(&xai.FieldDefinition{Key: "lang"}))
	if err != nil || upd.GetCollectionName() != "renamed" {
		t.Errorf("Update: %v %v", upd, err)
	}
	if _, err := cs.Update(t.Context(), col2.GetCollectionId()); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("Update without options: %v", err)
	}
	one, err := cs.UploadDocument(t.Context(), col2.GetCollectionId(), "test-processing-0.2", []byte("body"), map[string]string{"lang": "en"}, xai.PollOptions{Interval: 50 * time.Millisecond})
	if err != nil || one.GetStatus() != xai.DocumentProcessed || one.GetFields()["lang"] != "en" {
		t.Errorf("UploadDocument: %v %v", one, err)
	}
	name := "renamed.txt"
	updated, err := cs.UpdateDocument(t.Context(), col2.GetCollectionId(), one.GetFileMetadata().GetFileId(), xai.DocumentUpdate{Name: &name, Fields: map[string]string{"lang": "de"}})
	if err != nil || updated.GetFields()["lang"] != "de" {
		t.Errorf("UpdateDocument: %v %v", updated, err)
	}
	if _, err := cs.UpdateDocument(t.Context(), col2.GetCollectionId(), one.GetFileMetadata().GetFileId(), xai.DocumentUpdate{}); !errors.Is(err, xai.ErrInvalidArgument) {
		t.Errorf("empty update: %v", err)
	}
	batch, err := cs.BatchGetDocuments(t.Context(), col2.GetCollectionId(), one.GetFileMetadata().GetFileId(), "missing")
	if err != nil || len(batch) != 1 {
		t.Errorf("BatchGetDocuments: %d %v", len(batch), err)
	}

	// The management key is checked separately from the API key.
	wrong := newClient(t, xai.WithManagementAPIKey("nope"))
	if _, err := wrong.Collections().Get(t.Context(), "x"); !errors.Is(err, xai.ErrUnauthenticated) {
		t.Errorf("wrong management key: %v", err)
	}
	none, err := xai.New(xai.WithAPIKey(testAPIKey), xai.WithAPIHost(serverAddr), xai.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer none.Close()
	if _, err := none.Collections().Get(t.Context(), "x"); !errors.Is(err, xai.ErrMissingManagementAPIKey) {
		t.Errorf("no management key: %v", err)
	}
}

// tests/sync/chat_test.py::test_stream_batch: the server streams every
// output's chunks in one stream; each Response ends with the full text.
func TestChatStreamBatch(t *testing.T) {
	c := newClient(t)
	stream := c.Chat("grok-4").Append(xai.User(xai.Text("hi"))).StreamBatch(t.Context(), 2)
	var steps int
	for _, chunks := range stream.All() {
		steps++
		if len(chunks) != 2 {
			t.Fatalf("chunks = %d", len(chunks))
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if steps == 0 {
		t.Fatal("no chunks")
	}
	for i, r := range stream.Responses() {
		if r.Content() == "" {
			t.Errorf("response %d empty", i)
		}
	}
}

// tests/sync/client_test.py::test_retries: the first call fails with
// UNAVAILABLE; the default service config retries it, and a client that
// disables retries sees the failure.
func TestRetriesOnUnavailable(t *testing.T) {
	c := newClient(t, xai.WithAPIHost(flakyAddr))
	key, err := c.Auth().GetAPIKeyInfo(t.Context())
	if err != nil || key.GetRedactedApiKey() != "1**" {
		t.Fatalf("with retries: %v %v", key, err)
	}

	noRetry := newClient(t, xai.WithAPIHost(flakyAddr2), xai.WithDialOptions(grpc.WithDisableRetry()))
	if _, err := noRetry.Auth().GetAPIKeyInfo(t.Context()); !errors.Is(err, xai.ErrUnavailable) {
		t.Fatalf("without retries: want ErrUnavailable, got %v", err)
	}
}

// tests/sync/files_test.py::test_batch_upload: results come back in input
// order, a missing path fails on its own without stopping the rest.
func TestFilesUploadAll(t *testing.T) {
	c := newClient(t)
	dir := t.TempDir()
	var paths []string
	for i := range 5 {
		p := filepath.Join(dir, fmt.Sprintf("f%d.txt", i))
		if err := os.WriteFile(p, []byte(strings.Repeat("x", i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	paths = append(paths, filepath.Join(dir, "missing.txt"))
	results := c.Files().UploadAll(t.Context(), paths, 2)
	if len(results) != 6 {
		t.Fatalf("results = %d", len(results))
	}
	for i := range 5 {
		if results[i].Err != nil || results[i].File.GetSize() != int64(i+1) || results[i].Path != paths[i] {
			t.Errorf("result %d: %+v", i, results[i])
		}
	}
	if !errors.Is(results[5].Err, os.ErrNotExist) {
		t.Errorf("missing file: %v", results[5].Err)
	}
}

// tests/sync/chat_test.py::test_parse: a JSON schema request gets a JSON
// answer that decodes into the caller's value; a mismatched value is a
// *xai.JSONError that still carries the Response.
func TestChatSampleJSON(t *testing.T) {
	c := newClient(t)
	var weather struct {
		City        string  `json:"city"`
		Units       string  `json:"units"`
		Temperature float64 `json:"temperature"`
	}
	resp, err := c.Chat("grok-4").Append(xai.User(xai.Text("weather?"))).SampleJSON(t.Context(), `{"type":"object"}`, &weather)
	if err != nil || weather.City != "London" || weather.Temperature != 20 {
		t.Fatalf("SampleJSON: %+v %v", weather, err)
	}
	if resp.Proto().GetOutputs()[0].GetFinishReason().String() != "REASON_STOP" {
		t.Errorf("finish reason: %v", resp.FinishReason())
	}
	var wrong []string
	resp, err = c.Chat("grok-4").Append(xai.User(xai.Text("weather?"))).SampleJSON(t.Context(), `{"type":"array"}`, &wrong)
	var jerr *xai.JSONError
	if !errors.As(err, &jerr) || resp == nil || !strings.HasPrefix(jerr.Content, "{") {
		t.Fatalf("want JSONError with the response, got %v %v", resp, err)
	}
}

// tests/sync/chat_test.py::test_sample_with_server_side_tools: three
// outputs (assistant tool call, tool result, final assistant text); the
// Response reads the final text and gathers the tool calls.
func TestChatSampleServerSideTools(t *testing.T) {
	c := newClient(t)
	resp, err := c.Chat("grok-4", xai.WithTools(xai.WebSearchTool())).Append(xai.User(xai.Text("weather?"))).Sample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content() != "I am searching." {
		t.Errorf("content = %q", resp.Content())
	}
	if calls := resp.ToolCalls(); len(calls) == 0 || calls[0].GetFunction().GetName() != "web_search" {
		t.Errorf("tool calls = %v", calls)
	}
	if n := len(resp.Proto().GetOutputs()); n != 3 {
		t.Errorf("outputs = %d", n)
	}
}

// tests/sync/chat_test.py::test_encrypted_content: reasoning and encrypted
// content come back when asked for, and AppendResponse carries them on.
func TestChatEncryptedContent(t *testing.T) {
	c := newClient(t)
	chat := c.Chat("grok-4", xai.WithEncryptedContent(true)).Append(xai.User(xai.Text("hi")))
	resp, err := chat.Sample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resp.ReasoningContent() != "test reasoning content" || resp.EncryptedContent() != "test encrypted content" {
		t.Errorf("reasoning=%q encrypted=%q", resp.ReasoningContent(), resp.EncryptedContent())
	}
	chat.AppendResponse(resp)
	msgs := chat.Messages()
	if last := msgs[len(msgs)-1]; last.GetEncryptedContent() != "test encrypted content" || last.GetReasoningContent() != "test reasoning content" {
		t.Errorf("appended message = %v", last)
	}
}

// tests/sync/client_test.py::test_timeout_unary_unary: the client timeout
// bounds a call the server answers too late.
func TestClientTimeoutUnary(t *testing.T) {
	c := newClient(t, xai.WithAPIHost(slowAddr), xai.WithTimeout(500*time.Millisecond))
	start := time.Now()
	_, err := c.Chat("grok-4.6").Append(xai.User(xai.Text("hi"))).Sample(context.Background())
	if !errors.Is(err, xai.ErrDeadlineExceeded) {
		t.Fatalf("want ErrDeadlineExceeded, got %v", err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("took %v, the 500 ms timeout did not bound the call", d)
	}
}

// tests/sync/client_test.py::test_timeout_unary_stream: a stream that never
// starts within the timeout ends with the deadline error and no chunks.
func TestClientTimeoutStream(t *testing.T) {
	c := newClient(t, xai.WithAPIHost(slowAddr), xai.WithTimeout(500*time.Millisecond))
	stream := c.Chat("grok-4.6").Append(xai.User(xai.Text("hi"))).Stream(context.Background())
	n := 0
	for range stream.All() {
		n++
	}
	if !errors.Is(stream.Err(), xai.ErrDeadlineExceeded) {
		t.Fatalf("want ErrDeadlineExceeded, got %v", stream.Err())
	}
	if n != 0 || stream.Response() == nil {
		t.Errorf("chunks=%d response=%v", n, stream.Response())
	}
}

// A caller deadline on ctx wins over the client timeout.
func TestCallerDeadlineOnStream(t *testing.T) {
	c := newClient(t, xai.WithAPIHost(slowAddr))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	stream := c.Chat("grok-4.6").Append(xai.User(xai.Text("hi"))).Stream(ctx)
	for range stream.All() {
	}
	if !errors.Is(stream.Err(), xai.ErrDeadlineExceeded) {
		t.Fatalf("want ErrDeadlineExceeded, got %v", stream.Err())
	}
}

// Spans are emitted end to end: one per call, named like the Python SDK,
// with the response usage from the fake server attached.
func TestTelemetrySpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	c := newClient(t, xai.WithTracerProvider(tp))

	if _, err := c.Chat("grok-4.6").Append(xai.User(xai.Text("hi"))).Sample(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tokenize().TokenizeText(t.Context(), "hi", "grok-4.6"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Models().GetLanguageModel(t.Context(), "nope"); err == nil {
		t.Fatal("expected not found")
	}
	stream := c.Chat("grok-4.6").Append(xai.User(xai.Text("hi"))).Stream(t.Context())
	for range stream.All() {
	}

	names := map[string]bool{}
	var sample, failed sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		names[s.Name()] = true
		switch s.Name() {
		case "chat.sample grok-4.6":
			sample = s
		case "get_language_model nope":
			failed = s
		}
	}
	for _, want := range []string{"chat.sample grok-4.6", "tokenize_text grok-4.6", "get_language_model nope", "chat.stream grok-4.6"} {
		if !names[want] {
			t.Errorf("missing span %q in %v", want, names)
		}
	}
	if sample == nil || failed == nil {
		t.Fatal("spans not found")
	}
	for _, kv := range sample.Attributes() {
		if kv.Key == "gen_ai.usage.total_tokens" && kv.Value.AsInt64() != 15 {
			t.Errorf("total_tokens = %d", kv.Value.AsInt64())
		}
	}
	if failed.Status().Code != otelcodes.Error {
		t.Errorf("failed call span status = %v", failed.Status())
	}
}

// tests/sync/chat_test.py::test_image_generation_tool: the server answers an
// image_generation tool with a completed call and its envelope in one
// ROLE_TOOL chunk, then the assistant text.
func TestChatImageGenerationTool(t *testing.T) {
	c := newClient(t)
	tool, err := xai.ImageGenerationTool(xai.ImageGenerationAuto)
	if err != nil {
		t.Fatal(err)
	}
	stream := c.Chat("grok-4.6", xai.WithTools(tool)).Append(xai.User(xai.Text("draw a cat"))).Stream(t.Context())
	for range stream.All() {
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	resp := stream.Response()
	outs, err := resp.ImageOutputs()
	if err != nil || len(outs) != 1 {
		t.Fatalf("image outputs: %d %v", len(outs), err)
	}
	if len(outs[0].Image) < 4 || outs[0].Image[0] != 0xFF || outs[0].Image[1] != 0xD8 || outs[0].MIMEType != "image/jpeg" || outs[0].ImageUUID == "" {
		t.Errorf("image: mime=%s uuid=%q len=%d", outs[0].MIMEType, outs[0].ImageUUID, len(outs[0].Image))
	}
	if resp.Content() != "Here is your image." {
		t.Errorf("content = %q", resp.Content())
	}
}
