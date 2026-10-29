package xai

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// File describes an uploaded file.
type File = v1.File

// uploadChunkSize matches the Python SDK (3 MiB per stream message).
const uploadChunkSize = 3 << 20

// Files uploads, lists and downloads files.
type Files struct{ c *Client }

// Files returns the files service.
func (c *Client) Files() *Files { return &Files{c: c} }

// UploadOption configures Upload.
type UploadOption func(*uploadConfig)

type uploadConfig struct {
	expiresAfter time.Duration
	onProgress   func(uploaded, total int64)
}

// WithUploadExpiresAfter sets a time to live measured from the upload, between
// one hour and thirty days.
func WithUploadExpiresAfter(d time.Duration) UploadOption {
	return func(u *uploadConfig) { u.expiresAfter = d }
}

// WithUploadProgress reports bytes sent after every chunk. total is -1
// when the size is not known up front.
func WithUploadProgress(fn func(uploaded, total int64)) UploadOption {
	return func(u *uploadConfig) { u.onProgress = fn }
}

// UploadFile uploads the file at path under its base name.
func (f *Files) UploadFile(ctx context.Context, path string, opts ...UploadOption) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	info, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	return f.upload(ctx, filepath.Base(path), fh, info.Size(), opts)
}

// UploadBytes uploads in-memory content under the given name.
func (f *Files) UploadBytes(ctx context.Context, name string, data []byte, opts ...UploadOption) (*File, error) {
	return f.upload(ctx, name, bytes.NewReader(data), int64(len(data)), opts)
}

// Upload streams r under the given name. Pass size -1 when unknown.
func (f *Files) Upload(ctx context.Context, name string, r io.Reader, size int64, opts ...UploadOption) (*File, error) {
	return f.upload(ctx, name, r, size, opts)
}

func (f *Files) upload(ctx context.Context, name string, r io.Reader, size int64, opts []UploadOption) (_ *File, err error) {
	ctx, end := f.c.startSpan(ctx, "file.upload", f.c.serverAttrs()...)
	defer func() { end(err) }()
	if name == "" {
		return nil, &ValidationError{Param: "name", Value: name, Accepted: []string{"a non-empty file name"}}
	}
	var cfg uploadConfig
	for _, o := range opts {
		o(&cfg)
	}
	init := &v1.UploadFileInit{Name: name}
	if cfg.expiresAfter != 0 {
		if cfg.expiresAfter < time.Hour || cfg.expiresAfter > 30*24*time.Hour {
			return nil, &ValidationError{Param: "expires_after", Value: cfg.expiresAfter, Accepted: []string{"1h to 720h"}}
		}
		init.ExpiresAfter = proto.Int64(int64(cfg.expiresAfter / time.Second))
	}

	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	stream, err := v1.NewFilesClient(f.c.conn).UploadFile(ctx)
	if err != nil {
		return nil, wrapErr(err)
	}
	if err := stream.Send(&v1.UploadFileChunk{Chunk: &v1.UploadFileChunk_Init{Init: init}}); err != nil {
		return nil, wrapErr(recvAfterSendError(stream, err))
	}

	buf := make([]byte, uploadChunkSize)
	var sent int64
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if err := stream.Send(&v1.UploadFileChunk{Chunk: &v1.UploadFileChunk_Data{Data: buf[:n]}}); err != nil {
				return nil, wrapErr(recvAfterSendError(stream, err))
			}
			sent += int64(n)
			if cfg.onProgress != nil {
				cfg.onProgress(sent, size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	file, err := stream.CloseAndRecv()
	if err != nil {
		return nil, wrapErr(err)
	}
	return file, nil
}

// recvAfterSendError turns the io.EOF a client stream returns from Send into
// the real status the server closed the stream with.
func recvAfterSendError(stream grpcClientStream, err error) error {
	if errors.Is(err, io.EOF) {
		if _, rerr := stream.CloseAndRecv(); rerr != nil {
			return rerr
		}
	}
	return err
}

type grpcClientStream interface {
	CloseAndRecv() (*v1.File, error)
}

// Get returns a file's metadata.
func (f *Files) Get(ctx context.Context, fileID string) (_ *File, err error) {
	ctx, end := f.c.startSpan(ctx, "file.get", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	file, err := v1.NewFilesClient(f.c.conn).RetrieveFile(ctx, &v1.RetrieveFileRequest{FileId: fileID})
	if err != nil {
		return nil, wrapErr(err)
	}
	return file, nil
}

// Delete removes a file.
func (f *Files) Delete(ctx context.Context, fileID string) (err error) {
	ctx, end := f.c.startSpan(ctx, "file.delete", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	_, err = v1.NewFilesClient(f.c.conn).DeleteFile(ctx, &v1.DeleteFileRequest{FileId: fileID})
	return wrapErr(err)
}

// ListOrder selects the sort order of a listing.
type ListOrder int

// ListOrder values.
const (
	Ascending ListOrder = iota
	Descending
)

// ListPage is one page of files plus the token for the next one.
type ListPage struct {
	Files     []*File
	NextToken string
}

// FileSortBy selects the field files are sorted by.
type FileSortBy string

// FileSortBy values.
const (
	FileSortByCreatedAt FileSortBy = "created_at"
	FileSortByFilename  FileSortBy = "filename"
	FileSortBySize      FileSortBy = "size"
)

var fileSortByValues = map[FileSortBy]v1.FilesSortBy{
	FileSortByCreatedAt: v1.FilesSortBy_FILES_SORT_BY_CREATED_AT,
	FileSortByFilename:  v1.FilesSortBy_FILES_SORT_BY_FILENAME,
	FileSortBySize:      v1.FilesSortBy_FILES_SORT_BY_SIZE,
}

// FilesListOption narrows a file listing.
type FilesListOption func(*v1.ListFilesRequest) error

// WithFilesSortBy sorts by created_at, filename or size instead of the
// server default.
func WithFilesSortBy(by FileSortBy) FilesListOption {
	return func(r *v1.ListFilesRequest) error {
		v, ok := fileSortByValues[by]
		if !ok {
			return &ValidationError{Param: "sort_by", Value: string(by), Accepted: []string{"created_at", "filename", "size"}}
		}
		r.SortBy = v.Enum()
		return nil
	}
}

// WithFilesFilter applies a server-side filter expression to the listing.
func WithFilesFilter(expr string) FilesListOption {
	return func(r *v1.ListFilesRequest) error {
		if expr != "" {
			r.Filter = proto.String(expr)
		}
		return nil
	}
}

// ListPage returns one page of at most limit files (1..100, 0 means the
// server default of 100) starting at token.
func (f *Files) ListPage(ctx context.Context, limit int32, order ListOrder, token string, opts ...FilesListOption) (_ *ListPage, err error) {
	req := &v1.ListFilesRequest{Limit: limit}
	switch order {
	case Ascending:
		req.Order = v1.Ordering_ASCENDING
	case Descending:
		req.Order = v1.Ordering_DESCENDING
	default:
		return nil, &ValidationError{Param: "order", Value: order, Accepted: []string{"Ascending", "Descending"}}
	}
	if token != "" {
		req.PaginationToken = proto.String(token)
	}
	for _, o := range opts {
		if err := o(req); err != nil {
			return nil, err
		}
	}
	ctx, end := f.c.startSpan(ctx, "file.list", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewFilesClient(f.c.conn).ListFiles(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	return &ListPage{Files: resp.GetData(), NextToken: resp.GetPaginationToken()}, nil
}

// All iterates over every file, fetching pages as needed. The second value
// is non-nil only for the final, failed step. It walks the whole account:
// one with thousands of files takes minutes, so bound ctx or use ListPage
// when you only need the newest entries.
func (f *Files) All(ctx context.Context, order ListOrder, opts ...FilesListOption) iter.Seq2[*File, error] {
	return func(yield func(*File, error) bool) {
		token := ""
		for {
			page, err := f.ListPage(ctx, 0, order, token, opts...)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, file := range page.Files {
				if !yield(file, nil) {
					return
				}
			}
			if page.NextToken == "" {
				return
			}
			token = page.NextToken
		}
	}
}

// Content downloads a file's bytes into memory. For large files use
// ContentTo.
func (f *Files) Content(ctx context.Context, fileID string) ([]byte, error) {
	var buf bytes.Buffer
	if err := f.ContentTo(ctx, fileID, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ContentTo streams a file's bytes into w.
func (f *Files) ContentTo(ctx context.Context, fileID string, w io.Writer) (err error) {
	ctx, end := f.c.startSpan(ctx, "file.content", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	stream, err := v1.NewFilesClient(f.c.conn).RetrieveFileContent(ctx, &v1.RetrieveFileContentRequest{FileId: fileID})
	if err != nil {
		return wrapErr(err)
	}
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return wrapErr(err)
		}
		if _, err := w.Write(chunk.GetData()); err != nil {
			return err
		}
	}
}

// PublicURL is a shareable link to a file.
type PublicURL struct {
	URL string
	// ExpiresAt is zero when the link does not expire on its own.
	ExpiresAt time.Time
}

// CreatePublicURL makes a file reachable without authentication. A zero
// expiresAfter inherits the file's own expiry (or none); otherwise it must
// be between one hour and thirty days.
func (f *Files) CreatePublicURL(ctx context.Context, fileID string, expiresAfter time.Duration) (_ *PublicURL, err error) {
	req := &v1.CreatePublicUrlRequest{FileId: fileID}
	if expiresAfter != 0 {
		if expiresAfter < time.Hour || expiresAfter > 30*24*time.Hour {
			return nil, &ValidationError{Param: "expires_after", Value: expiresAfter, Accepted: []string{"1h to 720h"}}
		}
		req.ExpiresAfter = proto.Int64(int64(expiresAfter / time.Second))
	}
	ctx, end := f.c.startSpan(ctx, "file.create_public_url", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewFilesClient(f.c.conn).CreatePublicUrl(ctx, req)
	if err != nil {
		return nil, wrapErr(err)
	}
	out := &PublicURL{URL: resp.GetPublicUrl()}
	if resp.ExpiresAt != nil {
		out.ExpiresAt = resp.GetExpiresAt().AsTime()
	}
	return out, nil
}

// RevokePublicURL removes a file's public link. It reports whether a link
// was there to revoke.
func (f *Files) RevokePublicURL(ctx context.Context, fileID string) (_ bool, err error) {
	ctx, end := f.c.startSpan(ctx, "file.revoke_public_url", f.c.serverAttrs()...)
	defer func() { end(err) }()
	ctx, cancel := f.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewFilesClient(f.c.conn).RevokePublicUrl(ctx, &v1.RevokePublicUrlRequest{FileId: fileID})
	if err != nil {
		return false, wrapErr(err)
	}
	return resp.GetRevoked(), nil
}

// UploadResult is one outcome of UploadAll, in input order.
type UploadResult struct {
	Path string
	File *File
	Err  error
}

// UploadAll uploads several files concurrently, at most concurrency at a
// time (0 means 50, the Python SDK's default). It returns one result per
// path in input order; a failed upload does not stop the others. The
// context cancels uploads not yet started and those in flight.
func (f *Files) UploadAll(ctx context.Context, paths []string, concurrency int, opts ...UploadOption) []UploadResult {
	if concurrency <= 0 {
		concurrency = 50
	}
	results := make([]UploadResult, len(paths))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, p := range paths {
		results[i].Path = p
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i].Err = ctx.Err()
				return
			}
			results[i].File, results[i].Err = f.UploadFile(ctx, p, opts...)
		}(i, p)
	}
	wg.Wait()
	return results
}
