package xai

import (
	"context"
	"iter"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Batch is a server-side batch of requests processed asynchronously.
type Batch = v1.Batch

// BatchRequest is one request in a batch: a chat, image or video request.
type BatchRequest = v1.BatchRequest

// BatchRequestMetadata summarises one request in a batch.
type BatchRequestMetadata = v1.BatchRequestMetadata

// BatchChatRequest wraps a Chat's request as a batch item. id may be
// empty; the server then assigns one.
func BatchChatRequest(id string, ch *Chat) (*BatchRequest, error) {
	if ch.err != nil {
		return nil, ch.err
	}
	r := &BatchRequest{Request: &v1.BatchRequest_CompletionRequest{CompletionRequest: ch.request(1)}}
	if id != "" {
		r.BatchRequestId = proto.String(id)
	}
	return r, nil
}

// BatchResult is the outcome of one request: a response or an error.
type BatchResult struct {
	RequestID string
	// Chat is set for chat requests that succeeded.
	Chat *Response
	// Image is set for image requests that succeeded.
	Image *ImageResponse
	// Video is set for video requests that succeeded.
	Video *VideoResponse
	// Err is set when the request failed; it is an *Error with the gRPC
	// code the server recorded.
	Err error
}

func batchResultFromProto(pb *v1.BatchResult) *BatchResult {
	r := &BatchResult{RequestID: pb.GetBatchRequestId()}
	if st := pb.GetError(); st != nil {
		r.Err = wrapErr(status.ErrorProto(st))
		return r
	}
	data := pb.GetResponse()
	switch {
	case data.GetCompletionResponse() != nil:
		pbResp := data.GetCompletionResponse()
		r.Chat = newResponse(pbResp, autoDetectMultiOutput(0, outputIndexes(pbResp.GetOutputs())))
	case data.GetImageResponse() != nil:
		r.Image = data.GetImageResponse()
	case data.GetVideoResponse() != nil:
		r.Video = data.GetVideoResponse()
	}
	return r
}

// Batches manages batches.
type Batches struct{ c *Client }

// Batches returns the batch service.
func (c *Client) Batches() *Batches { return &Batches{c: c} }

// Create creates an empty batch, or one seeded from an uploaded JSONL file
// when inputFileID is set.
func (b *Batches) Create(ctx context.Context, name, inputFileID string) (*Batch, error) {
	ctx, cancel := b.c.CallContext(ctx)
	defer cancel()
	batch, err := v1.NewBatchMgmtClient(b.c.conn).CreateBatch(ctx, &v1.CreateBatchRequest{Name: name, InputFileId: inputFileID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return batch, nil
}

// Add appends requests to a batch.
func (b *Batches) Add(ctx context.Context, batchID string, reqs ...*BatchRequest) error {
	if len(reqs) == 0 {
		return &ValidationError{Param: "requests", Value: 0, Accepted: []string{"1 or more requests"}}
	}
	ctx, cancel := b.c.CallContext(ctx)
	defer cancel()
	_, err := v1.NewBatchMgmtClient(b.c.conn).AddBatchRequests(ctx, &v1.AddBatchRequestsRequest{BatchId: batchID, BatchRequests: reqs})
	return wrapErr(err)
}

// Get returns a batch with its current counts.
func (b *Batches) Get(ctx context.Context, batchID string) (*Batch, error) {
	ctx, cancel := b.c.CallContext(ctx)
	defer cancel()
	batch, err := v1.NewBatchMgmtClient(b.c.conn).GetBatch(ctx, &v1.GetBatchRequest{BatchId: batchID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return batch, nil
}

// Cancel cancels pending requests in a batch.
func (b *Batches) Cancel(ctx context.Context, batchID string) (*Batch, error) {
	ctx, cancel := b.c.CallContext(ctx)
	defer cancel()
	batch, err := v1.NewBatchMgmtClient(b.c.conn).CancelBatch(ctx, &v1.CancelBatchRequest{BatchId: batchID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return batch, nil
}

// Wait polls until every request in the batch has settled, then returns
// the batch. Defaults follow DefaultPollInterval and DefaultPollTimeout.
func (b *Batches) Wait(ctx context.Context, batchID string, opts PollOptions) (*Batch, error) {
	var result *Batch
	err := poll(ctx, opts, func(ctx context.Context) (bool, error) {
		batch, err := b.Get(ctx, batchID)
		if err != nil {
			return false, err
		}
		if batch.GetState().GetNumPending() == 0 {
			result = batch
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// All iterates over every batch, newest pages first as the server orders
// them. The error, if any, is yielded last.
func (b *Batches) All(ctx context.Context) iter.Seq2[*Batch, error] {
	return func(yield func(*Batch, error) bool) {
		client := v1.NewBatchMgmtClient(b.c.conn)
		token := ""
		for {
			req := &v1.ListBatchesRequest{}
			if token != "" {
				req.PaginationToken = proto.String(token)
			}
			cctx, cancel := b.c.CallContext(ctx)
			resp, err := client.ListBatches(cctx, req)
			cancel()
			if err != nil {
				yield(nil, wrapErr(err))
				return
			}
			for _, batch := range resp.GetBatches() {
				if !yield(batch, nil) {
					return
				}
			}
			if resp.GetPaginationToken() == "" {
				return
			}
			token = resp.GetPaginationToken()
		}
	}
}

// Requests iterates over the metadata of every request in a batch.
func (b *Batches) Requests(ctx context.Context, batchID string) iter.Seq2[*BatchRequestMetadata, error] {
	return func(yield func(*BatchRequestMetadata, error) bool) {
		client := v1.NewBatchMgmtClient(b.c.conn)
		token := ""
		for {
			req := &v1.ListBatchRequestMetadataRequest{BatchId: batchID}
			if token != "" {
				req.PaginationToken = proto.String(token)
			}
			cctx, cancel := b.c.CallContext(ctx)
			resp, err := client.ListBatchRequestMetadata(cctx, req)
			cancel()
			if err != nil {
				yield(nil, wrapErr(err))
				return
			}
			for _, m := range resp.GetBatchRequestMetadata() {
				if !yield(m, nil) {
					return
				}
			}
			if resp.GetPaginationToken() == "" {
				return
			}
			token = resp.GetPaginationToken()
		}
	}
}

// Results iterates over the settled results of a batch.
func (b *Batches) Results(ctx context.Context, batchID string) iter.Seq2[*BatchResult, error] {
	return func(yield func(*BatchResult, error) bool) {
		client := v1.NewBatchMgmtClient(b.c.conn)
		token := ""
		for {
			req := &v1.ListBatchResultsRequest{BatchId: batchID}
			if token != "" {
				req.PaginationToken = proto.String(token)
			}
			cctx, cancel := b.c.CallContext(ctx)
			resp, err := client.ListBatchResults(cctx, req)
			cancel()
			if err != nil {
				yield(nil, wrapErr(err))
				return
			}
			for _, r := range resp.GetResults() {
				if !yield(batchResultFromProto(r), nil) {
					return
				}
			}
			if resp.GetPaginationToken() == "" {
				return
			}
			token = resp.GetPaginationToken()
		}
	}
}

// Result returns one request and its result.
func (b *Batches) Result(ctx context.Context, batchID, requestID string) (*BatchRequest, *BatchResult, error) {
	ctx, cancel := b.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewBatchMgmtClient(b.c.conn).GetBatchRequestResult(ctx, &v1.GetBatchRequestResultRequest{BatchId: batchID, BatchRequestId: requestID})
	if err != nil {
		return nil, nil, wrapErr(err)
	}
	return resp.GetRequest(), batchResultFromProto(resp.GetResult()), nil
}
