# Changelog

## Unreleased

First public pre-release of the Go client. Module API may change until v0.1.0.

- Client with the Python SDK's defaults (host, key from `XAI_API_KEY`, 27 min bound, 20 MiB limits, keepalive, retry on UNAVAILABLE), lazy management channel.
- Chat: sample, sample batch, stream, stream batch (shared accumulator across outputs), deferred, stored completions, compact context, JSON schema sampling (`SampleJSON`), function and server-side tools (web_search, x_search, code_execution, collections_search, MCP), multi-output responses.
- Files (chunked upload with progress, concurrent `UploadAll`, list, content, delete, public URLs), Images, Videos, Batches (chat, image, video items), Embed, Auth, Models, Tokenize, Documents search.
- Collections over the management API from protos recovered out of the Python gencode (xai-org/xai-proto#83).
- Errors: `*Error` with gRPC code, `errors.Is` sentinels, `Status()`, `Retryable()`; `*ValidationError` for out-of-range literals; `ErrPollTimeout` for long operations.
- Conformance suite against `xai-sdk-python/tests/server.py` (API, management and flaky servers).
