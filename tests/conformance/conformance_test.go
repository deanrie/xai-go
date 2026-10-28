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

	"github.com/deanrie/xai-go/xai"
)

// Constants from tests/server.py.
const (
	testAPIKey    = "123"
	serverStartup = 60 * time.Second
)

var serverAddr string

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

	portCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if p, ok := strings.CutPrefix(sc.Text(), "port="); ok {
				portCh <- p
				break
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
	}()

	select {
	case port := <-portCh:
		serverAddr = "127.0.0.1:" + port
	case <-time.After(serverStartup):
		_ = cmd.Process.Kill()
		panic("conformance: test server did not report a port")
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
