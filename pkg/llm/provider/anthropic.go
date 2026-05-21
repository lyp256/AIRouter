package provider

import (
	"context"
	"net/http"

	"github.com/lyp256/airouter/pkg/llm"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

var _ llm.Provider = (*Anthropic)(nil)

type Anthropic struct{}

// Response implements [llm.Provider].
func (o *Anthropic) Response(ctx context.Context, auth llm.Auth, req llm.Request) (llm.Response, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatClaude, false)
	resp, err := doResponse(ctx, auth, providerReq, "/v1/messages", o.prepareRequest)
	if err != nil {
		return llm.Response{}, err
	}
	return translateProviderResponse(ctx, req, sdktranslator.FormatClaude, translatedBody, resp), nil
}

// Stream implements [llm.Provider].
func (o *Anthropic) Stream(ctx context.Context, auth llm.Auth, req llm.Request) (*llm.StreamResult, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatClaude, true)
	stream, err := doStream(ctx, auth, providerReq, "/v1/messages", o.prepareRequest)
	if err != nil {
		return nil, err
	}
	return translateProviderStream(ctx, req, sdktranslator.FormatClaude, translatedBody, stream), nil
}

func (o *Anthropic) prepareRequest(req *http.Request, auth llm.Auth) error {
	if req == nil {
		return nil
	}
	attrs := auth.Attributes
	apiKey := attr(attrs, llm.AttrAPIKey)
	if apiKey == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "missing provider api_key"}
	}
	header := attr(attrs, llm.AttrAPIKeyHeader)
	if header == "" {
		header = "x-api-key"
	}
	req.Header.Set(header, apiKey)

	version := attr(attrs, attrAPIVersion)
	if version == "" {
		version = "2023-06-01"
	}
	req.Header.Set("anthropic-version", version)
	if beta := attr(attrs, attrAnthropicBeta); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
	applyAttributeHeaders(req, attrs)
	return nil
}
