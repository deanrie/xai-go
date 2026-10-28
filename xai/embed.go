package xai

import (
	"context"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Embedding is the vectors computed for one input, in input order.
type Embedding = v1.Embedding

// EmbedResponse is the full embedding response including usage.
type EmbedResponse = v1.EmbedResponse

// EmbedInput is one item to embed: text or an image.
type EmbedInput = v1.EmbedInput

// EmbedText returns a text input for Embed.
func EmbedText(s string) *EmbedInput {
	return &EmbedInput{Input: &v1.EmbedInput_String_{String_: s}}
}

// EmbedImage returns an image input for Embed; detail follows ImageURL.
func EmbedImage(url string, detail ImageDetail) (*EmbedInput, error) {
	c, err := ImageURL(url, detail)
	if err != nil {
		return nil, err
	}
	return &EmbedInput{Input: &v1.EmbedInput_ImageUrl{ImageUrl: c.GetImageUrl()}}, nil
}

// Embedder computes embeddings.
type Embedder struct{ c *Client }

// Embed returns the embedding service.
func (c *Client) Embed() *Embedder { return &Embedder{c: c} }

// Embed computes float embeddings for the inputs with the given model.
// Vectors come back as float32 slices; use EmbedBase64 for the compact
// encoding.
func (e *Embedder) Embed(ctx context.Context, model string, inputs ...*EmbedInput) (*EmbedResponse, error) {
	return e.embed(ctx, model, v1.EmbedEncodingFormat_FORMAT_FLOAT, inputs)
}

// EmbedBase64 computes embeddings encoded as base64 strings.
func (e *Embedder) EmbedBase64(ctx context.Context, model string, inputs ...*EmbedInput) (*EmbedResponse, error) {
	return e.embed(ctx, model, v1.EmbedEncodingFormat_FORMAT_BASE64, inputs)
}

func (e *Embedder) embed(ctx context.Context, model string, format v1.EmbedEncodingFormat, inputs []*EmbedInput) (*EmbedResponse, error) {
	if len(inputs) == 0 {
		return nil, &ValidationError{Param: "inputs", Value: 0, Accepted: []string{"1 or more inputs"}}
	}
	ctx, cancel := e.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewEmbedderClient(e.c.conn).Embed(ctx, &v1.EmbedRequest{
		Input:          inputs,
		Model:          model,
		EncodingFormat: format,
	})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp, nil
}
