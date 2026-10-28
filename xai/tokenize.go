package xai

import (
	"context"

	v1 "github.com/deanrie/xai-go/internal/gen/xai/api/v1"
)

// Token is one token of a tokenized text.
type Token = v1.Token

// Tokenize splits text into the tokens a model sees.
type Tokenize struct{ c *Client }

// Tokenize returns the tokenize service.
func (c *Client) Tokenize() *Tokenize { return &Tokenize{c: c} }

// TokenizeText tokenizes text with the tokenizer of the given model.
func (t *Tokenize) TokenizeText(ctx context.Context, text, model string) ([]*Token, error) {
	ctx, cancel := t.c.CallContext(ctx)
	defer cancel()
	resp, err := v1.NewTokenizeClient(t.c.conn).TokenizeText(ctx, &v1.TokenizeTextRequest{Text: text, Model: model})
	if err != nil {
		return nil, wrapErr(err)
	}
	return resp.GetTokens(), nil
}
