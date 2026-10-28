package xai

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Re-exported proto types so callers do not import internal/gen.
type (
	LanguageModel        = v1.LanguageModel
	EmbeddingModel       = v1.EmbeddingModel
	ImageGenerationModel = v1.ImageGenerationModel
)

// Models lists and describes the models the API key can use.
type Models struct{ c *Client }

// Models returns the models service.
func (c *Client) Models() *Models { return &Models{c: c} }

// ListLanguageModels returns every language model available to the API key.
func (m *Models) ListLanguageModels(ctx context.Context) ([]*LanguageModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).ListLanguageModels(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetModels(), nil
}

// GetLanguageModel returns one language model by name.
func (m *Models) GetLanguageModel(ctx context.Context, name string) (*LanguageModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).GetLanguageModel(ctx, &v1.GetModelRequest{Name: name})
	return resp, wrapErr(err)
}

// ListEmbeddingModels returns every embedding model available to the API key.
func (m *Models) ListEmbeddingModels(ctx context.Context) ([]*EmbeddingModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).ListEmbeddingModels(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetModels(), nil
}

// GetEmbeddingModel returns one embedding model by name.
func (m *Models) GetEmbeddingModel(ctx context.Context, name string) (*EmbeddingModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).GetEmbeddingModel(ctx, &v1.GetModelRequest{Name: name})
	return resp, wrapErr(err)
}

// ListImageGenerationModels returns every image generation model available
// to the API key.
func (m *Models) ListImageGenerationModels(ctx context.Context) ([]*ImageGenerationModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).ListImageGenerationModels(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetModels(), nil
}

// GetImageGenerationModel returns one image generation model by name.
func (m *Models) GetImageGenerationModel(ctx context.Context, name string) (*ImageGenerationModel, error) {
	ctx, cancel := m.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewModelsClient(m.c.conn).GetImageGenerationModel(ctx, &v1.GetModelRequest{Name: name})
	return resp, wrapErr(err)
}
