# Changelog

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). The module is pre-release; until v0.1.0 any minor version may change the API. v0.1.0 is tagged on our own readiness; the open xai-proto items (#82, #83) only decide where the stubs are generated from.

## [Unreleased]

## [0.0.1] - unreleased

First public pre-release of the Go client.

### Added

- Client with the Python SDK's defaults: `api.x.ai:443` over TLS, key from `XAI_API_KEY`, 27-minute per-call bound, 20 MiB message limits, keepalive, retry on `UNAVAILABLE`; lazy management channel with its own key.
- Chat: `Sample`, `SampleBatch`, `Stream`, `StreamBatch` (one accumulator shared across outputs), `Defer`/`DeferBatch`, stored completions, `Compact`/`CompactContext`, `SampleJSON`; function and server-side tools (`web_search`, `x_search`, `code_execution`, `collections_search`, MCP); multi-output responses with the final assistant answer as `Content()`.
- Files: chunked upload with progress, `UploadAll` with bounded concurrency, paging, content streaming, delete, public URLs.
- Images (URL or base64, aspect ratio, quality, resolution, source and reference images, storage), Videos (start, poll, wait with progress, extend, storage), Batches (chat, image and video items; create, add, get, cancel, wait, paginated results), Embed, Auth, Models, Tokenize, Documents search (semantic, keyword, hybrid, rerankers).
- Collections over the management API (experimental: generated from protos recovered out of the Python gencode, see xai-org/xai-proto#83).
- Errors: `*Error` with the gRPC code, `errors.Is` sentinels, `Status()`, `Retryable()`; `*ValidationError` for out-of-range literals; `ErrPollTimeout`, `ErrDeferredExpired`, `*VideoError`, `*DocumentIndexingError`, `*JSONError`.
- Conformance suite against `xai-sdk-python/tests/server.py`: API, management, two flaky servers for the retry case, one slow server for the timeout cases.
- File content parts in chat messages (`FileRef`, `FileBytes`, `FileURL`), `ImageGenerationTool` and `AttachmentSearchTool`, `Response.ImageOutputs()` with the decoded image, `Response.ToolOutputs()`, `WithSearchParameters`; `XAI_API_HOST` / `XAI_MANAGEMENT_API_HOST` from the environment, `WithUserAgent`.
- OpenTelemetry client spans on every call (`WithTracerProvider`, `WithoutSensitiveAttributes`) with the Python SDK's span names and `gen_ai.*` attributes; the client code imports only the OpenTelemetry API.
- `docs/python-mapping.md` with every Python SDK method and its Go counterpart; runnable programs under `examples/`.

### Changed

- Before the first tag, upload and collection options were renamed to carry their service prefix: `WithExpiresAfter` -> `WithUploadExpiresAfter`; `WithChunking`, `WithMetric`, `WithEmbeddingModel`, `WithFieldDefinitions`, `WithTeamID` -> `WithCollection*`.

[Unreleased]: https://github.com/deanrie/xai-go/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/deanrie/xai-go/releases/tag/v0.0.1
