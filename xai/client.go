package xai

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
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

	envAPIKey            = "XAI_API_KEY"
	envManagementAPIKey  = "XAI_MANAGEMENT_KEY"
	envAPIHost           = "XAI_API_HOST"
	envManagementAPIHost = "XAI_MANAGEMENT_API_HOST"
	maxMessageBytes      = 20 << 20
)

// ErrMissingAPIKey is returned by New when no API key is given and
// XAI_API_KEY is not set.
var ErrMissingAPIKey = errors.New("xai: no API key: pass WithAPIKey or set " + envAPIKey)

// ErrMissingManagementAPIKey is returned by the management endpoints when no
// management key is given and XAI_MANAGEMENT_KEY is unset.
var ErrMissingManagementAPIKey = errors.New("xai: no management API key: pass WithManagementAPIKey or set " + envManagementAPIKey)

// Option configures a Client.
type Option func(*config)

type config struct {
	apiKey                string
	managementAPIKey      string
	apiHost               string
	managementAPIHost     string
	tracerProvider        trace.TracerProvider
	noSensitiveAttributes bool
	userAgentSuffix       string
	timeout               time.Duration
	metadata              metadata.MD
	insecure              bool
	dialOptions           []grpc.DialOption
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

// WithUserAgent appends an application identifier to the User-Agent the
// client sends ("XaiSdkGo/<version> <suffix>"), so server logs can tell
// applications apart. Keep it short, for example "my-service/1.4".
func WithUserAgent(suffix string) Option {
	return func(c *config) { c.userAgentSuffix = suffix }
}

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
	apiHost string

	tracerProvider        trace.TracerProvider
	noSensitiveAttributes bool

	// The management API lives on a second host with its own key. It is
	// dialled on first use so clients that never touch it pay nothing.
	mgmt struct {
		once     sync.Once
		conn     *grpc.ClientConn
		err      error
		target   string
		md       metadata.MD
		dialOpts []grpc.DialOption
	}
}

// New creates a Client. It does not contact the API; the first call dials.
func New(opts ...Option) (*Client, error) {
	cfg := config{
		apiHost:           firstNonEmpty(os.Getenv(envAPIHost), DefaultAPIHost),
		managementAPIHost: firstNonEmpty(os.Getenv(envManagementAPIHost), DefaultManagementAPIHost),
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

	md := sdkMetadata(cfg.metadata, cfg.apiKey)
	target := withDefaultPort(cfg.apiHost)

	dialOpts := []grpc.DialOption{
		grpc.WithUserAgent(strings.TrimSpace(userAgent + " " + cfg.userAgentSuffix)),
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
	c := &Client{conn: conn, timeout: cfg.timeout, md: md, apiHost: target, tracerProvider: cfg.tracerProvider, noSensitiveAttributes: cfg.noSensitiveAttributes}
	c.mgmt.target = withDefaultPort(cfg.managementAPIHost)
	c.mgmt.dialOpts = dialOpts
	if cfg.managementAPIKey != "" {
		c.mgmt.md = sdkMetadata(cfg.metadata, cfg.managementAPIKey)
	}
	return c, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sdkMetadata is the per-call metadata: caller entries, the bearer token
// and the SDK identification the API logs.
func sdkMetadata(base metadata.MD, apiKey string) metadata.MD {
	md := base.Copy()
	md.Set("authorization", "Bearer "+apiKey)
	md.Append("xai-sdk-version", "go/"+Version)
	md.Append("xai-sdk-language", "go/"+strings.TrimPrefix(runtime.Version(), "go"))
	return md
}

func withDefaultPort(host string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return host + ":443"
}

// managementConn dials the management API on first use.
func (c *Client) managementConn() (*grpc.ClientConn, error) {
	if c.mgmt.md == nil {
		return nil, ErrMissingManagementAPIKey
	}
	c.mgmt.once.Do(func() {
		c.mgmt.conn, c.mgmt.err = grpc.NewClient(c.mgmt.target, c.mgmt.dialOpts...)
		if c.mgmt.err != nil {
			c.mgmt.err = fmt.Errorf("xai: dial %s: %w", c.mgmt.target, c.mgmt.err)
		}
	})
	return c.mgmt.conn, c.mgmt.err
}

// ManagementCallContext is CallContext for the management API: it carries
// the management key instead of the API key.
func (c *Client) ManagementCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx = metadata.NewOutgoingContext(ctx, joinMetadata(ctx, c.mgmt.md))
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

// Close releases the connections. The Client must not be used afterwards.
func (c *Client) Close() error {
	err := c.conn.Close()
	if c.mgmt.conn != nil {
		if merr := c.mgmt.conn.Close(); err == nil {
			err = merr
		}
	}
	return err
}

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
