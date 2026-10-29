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

// WithCollectionEmbeddingModel sets the embedding model of the index.
func WithCollectionEmbeddingModel(model string) CollectionOption {
	return func(c *createCollection) { c.pb.IndexConfiguration = &rag.IndexConfiguration{ModelName: model} }
}

// WithCollectionChunking sets how documents are split; see ChunkByChars and friends.
func WithCollectionChunking(cfg *ChunkConfiguration) CollectionOption {
	return func(c *createCollection) { c.pb.ChunkConfiguration = cfg }
}

// WithCollectionMetric sets the vector distance.
func WithCollectionMetric(m HNSWMetric) CollectionOption {
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

// WithCollectionFieldDefinitions declares the metadata fields of the collection.
func WithCollectionFieldDefinitions(defs ...*FieldDefinition) CollectionOption {
	return func(c *createCollection) { c.pb.FieldDefinitions = defs }
}

// WithCollectionTeamID scopes the call to a team the management key can act for.
func WithCollectionTeamID(teamID string) CollectionOption {
	return func(c *createCollection) { c.pb.TeamId = proto.String(teamID) }
}

// Collections manages collections and their documents over the management
// API; search goes through Documents.
//
// Experimental: the protos behind this service are not published in
// xai-proto yet (xai-org/xai-proto#83) and are generated from descriptors
// recovered from the Python SDK. The Go API here may change when the
// upstream file lands.
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
func (cs *Collections) Create(ctx context.Context, name string, opts ...CollectionOption) (_ *CollectionMetadata, err error) {
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
	ctx, end := cs.c.startSpan(ctx, "collections.create_collection", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	meta, err := stub.CreateCollection(ctx, req.pb)
	if err != nil {
		return nil, wrapErr(err)
	}
	return meta, nil
}

// Get returns a collection's metadata.
func (cs *Collections) Get(ctx context.Context, collectionID string) (_ *CollectionMetadata, err error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.get_collection_metadata", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	meta, err := stub.GetCollectionMetadata(ctx, &collections.GetCollectionMetadataRequest{CollectionId: collectionID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return meta, nil
}

// Delete removes a collection.
func (cs *Collections) Delete(ctx context.Context, collectionID string) (err error) {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.delete_collection", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.DeleteCollection(ctx, &collections.DeleteCollectionRequest{CollectionId: collectionID})
	return wrapErr(err)
}

// CollectionSortBy selects the field collections are sorted by.
type CollectionSortBy string

// CollectionSortBy values.
const (
	CollectionSortByName CollectionSortBy = "name"
	CollectionSortByAge  CollectionSortBy = "age"
)

var collectionSortByValues = map[CollectionSortBy]collections.CollectionsSortBy{
	CollectionSortByName: collections.CollectionsSortBy_COLLECTIONS_SORT_BY_NAME,
	CollectionSortByAge:  collections.CollectionsSortBy_COLLECTIONS_SORT_BY_AGE,
}

// CollectionsListOption narrows or sizes a collection listing.
type CollectionsListOption func(*collections.ListCollectionsRequest) error

// WithCollectionsLimit caps one page at n collections (1..100); 0 keeps the
// server default.
func WithCollectionsLimit(n int32) CollectionsListOption {
	return func(r *collections.ListCollectionsRequest) error {
		if n != 0 {
			r.Limit = proto.Int32(n)
		}
		return nil
	}
}

// WithCollectionsSortBy sorts by name or age instead of the server default.
func WithCollectionsSortBy(by CollectionSortBy) CollectionsListOption {
	return func(r *collections.ListCollectionsRequest) error {
		v, ok := collectionSortByValues[by]
		if !ok {
			return &ValidationError{Param: "sort_by", Value: string(by), Accepted: []string{"name", "age"}}
		}
		r.SortBy = v.Enum()
		return nil
	}
}

// WithCollectionsFilter applies a server-side filter expression over
// collection_name, created_at and documents_count, for example
// `collection_name:"SEC" AND documents_count:>10`.
func WithCollectionsFilter(expr string) CollectionsListOption {
	return func(r *collections.ListCollectionsRequest) error {
		if expr != "" {
			r.Filter = proto.String(expr)
		}
		return nil
	}
}

// CollectionsPage is one page of collections plus the token for the next.
type CollectionsPage struct {
	Collections []*CollectionMetadata
	NextToken   string
}

func (cs *Collections) listRequest(order ListOrder, token string, opts []CollectionsListOption) (*collections.ListCollectionsRequest, error) {
	ord, err := sharedOrdering(order)
	if err != nil {
		return nil, err
	}
	req := &collections.ListCollectionsRequest{Order: ord.Enum()}
	if token != "" {
		req.PaginationToken = proto.String(token)
	}
	for _, o := range opts {
		if err := o(req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// ListPage returns one page of collections starting at token ("" for the
// first page). An empty NextToken means the last page.
func (cs *Collections) ListPage(ctx context.Context, order ListOrder, token string, opts ...CollectionsListOption) (_ *CollectionsPage, err error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	req, err := cs.listRequest(order, token, opts)
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.list_collections", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	resp, err := stub.ListCollections(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return &CollectionsPage{Collections: resp.GetCollections(), NextToken: resp.GetPaginationToken()}, nil
}

// All iterates over every collection, fetching pages as needed. It walks
// the whole list; bound ctx on accounts with many collections, or use
// ListPage.
func (cs *Collections) All(ctx context.Context, order ListOrder, opts ...CollectionsListOption) iter.Seq2[*CollectionMetadata, error] {
	return func(yield func(*CollectionMetadata, error) bool) {
		stub, err := cs.stub()
		if err != nil {
			yield(nil, err)
			return
		}
		token := ""
		for {
			req, err := cs.listRequest(order, token, opts)
			if err != nil {
				yield(nil, err)
				return
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
func (cs *Collections) AddDocument(ctx context.Context, collectionID, fileID string, fields map[string]string) (err error) {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.add_existing_document", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.AddDocumentToCollection(ctx, &collections.AddDocumentToCollectionRequest{CollectionId: collectionID, FileId: fileID, Fields: fields})
	return wrapErr(err)
}

// RemoveDocument detaches a file from a collection; the file itself stays.
func (cs *Collections) RemoveDocument(ctx context.Context, collectionID, fileID string) (err error) {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.remove_document", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	_, err = stub.RemoveDocumentFromCollection(ctx, &collections.RemoveDocumentFromCollectionRequest{CollectionId: collectionID, FileId: fileID})
	return wrapErr(err)
}

// GetDocument returns a document's metadata and indexing status.
func (cs *Collections) GetDocument(ctx context.Context, collectionID, fileID string) (_ *DocumentMetadata, err error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.get_document_metadata", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	doc, err := stub.GetDocumentMetadata(ctx, &collections.GetDocumentMetadataRequest{CollectionId: collectionID, FileId: fileID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return doc, nil
}

// DocumentSortBy selects the field documents are sorted by.
type DocumentSortBy string

// DocumentSortBy values.
const (
	DocumentSortByName DocumentSortBy = "name"
	DocumentSortByAge  DocumentSortBy = "age"
	DocumentSortBySize DocumentSortBy = "size"
)

var documentSortByValues = map[DocumentSortBy]collections.DocumentsSortBy{
	DocumentSortByName: collections.DocumentsSortBy_DOCUMENTS_SORT_BY_NAME,
	DocumentSortByAge:  collections.DocumentsSortBy_DOCUMENTS_SORT_BY_AGE,
	DocumentSortBySize: collections.DocumentsSortBy_DOCUMENTS_SORT_BY_SIZE,
}

// DocumentsListOption narrows or sizes a document listing.
type DocumentsListOption func(*collections.ListDocumentsRequest) error

// WithDocumentsLimit caps one page at n documents (1..100); 0 keeps the
// server default.
func WithDocumentsLimit(n int32) DocumentsListOption {
	return func(r *collections.ListDocumentsRequest) error {
		if n != 0 {
			r.Limit = proto.Int32(n)
		}
		return nil
	}
}

// WithDocumentsSortBy sorts by name, age or size instead of the server
// default.
func WithDocumentsSortBy(by DocumentSortBy) DocumentsListOption {
	return func(r *collections.ListDocumentsRequest) error {
		v, ok := documentSortByValues[by]
		if !ok {
			return &ValidationError{Param: "sort_by", Value: string(by), Accepted: []string{"name", "age", "size"}}
		}
		r.SortBy = v.Enum()
		return nil
	}
}

// WithDocumentsFilter applies a server-side filter expression over document
// fields and metadata.
func WithDocumentsFilter(expr string) DocumentsListOption {
	return func(r *collections.ListDocumentsRequest) error {
		if expr != "" {
			r.Filter = proto.String(expr)
		}
		return nil
	}
}

// DocumentsPage is one page of documents plus the token for the next.
type DocumentsPage struct {
	Documents []*DocumentMetadata
	NextToken string
}

func (cs *Collections) documentsRequest(collectionID string, order ListOrder, token string, opts []DocumentsListOption) (*collections.ListDocumentsRequest, error) {
	ord, err := sharedOrdering(order)
	if err != nil {
		return nil, err
	}
	req := &collections.ListDocumentsRequest{CollectionId: collectionID, Order: ord.Enum()}
	if token != "" {
		req.PaginationToken = proto.String(token)
	}
	for _, o := range opts {
		if err := o(req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// DocumentsPage returns one page of a collection's documents starting at
// token ("" for the first page). An empty NextToken means the last page.
func (cs *Collections) DocumentsPage(ctx context.Context, collectionID string, order ListOrder, token string, opts ...DocumentsListOption) (_ *DocumentsPage, err error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	req, err := cs.documentsRequest(collectionID, order, token, opts)
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.list_documents", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	resp, err := stub.ListDocuments(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return &DocumentsPage{Documents: resp.GetDocuments(), NextToken: resp.GetPaginationToken()}, nil
}

// Documents iterates over the documents of a collection, fetching pages as
// needed.
func (cs *Collections) Documents(ctx context.Context, collectionID string, order ListOrder, opts ...DocumentsListOption) iter.Seq2[*DocumentMetadata, error] {
	return func(yield func(*DocumentMetadata, error) bool) {
		stub, err := cs.stub()
		if err != nil {
			yield(nil, err)
			return
		}
		token := ""
		for {
			req, err := cs.documentsRequest(collectionID, order, token, opts)
			if err != nil {
				yield(nil, err)
				return
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
func (cs *Collections) ReindexDocument(ctx context.Context, collectionID, fileID string) (err error) {
	stub, err := cs.stub()
	if err != nil {
		return err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.reindex_document", cs.c.serverAttrs()...)
	defer func() { end(err) }()
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
func (cs *Collections) GenerateDescription(ctx context.Context, collectionID string) (_ string, err error) {
	stub, err := cs.stub()
	if err != nil {
		return "", err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.generate_collection_description", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	resp, err := stub.GenerateCollectionDescription(ctx, &collections.GenerateCollectionDescriptionRequest{CollectionId: collectionID})
	if err != nil {
		return "", wrapErr(err)
	}
	return resp.GetCollectionDescription(), nil
}

// UpdateOption sets a field of Collections.Update.
type UpdateOption func(*collections.UpdateCollectionRequest)

// RenameCollection changes the collection name.
func RenameCollection(name string) UpdateOption {
	return func(r *collections.UpdateCollectionRequest) { r.CollectionName = proto.String(name) }
}

// UpdateDescription changes the collection description.
func UpdateDescription(s string) UpdateOption {
	return func(r *collections.UpdateCollectionRequest) { r.CollectionDescription = proto.String(s) }
}

// UpdateChunking changes how new documents are split.
func UpdateChunking(cfg *ChunkConfiguration) UpdateOption {
	return func(r *collections.UpdateCollectionRequest) { r.ChunkConfiguration = cfg }
}

// AddFieldDefinition declares a new metadata field.
func AddFieldDefinition(def *FieldDefinition) UpdateOption {
	return func(r *collections.UpdateCollectionRequest) {
		r.FieldDefinitionUpdates = append(r.FieldDefinitionUpdates, &collections.FieldDefinitionUpdate{
			FieldDefinition: def, Operation: collections.FieldDefinitionOperation_FIELD_DEFINITION_ADD,
		})
	}
}

// RemoveFieldDefinition drops a metadata field by key.
func RemoveFieldDefinition(key string) UpdateOption {
	return func(r *collections.UpdateCollectionRequest) {
		r.FieldDefinitionUpdates = append(r.FieldDefinitionUpdates, &collections.FieldDefinitionUpdate{
			FieldDefinition: &FieldDefinition{Key: key}, Operation: collections.FieldDefinitionOperation_FIELD_DEFINITION_DELETE,
		})
	}
}

// Update changes a collection's name, description, chunking or fields.
func (cs *Collections) Update(ctx context.Context, collectionID string, opts ...UpdateOption) (_ *CollectionMetadata, err error) {
	if len(opts) == 0 {
		return nil, &ValidationError{Param: "opts", Value: 0, Accepted: []string{"at least one UpdateOption"}}
	}
	req := &collections.UpdateCollectionRequest{CollectionId: collectionID}
	for _, o := range opts {
		o(req)
	}
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.update_collection", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	meta, err := stub.UpdateCollection(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return meta, nil
}

// BatchGetDocuments returns metadata for several documents at once; ids
// the collection does not know are left out.
func (cs *Collections) BatchGetDocuments(ctx context.Context, collectionID string, fileIDs ...string) (_ []*DocumentMetadata, err error) {
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.batch_get_documents", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	resp, err := stub.BatchGetDocuments(ctx, &collections.BatchGetDocumentsRequest{CollectionId: collectionID, FileIds: fileIDs})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetDocuments(), nil
}

// UploadDocument uploads content as a file, adds it to the collection and
// waits until it is indexed. It is Files.UploadBytes, AddDocument and
// WaitForDocument in one call, matching the Python SDK's upload_document.
func (cs *Collections) UploadDocument(ctx context.Context, collectionID, name string, data []byte, fields map[string]string, opts PollOptions) (*DocumentMetadata, error) {
	file, err := cs.c.Files().UploadBytes(ctx, name, data)
	if err != nil {
		return nil, err
	}
	if err := cs.AddDocument(ctx, collectionID, file.GetId(), fields); err != nil {
		return nil, err
	}
	return cs.WaitForDocument(ctx, collectionID, file.GetId(), opts)
}

// DocumentUpdate carries the optional changes for UpdateDocument. Nil
// pointers and a nil Fields map leave that part untouched.
type DocumentUpdate struct {
	Name        *string
	Data        []byte
	ContentType *string
	Fields      map[string]string
}

// UpdateDocument replaces a document's name, content, content type or
// metadata fields; the server re-indexes when the content changes.
func (cs *Collections) UpdateDocument(ctx context.Context, collectionID, fileID string, u DocumentUpdate) (_ *DocumentMetadata, err error) {
	if u.Name == nil && u.Data == nil && u.ContentType == nil && u.Fields == nil {
		return nil, &ValidationError{Param: "update", Value: "empty", Accepted: []string{"at least one of Name, Data, ContentType, Fields"}}
	}
	req := &collections.UpdateDocumentRequest{CollectionId: collectionID, FileId: fileID, Name: u.Name, ContentType: u.ContentType, Fields: u.Fields}
	if u.Data != nil {
		req.Data = u.Data
	}
	stub, err := cs.stub()
	if err != nil {
		return nil, err
	}
	ctx, end := cs.c.startSpan(ctx, "collections.update_document", cs.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := cs.c.ManagementCallContext(ctx)
	defer cancel()
	doc, err := stub.UpdateDocument(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return doc, nil
}
