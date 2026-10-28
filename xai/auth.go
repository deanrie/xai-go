package xai

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// APIKey describes the key the client authenticates with.
type APIKey = v1.ApiKey

// Auth answers questions about the current API key.
type Auth struct{ c *Client }

// Auth returns the auth service.
func (c *Client) Auth() *Auth { return &Auth{c: c} }

// GetAPIKeyInfo returns the redacted key, its name, ACLs and status.
func (a *Auth) GetAPIKeyInfo(ctx context.Context) (*APIKey, error) {
	ctx, cancel := a.c.CallContext(ctx)
	defer cancel()
	key, err := v1.NewAuthClient(a.c.conn).GetApiKeyInfo(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, wrapErr(err)
	}
	return key, nil
}
