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

Options: `xai.WithAPIKey`, `xai.WithAPIHost`, `xai.WithTimeout`, `xai.WithMetadata`, `xai.WithInsecure` (local test servers), `xai.WithDialOptions`.

Errors from the API are `*xai.Error` with the gRPC code; `errors.Is(err, xai.ErrNotFound)` and the other sentinels work across the whole SDK. Arguments outside the accepted values fail before the request is sent with `*xai.ValidationError`, never with a silent fallback.

## Status

| Service | State |
| --- | --- |
| Models | done |
| Tokenize | done |
| Chat (create, stream, tools, defer, batch) | in progress |
| Files, Collections, Documents | planned |
| Batch, Embed, Auth | planned |
| Image, Video, Sample | planned |

Conformance is checked against the Python SDK's fake gRPC server (`tests/server.py` in xai-sdk-python), so behaviour that the two SDKs share is tested the same way.

## Regenerating the stubs

`scripts/regen.sh` clones xai-proto at the commit recorded in `internal/gen/XAI_PROTO_COMMIT`, runs `buf generate` with the Go plugins from xai-proto's `buf.gen.yaml`, and copies the result into `internal/gen`. CI checks that the committed stubs match.

## License

Apache-2.0, like the other xAI SDKs.
