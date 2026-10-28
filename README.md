# xai-go

A Go client for the [xAI API](https://docs.x.ai/) over gRPC. Pre-release: the API of this module changes without notice until v0.1.0.

The client is a thin layer over stubs generated from [xai-proto](https://github.com/xai-org/xai-proto), with the same defaults and behaviour as the official [Python SDK](https://github.com/xai-org/xai-sdk-python): `api.x.ai:443` over TLS, the API key from `XAI_API_KEY`, a 27-minute per-call bound, 20 MiB message limits, keepalive, and retries on `UNAVAILABLE`. Every call takes a `context.Context`.

## Install

```sh
go get github.com/deanrie/xai-go
```

Requires Go 1.26 or later (the gRPC dependency sets the floor).

## Use

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/deanrie/xai-go/xai"
)

func main() {
	client, err := xai.New() // reads XAI_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	models, err := client.Models().ListLanguageModels(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range models {
		fmt.Println(m.GetName())
	}
}
```

Chat:

```go
chat := client.Chat("grok-4.6", xai.WithMaxTokens(200), xai.WithReasoningEffort(xai.ReasoningEffortLow))
chat.Append(xai.System("Answer in one sentence.")).Append(xai.User(xai.Text("Why is the sky blue?")))

resp, err := chat.Sample(ctx)          // one response
fmt.Println(resp.Content(), resp.Usage())

stream := chat.Stream(ctx)             // incremental
for resp, chunk := range stream.All() {
    fmt.Print(chunk.Content())
    _ = resp // accumulated so far
}
if err := stream.Err(); err != nil { /* ... */ }

chat.AppendResponse(resp)              // continue the conversation
```

With server-side tools (`web_search`, `x_search`, `code_execution`, …) the response covers every output: `Content()` is the final assistant answer and `ToolCalls()` gathers calls from all assistant outputs.

Other services follow the same shape:

```go
file, err := client.Files().UploadFile(ctx, "report.pdf")
data, err := client.Files().Content(ctx, file.GetId())
page, err := client.Files().ListPage(ctx, 100, xai.Descending, "") // Files().All walks every page; large accounts have thousands

img, err := client.Images().Generate(ctx, "grok-imagine", "a lighthouse at dusk", xai.WithImageAspectRatio("16:9"))
url, err := img.URL()

video, err := client.Videos().Generate(ctx, "grok-imagine-video", "waves at night", xai.PollOptions{}, xai.WithVideoDuration(8))

batch, err := client.Batches().Create(ctx, "nightly", "")
req, err := xai.BatchChatRequest("q1", chat)
err = client.Batches().Add(ctx, batch.GetBatchId(), req)
batch, err = client.Batches().Wait(ctx, batch.GetBatchId(), xai.PollOptions{Timeout: time.Hour})
for result, err := range client.Batches().Results(ctx, batch.GetBatchId()) { /* result.Chat, result.Err */ }

vectors, err := client.Embed().Embed(ctx, "grok-embed", xai.EmbedText("hello"))
tokens, err := client.Tokenize().TokenizeText(ctx, "hello", "grok-4.6")
key, err := client.Auth().GetAPIKeyInfo(ctx)
```

Long-running operations (`Defer`, `Videos().Wait`, `Batches().Wait`) take `xai.PollOptions`; the zero value means a 1 s interval and a 10 min timeout, and `errors.Is(err, xai.ErrPollTimeout)` reports the timeout.

Options: `xai.WithAPIKey`, `xai.WithAPIHost`, `xai.WithTimeout`, `xai.WithMetadata`, `xai.WithInsecure` (local test servers), `xai.WithDialOptions`.

Errors from the API are `*xai.Error` with the gRPC code; `errors.Is(err, xai.ErrNotFound)` and the other sentinels work across the whole SDK. Arguments outside the accepted values fail before the request is sent with `*xai.ValidationError`, never with a silent fallback.

## Examples

`examples/chat`, `examples/stream` and `examples/tools` are small programs against the real API; each needs `XAI_API_KEY`. The whole client was exercised against `api.x.ai` with a real key (auth, models, tokenize, chat sample/stream/tools/JSON/deferred, embeddings, files, image generation, batches, error mapping); `grok-4` resolves to a current internal build, so the examples name `grok-4.6` explicitly:

```sh
XAI_API_KEY=... go run ./examples/chat "What is a lighthouse for?"
```

## Status

| Service | State |
| --- | --- |
| Models | done |
| Tokenize | done |
| Chat (sample, stream, batch, stream batch, function tools, multi-output) | done |
| Chat (deferred, stored completions, compact context) | done |
| Chat (web_search, x_search, collections_search, MCP, code_execution options) | done |
| Files (upload, concurrent upload, list, content, delete, public URLs) | done |
| Embed, Auth | done |
| Image (generate, batch, url or base64, edit inputs) | done |
| Video (start, poll, wait, extend) | done |
| Batch (chat, image and video requests; create, add, wait, results, cancel) | done |
| Collections (create, update, list, documents, upload and wait, update document, batch get, delete) | experimental: generated from recovered protos until [xai-proto#83](https://github.com/xai-org/xai-proto/issues/83) is resolved; the Go API may change when the upstream file lands |
| Documents (search with semantic, keyword, hybrid retrieval) | done |
| Sample (legacy) | not planned |

Conformance is checked against the Python SDK's fake gRPC server (`tests/server.py` in xai-sdk-python), so behaviour that the two SDKs share is tested the same way.

Coming from the Python SDK: [`docs/python-mapping.md`](docs/python-mapping.md) lists every Python method and its Go counterpart.

## Conformance tests

`tests/conformance` runs the Go client against the gRPC test server that ships with the Python SDK (`xai-sdk-python/tests/server.py`), so both SDKs are checked against the same behaviour. The tests skip unless `XAI_SDK_PYTHON_DIR` points at a checkout and `uv` is installed:

```sh
XAI_SDK_PYTHON_DIR=~/src/xai-sdk-python go test ./tests/conformance/
```

## Regenerating the stubs

`scripts/regen.sh` clones xai-proto at the commit recorded in `internal/gen/XAI_PROTO_COMMIT`, runs `buf generate` with the Go plugins from xai-proto's `buf.gen.yaml`, and copies the result into `internal/gen`. CI checks that the committed stubs match.

`collections.proto`, `shared.proto` and `types.proto` are not published in xai-proto ([xai-proto#83](https://github.com/xai-org/xai-proto/issues/83)). Until they are, `regen.sh` takes them from [`deanrie/xai-proto@collections-from-descriptor`](https://github.com/deanrie/xai-proto/tree/collections-from-descriptor), where they are rendered from the descriptors compiled into the Python SDK and checked identical to them; the commit is recorded in `internal/gen/RECOVERED_PROTO_COMMIT`. They generate into their own Go packages (`collections`, `shared`, `rag`) because their proto packages differ from `xai_api`.

## License

Apache-2.0. The generated code under `internal/gen` derives from [xai-proto](https://github.com/xai-org/xai-proto) (Apache-2.0, X.AI LLC); see `NOTICE`. This is not an official xAI SDK.
