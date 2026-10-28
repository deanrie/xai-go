package xai

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestNewRequiresAnAPIKey(t *testing.T) {
	t.Setenv(envAPIKey, "")
	if _, err := New(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("New() without a key: got %v, want ErrMissingAPIKey", err)
	}
	t.Setenv(envAPIKey, "   ")
	if _, err := New(); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("New() with a blank key: got %v, want ErrMissingAPIKey", err)
	}
}

func TestNewReadsTheKeyFromTheEnvironment(t *testing.T) {
	t.Setenv(envAPIKey, "env-key")
	c, err := New(WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := c.md.Get("authorization"); len(got) != 1 || got[0] != "Bearer env-key" {
		t.Fatalf("authorization metadata = %v", got)
	}
}

func TestCallContextCarriesMetadataAndTheDefaultTimeout(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure(), WithMetadata(map[string]string{"x-test": "1"}), WithTimeout(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := c.CallContext(context.Background())
	defer cancel()
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("no outgoing metadata")
	}
	for k, want := range map[string]string{"authorization": "Bearer k", "x-test": "1", "xai-sdk-version": "go/" + Version} {
		if got := md.Get(k); len(got) != 1 || got[0] != want {
			t.Errorf("metadata %s = %v, want %q", k, got, want)
		}
	}
	if got := md.Get("xai-sdk-language"); len(got) != 1 || got[0] == "" {
		t.Errorf("xai-sdk-language = %v", got)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("CallContext set no deadline on a context without one")
	}
	if remaining := time.Until(deadline); remaining < 80*time.Second || remaining > 90*time.Second {
		t.Errorf("deadline in %s, want about 90s", remaining)
	}
}

func TestCallContextKeepsTheCallersDeadline(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	parent, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelParent()
	ctx, cancel := c.CallContext(parent)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if remaining := time.Until(deadline); remaining > 5*time.Second {
		t.Errorf("CallContext extended the caller's 5s deadline to %s", remaining)
	}
}

func TestCallContextKeepsCallerMetadata(t *testing.T) {
	c, err := New(WithAPIKey("k"), WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	parent := metadata.AppendToOutgoingContext(context.Background(), "x-request", "r1")
	ctx, cancel := c.CallContext(parent)
	defer cancel()
	md, _ := metadata.FromOutgoingContext(ctx)
	if got := md.Get("x-request"); len(got) != 1 || got[0] != "r1" {
		t.Errorf("caller metadata lost: %v", got)
	}
	if got := md.Get("authorization"); len(got) != 1 {
		t.Errorf("authorization = %v", got)
	}
}

func TestWrapErrMapsStatusCodes(t *testing.T) {
	err := wrapErr(status.Error(codes.NotFound, "no such model"))
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != codes.NotFound {
		t.Fatalf("wrapErr: %v", err)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(err, ErrNotFound) = false")
	}
	if errors.Is(err, ErrUnauthenticated) {
		t.Error("errors.Is(err, ErrUnauthenticated) = true for a NotFound")
	}
	if wrapErr(nil) != nil {
		t.Error("wrapErr(nil) != nil")
	}
	plain := errors.New("local")
	if wrapErr(plain) != plain {
		t.Error("wrapErr changed a non-status error")
	}
	if wrapErr(err) != err {
		t.Error("wrapErr re-wrapped an *Error")
	}
}

func TestValidationErrorIsInvalidArgument(t *testing.T) {
	err := &ValidationError{Param: "detail", Value: "hgh", Accepted: []string{"auto", "low", "high"}}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Error("ValidationError should match ErrInvalidArgument")
	}
	if err.Error() != `xai: invalid detail hgh: must be one of [auto low high]` {
		t.Errorf("message: %s", err.Error())
	}
}

func TestPollDefaultsAndBounds(t *testing.T) {
	n := PollOptions{}.normalized()
	if n.Interval != DefaultPollInterval || n.Timeout != DefaultPollTimeout {
		t.Errorf("defaults: %+v", n)
	}
	n = PollOptions{Interval: -1, Timeout: 365 * 24 * time.Hour}.normalized()
	if n.Interval != DefaultPollInterval || n.Timeout != MaxPollDuration {
		t.Errorf("bounds: %+v", n)
	}
}

func TestPollStopsOnDoneTimeoutAndContext(t *testing.T) {
	calls := 0
	err := poll(context.Background(), PollOptions{Interval: time.Millisecond, Timeout: time.Second}, func(context.Context) (bool, error) {
		calls++
		return calls == 3, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("done after 3: err=%v calls=%d", err, calls)
	}

	err = poll(context.Background(), PollOptions{Interval: time.Millisecond, Timeout: 20 * time.Millisecond}, func(context.Context) (bool, error) {
		return false, nil
	})
	if !errors.Is(err, ErrPollTimeout) {
		t.Fatalf("timeout: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = poll(ctx, PollOptions{Interval: time.Hour}, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}

	boom := errors.New("boom")
	err = poll(context.Background(), PollOptions{}, func(context.Context) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("step error: %v", err)
	}
}
