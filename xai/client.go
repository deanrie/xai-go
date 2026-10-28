// Package xai is a Go client for the xAI gRPC API.
//
// The client is a thin layer over the stubs generated from xai-proto. Every
// call takes a context.Context; options are functional. The defaults match
// the Python SDK: api.x.ai:443 over TLS, the API key from XAI_API_KEY, a
// 27-minute per-call timeout, 20 MiB message limits and keepalive pings.
package xai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

const (
	// DefaultAPIHost is the production API endpoint.
	DefaultAPIHost = "api.x.ai"
	// DefaultManagementAPIHost is the production Management API endpoint.
	DefaultManagementAPIHost = "management-api.x.ai"
	// DefaultTimeout bounds every unary call and the lifetime of every
	// stream unless a request-level deadline is set on the context. The
	// Python SDK uses the same 27 minutes.
	DefaultTimeout = 27 * time.Minute

	envAPIKey           = "XAI_API_KEY"
	envManagementAPIKey = "XAI_MANAGEMENT_KEY"
	maxMessageBytes     = 20 << 20
)

// ErrMissingAPIKey is returned by New when no API key is given and
// XAI_API_KEY is not set.
var ErrMissingAPIKey = errors.New("xai: no API key: pass WithAPIKey or set " + envAPIKey)

// Option configures a Client.
type Option func(*config)

type config struct {
	apiKey            string
	managementAPIKey  string
	apiHost           string
	managementAPIHost string
	timeout           time.Duration
	metadata          metadata.MD
	insecure          bool
	dialOptions       []grpc.DialOption
}

// WithAPIKey sets the API key. The default is XAI_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithManagementAPIKey sets the Management API key. The default is
// XAI_MANAGEMENT_KEY. It is only needed for the management endpoints.
func WithManagementAPIKey(key string) Option {
	return func(c *config) { c.managementAPIKey = key }
}

// WithAPIHost sets the API host, as "host" or "host:port". Port 443 is used
// when none is given.
func WithAPIHost(host string) Option { return func(c *config) { c.apiHost = host } }

// WithManagementAPIHost sets the Management API host.
func WithManagementAPIHost(host string) Option {
	return func(c *config) { c.managementAPIHost = host }
}

// WithTimeout sets the per-call timeout applied when the caller's context
// has no deadline. Zero or negative means DefaultTimeout; there is no way to
// disable the bound, which keeps a dead connection from holding a call open
// for longer than the API itself would.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithMetadata adds gRPC metadata to every request. Later options with the
// same key append rather than replace.
func WithMetadata(md map[string]string) Option {
	return func(c *config) {
		for k, v := range md {
			c.metadata.Append(k, v)
		}
	}
}

// WithInsecure dials without TLS. The API key is still sent as metadata.
// Intended for local test servers only.
func WithInsecure() Option { return func(c *config) { c.insecure = true } }

// WithDialOptions appends raw grpc.DialOptions after the SDK's own, so they
// can override message limits, keepalive or the service config.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *config) { c.dialOptions = append(c.dialOptions, opts...) }
}

// Client holds the connection to the API and the per-call defaults.
type Client struct {
	conn    *grpc.ClientConn
	timeout time.Duration
	md      metadata.MD
}

// New creates a Client. It does not contact the API; the first call dials.
func New(opts ...Option) (*Client, error) {
	cfg := config{
		apiHost:           DefaultAPIHost,
		managementAPIHost: DefaultManagementAPIHost,
		timeout:           DefaultTimeout,
		metadata:          metadata.MD{},
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv(envAPIKey)
	}
	if strings.TrimSpace(cfg.apiKey) == "" {
		return nil, ErrMissingAPIKey
	}
	if cfg.managementAPIKey == "" {
		cfg.managementAPIKey = os.Getenv(envManagementAPIKey)
	}
	if cfg.timeout <= 0 {
		cfg.timeout = DefaultTimeout
	}

	md := cfg.metadata.Copy()
	md.Set("authorization", "Bearer "+cfg.apiKey)
	md.Append("xai-sdk-version", "go/"+Version)
	md.Append("xai-sdk-language", "go/"+strings.TrimPrefix(runtime.Version(), "go"))

	target := cfg.apiHost
	if !strings.Contains(target, ":") {
		target += ":443"
	}

	dialOpts := []grpc.DialOption{
		grpc.WithUserAgent(userAgent),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(maxMessageBytes),
			grpc.MaxCallRecvMsgSize(maxMessageBytes),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultServiceConfig(defaultServiceConfig),
	}
	if cfg.insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})))
	}
	dialOpts = append(dialOpts, cfg.dialOptions...)

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("xai: dial %s: %w", target, err)
	}
	return &Client{conn: conn, timeout: cfg.timeout, md: md}, nil
}

// Close releases the connection. The Client must not be used afterwards.
func (c *Client) Close() error { return c.conn.Close() }

// Conn exposes the underlying connection for callers who want to use the
// generated stubs directly. The connection carries the transport settings
// but not the authorization metadata; use CallContext for that.
func (c *Client) Conn() *grpc.ClientConn { return c.conn }

// CallContext returns a context with the client's metadata attached and,
// when ctx has no deadline, the client's timeout applied. The returned
// cancel must be called once the call (or stream) is finished.
func (c *Client) CallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = metadata.NewOutgoingContext(ctx, joinMetadata(ctx, c.md))
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

// joinMetadata merges metadata the caller already put on ctx with the
// client's, caller entries first.
func joinMetadata(ctx context.Context, own metadata.MD) metadata.MD {
	if existing, ok := metadata.FromOutgoingContext(ctx); ok {
		return metadata.Join(existing, own)
	}
	return own
}

// defaultServiceConfig mirrors the Python SDK's retry policy: UNAVAILABLE is
// retried up to five attempts with exponential backoff from 100 ms to 1 s.
const defaultServiceConfig = `{
  "methodConfig": [{
    "name": [{}],
    "retryPolicy": {
      "maxAttempts": 5,
      "initialBackoff": "0.1s",
      "maxBackoff": "1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`
