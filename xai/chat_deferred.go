package xai

import (
	"context"
	"errors"
	"fmt"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// ErrDeferredExpired is returned when the server dropped a deferred request
// before the client collected the result.
var ErrDeferredExpired = errors.New("xai: deferred request expired")

// ErrDeferredFailed is returned when the server reports a deferred request
// as failed.
var ErrDeferredFailed = errors.New("xai: deferred request failed")

// Defer samples one completion through a deferred request: the server
// accepts the request and the client polls for the result. This keeps
// long generations alive over connections that drop long-running calls.
// Polling defaults follow DefaultPollInterval and DefaultPollTimeout.
func (ch *Chat) Defer(ctx context.Context, opts PollOptions) (*Response, error) {
	pb, err := ch.deferN(ctx, 1, opts)
	if err != nil {
		return nil, err
	}
	index := autoDetectMultiOutput(ch.initialIndex(), outputIndexes(pb.GetOutputs()))
	return newResponse(pb, index), nil
}

// DeferBatch is Defer for n completions.
func (ch *Chat) DeferBatch(ctx context.Context, n int, opts PollOptions) ([]*Response, error) {
	if n < 1 {
		return nil, &ValidationError{Param: "n", Value: n, Accepted: []string{"1 or more"}}
	}
	pb, err := ch.deferN(ctx, n, opts)
	if err != nil {
		return nil, err
	}
	out := make([]*Response, n)
	for i := range out {
		out[i] = newResponse(pb, int32(i))
	}
	return out, nil
}

func (ch *Chat) deferN(ctx context.Context, n int, opts PollOptions) (*v1.GetChatCompletionResponse, error) {
	if ch.err != nil {
		return nil, ch.err
	}
	client := v1.NewChatClient(ch.c.conn)

	startCtx, cancel := ch.c.CallContext(ctx)
	started, err := client.StartDeferredCompletion(startCtx, ch.request(int32(n)))
	cancel()
	if err != nil {
		return nil, wrapErr(err)
	}

	var result *v1.GetChatCompletionResponse
	err = poll(ctx, opts, func(ctx context.Context) (bool, error) {
		ctx, cancel := ch.c.CallContext(ctx)
		defer cancel()
		r, err := client.GetDeferredCompletion(ctx, &v1.GetDeferredRequest{RequestId: started.GetRequestId()})
		if err != nil {
			return false, wrapErr(err)
		}
		switch r.GetStatus() {
		case v1.DeferredStatus_DONE:
			result = r.GetResponse()
			return true, nil
		case v1.DeferredStatus_PENDING:
			return false, nil
		case v1.DeferredStatus_EXPIRED:
			return false, ErrDeferredExpired
		case v1.DeferredStatus_FAILED:
			return false, ErrDeferredFailed
		case v1.DeferredStatus_INVALID_DEFERRED_STATUS:
			return false, fmt.Errorf("xai: deferred request %q: status not set", started.GetRequestId())
		default:
			return false, fmt.Errorf("xai: deferred request %q: unknown status %v", started.GetRequestId(), r.GetStatus())
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetStoredCompletion fetches a completion the server stored for a chat
// created with WithStoreMessages.
func (c *Client) GetStoredCompletion(ctx context.Context, responseID string) (*Response, error) {
	ctx, cancel := c.CallContext(ctx)
	defer cancel()
	pb, err := v1.NewChatClient(c.conn).GetStoredCompletion(ctx, &v1.GetStoredCompletionRequest{ResponseId: responseID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return newResponse(pb, autoDetectMultiOutput(0, outputIndexes(pb.GetOutputs()))), nil
}

// DeleteStoredCompletion deletes a stored completion.
func (c *Client) DeleteStoredCompletion(ctx context.Context, responseID string) error {
	ctx, cancel := c.CallContext(ctx)
	defer cancel()
	_, err := v1.NewChatClient(c.conn).DeleteStoredCompletion(ctx, &v1.DeleteStoredCompletionRequest{ResponseId: responseID})
	return wrapErr(err)
}

// CompactContextResponse is the server's compacted form of a conversation.
type CompactContextResponse = v1.CompactContextResponse

// CompactContext asks the server to compress the current messages into one
// encrypted message, without changing this Chat. Use Compact to apply it.
func (ch *Chat) CompactContext(ctx context.Context) (*CompactContextResponse, error) {
	if ch.err != nil {
		return nil, ch.err
	}
	ctx, cancel := ch.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewChatClient(ch.c.conn).CompactContext(ctx, &v1.CompactContextRequest{Model: ch.req.GetModel(), Input: ch.req.GetMessages()})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp, nil
}

// Compact replaces the conversation with its compacted form: all messages
// become one user message carrying the encrypted content, the same way the
// Python SDK's chat.compact() does.
func (ch *Chat) Compact(ctx context.Context) (*CompactContextResponse, error) {
	resp, err := ch.CompactContext(ctx)
	if err != nil {
		return nil, err
	}
	ch.req.Messages = []*Message{{Role: v1.MessageRole_ROLE_USER, EncryptedContent: resp.GetEncryptedContent()}}
	return resp, nil
}
