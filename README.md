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
chat := client.Chat("grok-4", xai.WithMaxTokens(200), xai.WithReasoningEffort(xai.ReasoningEffortLow))
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

Options: `xai.WithAPIKey`, `xai.WithAPIHost`, `xai.WithTimeout`, `xai.WithMetadata`, `xai.WithInsecure` (local test servers), `xai.WithDialOptions`.

Errors from the API are `*xai.Error` with the gRPC code; `errors.Is(err, xai.ErrNotFound)` and the other sentinels work across the whole SDK. Arguments outside the accepted values fail before the request is sent with `*xai.ValidationError`, never with a silent fallback.

## Status

| Service | State |
| --- | --- |
| Models | done |
| Tokenize | done |
| Chat (sample, stream, batch, function tools, multi-output) | done |
| Chat (deferred, stored completions, server-side tools) | done |
| Chat (compact context, search parameters, MCP tool options) | planned |
| Files (upload, list, content, delete) | done |
| Collections, Documents | planned |
| Embed, Auth | done |
| Batch | planned |
| Image (generate, batch, url or base64, edit inputs) | done |
| Video, Sample | planned |

Conformance is checked against the Python SDK's fake gRPC server (`tests/server.py` in xai-sdk-python), so behaviour that the two SDKs share is tested the same way.

## Conformance tests

`tests/conformance` runs the Go client against the gRPC test server that ships with the Python SDK (`xai-sdk-python/tests/server.py`), so both SDKs are checked against the same behaviour. The tests skip unless `XAI_SDK_PYTHON_DIR` points at a checkout and `uv` is installed:

```sh
XAI_SDK_PYTHON_DIR=~/src/xai-sdk-python go test ./tests/conformance/
```

## Regenerating the stubs

`scripts/regen.sh` clones xai-proto at the commit recorded in `internal/gen/XAI_PROTO_COMMIT`, runs `buf generate` with the Go plugins from xai-proto's `buf.gen.yaml`, and copies the result into `internal/gen`. CI checks that the committed stubs match.

## License

Apache-2.0, like the other xAI SDKs.
