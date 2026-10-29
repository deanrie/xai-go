# Python SDK to Go

Every method of the official Python SDK's sync client and where it lives here. Names follow Go conventions; behaviour follows the Python SDK and is checked against its test server in `tests/conformance`.

| Python (`client.<service>.<method>`) | Go |
| --- | --- |
| `chat.create(model, ...)` | `client.Chat(model, opts...)` |
| `chat.append(message)` | `Chat.Append(msg)`, `Chat.AppendResponse(resp)` |
| `chat.sample()` | `Chat.Sample(ctx)` |
| `chat.sample_batch(n)` | `Chat.SampleBatch(ctx, n)` |
| `chat.stream()` | `Chat.Stream(ctx)` then `for resp, chunk := range stream.All()` |
| `chat.stream_batch(n)` | `Chat.StreamBatch(ctx, n)` then `for resps, chunks := range stream.All()` |
| `chat.defer()`, `chat.defer_batch(n)` | `Chat.Defer(ctx, poll)`, `Chat.DeferBatch(ctx, n, poll)` |
| `chat.parse(Model)` | `Chat.SampleJSON(ctx, schema, &out)` (or `WithJSONSchema` and your own decode) |
| `chat.compact()`, `chat.compact_context()` | `Chat.Compact(ctx)`, `Chat.CompactContext(ctx)` |
| `chat.get_stored_completion(id)` | `Client.GetStoredCompletion(ctx, id)` |
| `chat.delete_stored_completion(id)` | `Client.DeleteStoredCompletion(ctx, id)` |
| `user(...)`, `system(...)`, `assistant(...)`, `developer(...)`, `tool_result(...)` | `User(...)`, `System(...)`, `Assistant(...)`, `Developer(...)`, `ToolResult(...)` |
| `text(...)`, `image(url, detail)` | `Text(...)`, `ImageURL(url, detail)`, `ImageFile(fileID, detail)` (uploaded file as the image source; also accepted by `WithSourceImage` / `WithReferenceImages`, matching `image_file_id` / `image_file_ids`) |
| `file(file_id=...)`, `file(data=..., filename=...)`, `file(url=...)` | `FileRef(id)`, `FileBytes(name, data, mimeType)`, `FileURL(url)` |
| `tool(...)`, `web_search(...)`, `x_search(...)`, `code_execution()`, `collections_search(...)`, `mcp(...)`, `image_generation(action)`, `attachment_search(...)` | `FunctionTool(...)`, `WebSearchTool(...)`, `XSearchTool(...)`, `CodeExecutionTool()`, `CollectionsSearchTool(...)`, `MCPTool(...)`, `ImageGenerationTool(action)`, `AttachmentSearchTool(limit)` |
| `chat.create(..., search_parameters=SearchParameters(...))` | `WithSearchParameters(&xai.SearchParameters{Mode: xai.SearchAuto, ...})` |
| `chat.create(..., logprobs=True, top_logprobs=n)` | `WithLogprobs(n)` |
| `chat.create(..., tool_choice="auto"/"none"/"required")`, `required_tool(name)` | `WithToolChoice(xai.ToolModeAuto/None/Required)`, `WithRequiredTool(name)` / `RequiredTool(name)` |
| `chat.create(..., frequency_penalty=, presence_penalty=)` | `WithFrequencyPenalty(p)`, `WithPresencePenalty(p)` |
| `chat.create(..., service_tier="priority")` | `WithServiceTier(xai.ServiceTierPriority)` |
| `chat.create(..., include=["inline_citations", ...])` | `WithInclude(xai.IncludeInlineCitations, ...)` |
| `chat.create(..., conversation_id=)` (telemetry label only) | `WithConversationID(id)` |
| `Response.cost_usd`, `.created`, `.system_fingerprint`, `.service_tier`, `.logprobs`, `.server_side_tool_usage`, `.request_settings`, `.debug_output` | `CostUSD() (float64, bool)`, `Created()`, `SystemFingerprint()`, `ServiceTier()`, `Logprobs()`, `ServerSideToolUsage()`, `Settings()`, `DebugOutput()` |
| `Response.image_outputs`, `.tool_outputs` | `Response.ImageOutputs()` (decoded bytes, MIME type, uuid, tool call), `Response.ToolOutputs()` |
| `files.upload(path_or_bytes, filename=...)` | `Files.UploadFile(ctx, path)`, `Files.UploadBytes(ctx, name, data)`, `Files.Upload(ctx, name, r, size)` |
| `files.batch_upload(files, batch_size)` | `Files.UploadAll(ctx, paths, concurrency, opts...)` (results in input order, per-file errors) |
| `files.list(limit, order, sort_by, pagination_token, filter)` | `Files.ListPage(ctx, limit, order, token, WithFilesSortBy/WithFilesFilter...)`; `Files.All(ctx, order, opts...)` walks every page, so bound its context on large accounts |
| `files.get(id)` | `Files.Get(ctx, id)` |
| `files.delete(id)` | `Files.Delete(ctx, id)` |
| `files.content(id)` | `Files.Content(ctx, id)`, `Files.ContentTo(ctx, id, w)` |
| `files.create_public_url(id, ...)` | `Files.CreatePublicURL(ctx, id, expiresAfter)` |
| `files.revoke_public_url(id)` | `Files.RevokePublicURL(ctx, id)` |
| `image.sample(prompt, model, ...)` | `Images.Generate(ctx, model, prompt, opts...)` |
| `image.sample_batch(prompt, model, n, ...)` | `Images.GenerateBatch(ctx, model, prompt, n, opts...)` |
| `image.prepare(...)`, `video.prepare(...)` (batch items) | `BatchImageRequest(id, model, prompt, n, opts...)`, `BatchVideoRequest(id, model, prompt, opts...)` |
| `ImageResponse.url`, `.base64`, `.image`, `.file_output`, `.storage_error`, `.public_url`, `.public_url_error`, `.respect_moderation`, `.model`, `.usage`, `.cost_usd` | `Image.URL()`, `.Base64()`, `.Bytes()`, `.FileOutput()`, `.StorageError()`, `.PublicURL()`, `.PublicURLError()`, `.RespectModeration()`, `.Model()`, `.Usage()`, `.CostUSD()` |
| `video.generate(...)` | `Videos.Generate(ctx, model, prompt, poll, opts...)` returns `*Video` |
| `VideoResponse.url`, `.duration`, `.file_output`, `.storage_error`, `.public_url`, `.public_url_error`, `.respect_moderation`, `.model`, `.usage`, `.cost_usd` | `Video.URL()` (`ErrVideoModerated` when withheld), `.Duration()`, `.FileOutput()`, `.StorageError()`, `.PublicURL()`, `.PublicURLError()`, `.RespectModeration()`, `.Model()`, `.Usage()`, `.CostUSD()`; `Video.Proto()` is the raw `VideoResponse` |
| `BatchResult.response` as image / video | `BatchResult.Images()`, `BatchResult.VideoResult()` (raw protos stay in `.Image` / `.Video`) |
| `video.start(...)`, `video.get(id)` | `Videos.Start(...)`, `Videos.Get(ctx, id)`, `Videos.Wait(ctx, id, poll, onProgress)` |
| `video.extend(...)`, `video.extend_start(...)` | `Videos.Extend(...)`, `Videos.StartExtend(...)` |
| `batch.create(name, input_file_id)` | `Batches.Create(ctx, name, inputFileID)` |
| `batch.add(batch_id, requests)` | `Batches.Add(ctx, batchID, reqs...)` with `BatchChatRequest(id, chat)` |
| `batch.get(id)`, `batch.cancel(id)` | `Batches.Get(ctx, id)`, `Batches.Cancel(ctx, id)`, plus `Batches.Wait(ctx, id, poll)` |
| `batch.list(...)` | `Batches.All(ctx)` |
| `batch.list_batch_requests(id)` | `Batches.Requests(ctx, id)` |
| `batch.list_batch_results(id)` | `Batches.Results(ctx, id)`, `Batches.Result(ctx, id, requestID)` |
| `models.list_language_models()` and the other five | `Models.ListLanguageModels(ctx)` and the other five |
| `tokenizer.tokenize_text(text, model)` | `Tokenize.TokenizeText(ctx, text, model)` |
| `auth.get_api_key_info()` | `Auth.GetAPIKeyInfo(ctx)` |
| `Telemetry(...)` / `XAI_SDK_DISABLE_TRACING_SENSITIVE_ATTRIBUTES` | `WithTracerProvider(tp)`, `WithoutSensitiveAttributes()` |
| (no Python client for the `Embedder` service) | `Embed.Embed(ctx, model, inputs...)`, `Embed.EmbedBase64(...)` |
| `collections.create(name, ...)` | `Collections.Create(ctx, name, opts...)` (Collections is experimental: generated from recovered protos until xai-org/xai-proto#83 is resolved) |
| `collections.list(limit, order, sort_by, filter, pagination_token)`, `.get(id)`, `.update(id, ...)`, `.delete(id)` | `Collections.ListPage(ctx, order, token, WithCollectionsLimit/SortBy/Filter...)`, `Collections.All(ctx, order, opts...)`, `.Get`, `.Update(ctx, id, opts...)`, `.Delete` |
| `collections.search(query, collection_ids, ...)` | `Documents.Search(ctx, query, collectionIDs, opts...)` |
| `collections.upload_document(id, data, name, fields)` | `Collections.UploadDocument(ctx, id, name, data, fields, poll)` |
| `collections.add_existing_document(id, file_id, fields)` | `Collections.AddDocument(ctx, id, fileID, fields)` |
| `collections.list_documents(id, limit, order, sort_by, pagination_token, filter)` | `Collections.DocumentsPage(ctx, id, order, token, WithDocumentsLimit/SortBy/Filter...)`, `Collections.Documents(ctx, id, order, opts...)` |
| `collections.get_document(file_id, id)` | `Collections.GetDocument(ctx, id, fileID)`, `Collections.WaitForDocument(...)` |
| `collections.batch_get_documents(id, file_ids)` | `Collections.BatchGetDocuments(ctx, id, fileIDs...)` |
| `collections.remove_document(id, file_id)` | `Collections.RemoveDocument(ctx, id, fileID)` |
| `collections.update_document(id, file_id, ...)` | `Collections.UpdateDocument(ctx, id, fileID, DocumentUpdate{...})` |
| `collections.reindex_document(id, file_id)` | `Collections.ReindexDocument(ctx, id, fileID)` |
| `collections.generate_description(id)` | `Collections.GenerateDescription(ctx, id)` |

Differences worth knowing:

- Errors: the Python SDK raises `grpc.RpcError`; here every API failure is `*Error` with the gRPC `Code`, and `errors.Is(err, xai.ErrNotFound)` works. Invalid literals fail before the request with `*ValidationError`.
- Polling (`defer`, video, batch, document indexing) takes `PollOptions`; the zero value is a 1 s interval and a 10 min timeout, and the timeout is `ErrPollTimeout`.
- `Image.URL()` and `Image.Base64()` distinguish "returned in the other format" (`ErrImageFormat`) from "withheld by moderation" (`ErrImageModerated`); the Python SDK raises `ValueError` for both.
- `*Error` exposes `Status()` for server-attached details and `Retryable()`; the Python SDK leaves that to `grpc.RpcError`.
- Image and video storage (`storage_options`) is `WithImageStorage` / `WithVideoStorage` with a `StorageOptions` value.
- Hosts: `WithAPIHost` / `WithManagementAPIHost` also read `XAI_API_HOST` / `XAI_MANAGEMENT_API_HOST` from the environment (the Python SDK takes them as arguments only); `WithUserAgent(suffix)` appends an application id to the `User-Agent`.
- Telemetry: `WithTracerProvider` instead of the `Telemetry` config object; span names and `gen_ai.*` attributes match, `WithoutSensitiveAttributes` stands in for `XAI_SDK_DISABLE_TRACING_SENSITIVE_ATTRIBUTES`. The OTLP exporter is the application's choice; the client code imports only the OpenTelemetry API (the SDK appears in go.mod for the tests alone).
- `Sample` (the legacy sampling service) is not planned.
