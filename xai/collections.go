package xai

import (
	"context"
	"fmt"
	"iter"

	"google.golang.org/protobuf/proto"

	"github.com/deanrie/xai-go/internal/gen/xai/api/v1/collections"
	"github.com/deanrie/xai-go/internal/gen/xai/api/v1/rag"
	"github.com/deanrie/xai-go/internal/gen/xai/api/v1/shared"
)

// Collection types. The protos behind them are not published in xai-proto
// yet (xai-org/xai-proto#83); the SDK generates them from descriptors
// recovered from the Python SDK, see scripts/regen.sh.
type (
	// CollectionMetadata describes a collection.
	CollectionMetadata = collections.CollectionMetadata
	// DocumentMetadata describes a document in a collection.
	DocumentMetadata = collections.DocumentMetadata
	// FieldDefinition declares a metadata field documents may carry.
	FieldDefinition = collections.FieldDefinition
	// ChunkConfiguration controls how documents are split for indexing.
	ChunkConfiguration = rag.ChunkConfiguration
	// IndexConfiguration names the embedding model of a collection.
	IndexConfiguration = rag.IndexConfiguration
	// DocumentStatus is the indexing state of a document.
	DocumentStatus = collections.DocumentStatus
)

// DocumentStatus values.
const (
	DocumentProcessing = collections.DocumentStatus_DOCUMENT_STATUS_PROCESSING
	DocumentProcessed  = collections.DocumentStatus_DOCUMENT_STATUS_PROCESSED
	DocumentFailed     = collections.DocumentStatus_DOCUMENT_STATUS_FAILED
	DocumentChunked    = collections.DocumentStatus_DOCUMENT_STATUS_CHUNKED
	DocumentEmbedding  = collections.DocumentStatus_DOCUMENT_STATUS_EMBEDDING
	DocumentWriting    = collections.DocumentStatus_DOCUMENT_STATUS_WRITING
)

// DocumentIndexingError is returned by WaitForDocument when the server
// marks a document as failed.
type DocumentIndexingError struct {
	FileID  string
	Message string
}

func (e *DocumentIndexingError) Error() string {
	return fmt.Sprintf("xai: indexing document %s failed: %s", e.FileID, e.Message)
}

// Chunking helpers.

// ChunkByChars splits documents into character windows.
func ChunkByChars(maxChars, overlapChars int32) *ChunkConfiguration {
	return &ChunkConfiguration{Config: &rag.ChunkConfiguration_CharsConfiguration{CharsConfiguration: &rag.CharsConfiguration{
		MaxChunkSizeChars: maxChars, ChunkOverlapChars: overlapChars,
	}}}
}

// ChunkByTokens splits documents into token windows of the given encoding.
func ChunkByTokens(maxTokens, overlapTokens int32, encoding string) *ChunkConfiguration {
	return &ChunkConfiguration{Config: &rag.ChunkConfiguration_TokensConfiguration{TokensConfiguration: &rag.TokensConfiguration{
		MaxChunkSizeTokens: maxTokens, ChunkOverlapTokens: overlapTokens, EncodingName: encoding,
	}}}
}

// ChunkByBytes splits documents into byte windows.
func ChunkByBytes(maxBytes, overlapBytes int32) *ChunkConfiguration {
	return &ChunkConfiguration{Config: &rag.ChunkConfiguration_BytesConfiguration{BytesConfiguration: &rag.BytesConfiguration{
		MaxChunkSizeBytes: maxBytes, ChunkOverlapBytes: overlapBytes,
	}}}
}

// HNSWMetric is the vector distance of a collection's index.
type HNSWMetric string

// Accepted HNSWMetric values.
const (
	MetricCosine       HNSWMetric = "cosine"
	MetricEuclidean    HNSWMetric = "euclidean"
	MetricInnerProduct HNSWMetric = "inner_product"
)

var hnswMetricValues = map[HNSWMetric]rag.HNSWMetric{
	MetricCosine:       rag.HNSWMetric_HNSW_METRIC_COSINE,
	MetricEuclidean:    rag.HNSWMetric_HNSW_METRIC_EUCLIDEAN,
	MetricInnerProduct: rag.HNSWMetric_HNSW_METRIC_INNER_PRODUCT,
}

// CollectionOption sets a parameter of CreateCollection.
type CollectionOption func(*createCollection)

type createCollection struct {
	pb  *collections.CreateCollectionRequest
	err error
}

// WithCollectionDescription sets the description the model sees when
// choosing a collection to search.
func WithCollectionDescription(s string) CollectionOption {
	return func(c *createCollection) { c.pb.CollectionDescription = proto.String(s) }
}

// WithEmbeddingModel sets the embedding model of the index.
func WithEmbeddingModel(model string) CollectionOption {
	return func(c *createCollection) { c.pb.IndexConfiguration = &rag.IndexConfiguration{ModelName: model} }
}

// WithChunking sets how documents are split; see ChunkByChars and friends.
func WithChunking(cfg *ChunkConfiguration) CollectionOption {
	return func(c *createCollection) { c.pb.ChunkConfiguration = cfg }
}

// WithMetric sets the vector distance.
func WithMetric(m HNSWMetric) CollectionOption {
	return func(c *createCollection) {
		v, ok := hnswMetricValues[m]
		if !ok {
			if c.err == nil {
				c.err = &ValidationError{Param: "metric", Value: string(m), Accepted: []string{"cosine", "euclidean", "inner_product"}}
			}
			return
		}
		c.pb.MetricSpace = v.Enum()
	}
}

// WithFieldDefinitions declares the metadata fields of the collection.
func WithFieldDefinitions(defs ...*FieldDefinition) CollectionOption {
	return func(c *createCollection) { c.pb.FieldDefinitions = defs }
}

// WithTeamID scopes the call to a team the management key can act for.
func WithTeamID(teamID string) CollectionOption {
	return func(c *createCollection) { c.pb.TeamId = proto.String(teamID) }
}

// Collections manages collections and their documents over the management
// API; search goes through Documents.
type Collections struct{ c *Client }

// Collections returns the collections service. It needs a management API key
// (WithManagementAPIKey or XAI_MANAGEMENT_KEY); calls without one return
// ErrMissingManagementAPIKey.
func (c *Client) Collections() *Collections { return &Collections{c: c} }

func (cs *Collections) stub() (collections.CollectionsClient, error) {
	conn, err := cs.c.managementConn()
	if err != nil {
		return nil, err
	}
	return collections.NewCollectionsClient(conn), nil
}

// Create creates a collection.
func (cs *Collections) Create(ctx context.Context, name string, opts ...CollectionOption) (*CollectionMetadata, error) {
	req := &createCollection{pb: &collections.CreateCollectionRequest{CollectionName: name}}
	for _, o := range opts {
		o(req)
	}
	if req.err != nil {
		return nil, req.err
	}
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	meta, err := stub.CreateCollection(ctx, req.pb)
	if err != nil {
		return nil, wrapErr(err)
	}
	return meta, nil
}

// Get returns a collection's metadata.
func (cs *Collections) Get(ctx context.Context, collectionID string) (*CollectionMetadata, error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	meta, err := stub.GetCollectionMetadata(ctx, &collections.GetCollectionMetadataRequest{CollectionId: collectionID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return meta, nil
}

// Delete removes a collection.
func (cs *Collections) Delete(ctx context.Context, collectionID string) error {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.DeleteCollection(ctx, &collections.DeleteCollectionRequest{CollectionId: collectionID})
	return wrapErr(err)
}

// All iterates over every collection, fetching pages as needed.
func (cs *Collections) All(ctx context.Context, order ListOrder) iter.Seq2[*CollectionMetadata, error] {
	return func(yield func(*CollectionMetadata, error) bool) {
		stub, err := cs.stub()
		if err != nil {
			yield(nil, err)
			return
		}
		ord, err := sharedOrdering(order)
		if err != nil {
			yield(nil, err)
			return
		}
		token := ""
		for {
			req := &collections.ListCollectionsRequest{Order: ord.Enum()}
			if token != "" {
				req.PaginationToken = proto.String(token)
			}
			cctx, cancel := cs.c.ManagementCallContext(ctx)
			resp, err := stub.ListCollections(cctx, req)
			cancel()
			if err != nil {
				yield(nil, wrapErr(err))
				return
			}
			for _, m := range resp.GetCollections() {
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

func sharedOrdering(order ListOrder) (shared.Ordering, error) {
	switch order {
	case Ascending:
		return shared.Ordering_ORDERING_ASCENDING, nil
	case Descending:
		return shared.Ordering_ORDERING_DESCENDING, nil
	default:
		return 0, &ValidationError{Param: "order", Value: order, Accepted: []string{"Ascending", "Descending"}}
	}
}

// AddDocument attaches an already uploaded file (see Files.Upload) to a
// collection with optional metadata fields. Indexing runs asynchronously;
// use WaitForDocument to block until it is searchable.
func (cs *Collections) AddDocument(ctx context.Context, collectionID, fileID string, fields map[string]string) error {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.AddDocumentToCollection(ctx, &collections.AddDocumentToCollectionRequest{CollectionId: collectionID, FileId: fileID, Fields: fields})
	return wrapErr(err)
}

// RemoveDocument detaches a file from a collection; the file itself stays.
func (cs *Collections) RemoveDocument(ctx context.Context, collectionID, fileID string) error {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.RemoveDocumentFromCollection(ctx, &collections.RemoveDocumentFromCollectionRequest{CollectionId: collectionID, FileId: fileID})
	return wrapErr(err)
}

// GetDocument returns a document's metadata and indexing status.
func (cs *Collections) GetDocument(ctx context.Context, collectionID, fileID string) (*DocumentMetadata, error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	doc, err := stub.GetDocumentMetadata(ctx, &collections.GetDocumentMetadataRequest{CollectionId: collectionID, FileId: fileID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return doc, nil
}

// Documents iterates over the documents of a collection.
func (cs *Collections) Documents(ctx context.Context, collectionID string, order ListOrder) iter.Seq2[*DocumentMetadata, error] {
	return func(yield func(*DocumentMetadata, error) bool) {
		stub, err := cs.stub()
		if err != nil {
			yield(nil, err)
			return
		}
		ord, err := sharedOrdering(order)
		if err != nil {
			yield(nil, err)
			return
		}
		token := ""
		for {
			req := &collections.ListDocumentsRequest{CollectionId: collectionID, Order: ord.Enum()}
			if token != "" {
				req.PaginationToken = proto.String(token)
			}
			cctx, cancel := cs.c.ManagementCallContext(ctx)
			resp, err := stub.ListDocuments(cctx, req)
			cancel()
			if err != nil {
				yield(nil, wrapErr(err))
				return
			}
			for _, d := range resp.GetDocuments() {
				if !yield(d, nil) {
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

// ReindexDocument re-runs indexing for a document.
func (cs *Collections) ReindexDocument(ctx context.Context, collectionID, fileID string) error {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.ReIndexDocument(ctx, &collections.ReIndexDocumentRequest{CollectionId: collectionID, FileId: fileID})
	return wrapErr(err)
}

// WaitForDocument polls until the document is indexed. A failed document is
// a *DocumentIndexingError; a timeout is ErrPollTimeout. The same statuses
// count as in-progress as in the Python SDK.
func (cs *Collections) WaitForDocument(ctx context.Context, collectionID, fileID string, opts PollOptions) (*DocumentMetadata, error) {
	var result *DocumentMetadata
	err := poll(ctx, opts, func(ctx context.Context) (bool, error) {
		doc, err := cs.GetDocument(ctx, collectionID, fileID)
		if err != nil {
			return false, err
		}
		switch doc.GetStatus() {
		case DocumentProcessed:
			result = doc
			return true, nil
		case DocumentFailed:
			return false, &DocumentIndexingError{FileID: fileID, Message: doc.GetErrorMessage()}
		case DocumentProcessing, DocumentChunked, DocumentEmbedding, DocumentWriting,
			collections.DocumentStatus_DOCUMENT_STATUS_UNKNOWN:
			return false, nil
		default:
			return false, nil
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// GenerateDescription asks the server to write a description of the
// collection from its documents.
func (cs *Collections) GenerateDescription(ctx context.Context, collectionID string) (string, error) {
	stub, err := cs.stub()
	if err != nil {
		return "", err
	}
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	resp, err := stub.GenerateCollectionDescription(ctx, &collections.GenerateCollectionDescriptionRequest{CollectionId: collectionID})
	if err != nil {
		return "", wrapErr(err)
	}
	return resp.GetCollectionDescription(), nil
}
