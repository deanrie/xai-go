package xai

import (
	"context"

	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// SearchMatch is one chunk returned by a document search.
type SearchMatch = v1.SearchMatch

// SearchOption sets a parameter of a document search.
type SearchOption func(*v1.SearchRequest)

// WithSearchLimit caps the number of matches.
func WithSearchLimit(n int32) SearchOption {
	return func(r *v1.SearchRequest) { r.Limit = proto.Int32(n) }
}

// WithSearchInstructions guides the reranker with natural language.
func WithSearchInstructions(s string) SearchOption {
	return func(r *v1.SearchRequest) { r.Instructions = proto.String(s) }
}

// WithSemanticRetrieval searches by embedding only, optionally reranked.
func WithSemanticRetrieval(reranker *RerankerModel) SearchOption {
	return func(r *v1.SearchRequest) {
		r.RetrievalMode = &v1.SearchRequest_SemanticRetrieval{SemanticRetrieval: &v1.SemanticRetrieval{Reranker: reranker}}
	}
}

// WithKeywordRetrieval searches by keywords only, optionally reranked.
func WithKeywordRetrieval(reranker *RerankerModel) SearchOption {
	return func(r *v1.SearchRequest) {
		r.RetrievalMode = &v1.SearchRequest_KeywordRetrieval{KeywordRetrieval: &v1.KeywordRetrieval{Reranker: reranker}}
	}
}

// WithHybridRetrieval combines both and reranks with a model.
func WithHybridRetrieval(reranker *RerankerModel, searchMultiplier int32) SearchOption {
	return func(r *v1.SearchRequest) {
		h := &v1.HybridRetrieval{}
		if searchMultiplier > 0 {
			h.SearchMultiplier = proto.Int32(searchMultiplier)
		}
		if reranker != nil {
			h.Reranker = &v1.HybridRetrieval_RerankerModel{RerankerModel: reranker}
		}
		r.RetrievalMode = &v1.SearchRequest_HybridRetrieval{HybridRetrieval: h}
	}
}

// WithHybridRRF combines both retrievals with reciprocal rank fusion.
func WithHybridRRF(k int32, embeddingWeight, textWeight float32, searchMultiplier int32) SearchOption {
	return func(r *v1.SearchRequest) {
		h := &v1.HybridRetrieval{Reranker: &v1.HybridRetrieval_ReciprocalRankFusion{ReciprocalRankFusion: &v1.ReciprocalRankFusion{
			EmbeddingWeight: embeddingWeight,
			TextWeight:      textWeight,
		}}}
		if k > 0 {
			h.GetReciprocalRankFusion().K = proto.Int32(k)
		}
		if searchMultiplier > 0 {
			h.SearchMultiplier = proto.Int32(searchMultiplier)
		}
		r.RetrievalMode = &v1.SearchRequest_HybridRetrieval{HybridRetrieval: h}
	}
}

// RerankerModel names a reranking model and optional instructions.
type RerankerModel = v1.RerankerModel

// Reranker returns a reranker configuration.
func Reranker(model, instructions string) *RerankerModel {
	r := &RerankerModel{}
	if model != "" {
		r.Model = proto.String(model)
	}
	if instructions != "" {
		r.Instructions = proto.String(instructions)
	}
	return r
}

// Documents searches the documents in collections.
type Documents struct{ c *Client }

// Documents returns the document search service.
func (c *Client) Documents() *Documents { return &Documents{c: c} }

// Search returns the chunks in the given collections that best match the
// query.
func (d *Documents) Search(ctx context.Context, query string, collectionIDs []string, opts ...SearchOption) ([]*SearchMatch, error) {
	if len(collectionIDs) == 0 {
		return nil, &ValidationError{Param: "collection_ids", Value: 0, Accepted: []string{"1 or more collection ids"}}
	}
	req := &v1.SearchRequest{Query: query, Source: &v1.DocumentsSource{CollectionIds: collectionIDs}}
	for _, o := range opts {
		o(req)
	}
	ctx, cancel := d.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewDocumentsClient(d.c.conn).Search(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetMatches(), nil
}
