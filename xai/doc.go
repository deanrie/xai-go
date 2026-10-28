// Package xai is a Go client for the xAI API over gRPC.
//
// A Client is created with New and holds one connection to api.x.ai (and,
// on first use, one to management-api.x.ai). Services hang off the client:
//
//	client, err := xai.New() // XAI_API_KEY
//	chat := client.Chat("grok-4").Append(xai.User(xai.Text("hello")))
//	resp, err := chat.Sample(ctx)
//
// Every call takes a context.Context. When the context carries no deadline
// the client's timeout (DefaultTimeout, 27 minutes, matching the official
// Python SDK) applies. API failures are *Error values carrying the gRPC
// status code; errors.Is works against the sentinels ErrNotFound,
// ErrUnauthenticated and so on. Arguments outside the accepted set fail
// before any request is sent with *ValidationError, never with a silent
// fallback.
//
// Long operations (Chat.Defer, Videos.Wait, Batches.Wait) poll with
// PollOptions; the zero value means DefaultPollInterval and
// DefaultPollTimeout.
//
// The protobuf types the API speaks are generated from xai-proto and
// exposed as type aliases (Message, Tool, SamplingUsage, ...), so callers
// can reach any field without the SDK wrapping each one.
package xai
