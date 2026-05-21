package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/lyp256/airouter/pkg/llm"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

var _ llm.Provider = (*OpenAI)(nil)
var _ llm.Provider = (*OpenAIResponses)(nil)

type OpenAI struct{}

type OpenAIResponses struct{}

// Response implements [llm.Provider].
func (o *OpenAI) Response(ctx context.Context, auth llm.Auth, req llm.Request) (llm.Response, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatOpenAI, false)
	resp, err := doResponse(ctx, auth, providerReq, "/v1/chat/completions", o.prepareRequest)
	if err != nil {
		return llm.Response{}, err
	}
	return translateProviderResponse(ctx, req, sdktranslator.FormatOpenAI, translatedBody, resp), nil
}

// Stream implements [llm.Provider].
func (o *OpenAI) Stream(ctx context.Context, auth llm.Auth, req llm.Request) (*llm.StreamResult, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatOpenAI, true)
	stream, err := doStream(ctx, auth, providerReq, "/v1/chat/completions", o.prepareRequest)
	if err != nil {
		return nil, err
	}
	return translateProviderStream(ctx, req, sdktranslator.FormatOpenAI, translatedBody, stream), nil
}

// Response implements [llm.Provider].
func (o *OpenAIResponses) Response(ctx context.Context, auth llm.Auth, req llm.Request) (llm.Response, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatOpenAIResponse, false)
	resp, err := doResponse(ctx, auth, providerReq, "/v1/responses", prepareOpenAIRequest)
	if err != nil {
		return llm.Response{}, err
	}
	return translateProviderResponse(ctx, req, sdktranslator.FormatOpenAIResponse, translatedBody, resp), nil
}

// Stream implements [llm.Provider].
func (o *OpenAIResponses) Stream(ctx context.Context, auth llm.Auth, req llm.Request) (*llm.StreamResult, error) {
	providerReq, translatedBody := translateProviderRequest(req, sdktranslator.FormatOpenAIResponse, true)
	stream, err := doStream(ctx, auth, providerReq, "/v1/responses", prepareOpenAIRequest)
	if err != nil {
		return nil, err
	}
	return translateProviderStream(ctx, req, sdktranslator.FormatOpenAIResponse, translatedBody, stream), nil
}

func (o *OpenAI) prepareRequest(req *http.Request, auth llm.Auth) error {
	return prepareOpenAIRequest(req, auth)
}

func prepareOpenAIRequest(req *http.Request, auth llm.Auth) error {
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
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else if strings.EqualFold(header, "Authorization") {
		req.Header.Set(header, "Bearer "+apiKey)
	} else {
		req.Header.Set(header, apiKey)
	}
	req.Header.Set("User-Agent", "airouter-openai-compat")
	applyAttributeHeaders(req, attrs)
	return nil
}
