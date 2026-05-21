package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyp256/airouter/pkg/llm"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestOpenAIResponse(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("X-Test", "ok")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "resp_1"})
	}))
	defer server.Close()

	resp, err := (&OpenAI{}).Response(context.Background(), llm.Auth{
		Attributes: map[string]string{
			attrBaseURL:    server.URL,
			llm.AttrAPIKey: "test-key",
		},
	}, llm.Request{Payload: []byte(`{"model":"gpt-test"}`)})
	if err != nil {
		t.Fatalf("Response returned error: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Headers.Get("X-Test") != "ok" {
		t.Fatalf("missing response header")
	}
}

func TestOpenAIResponsesResponse(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "resp_1",
			"object": "response",
			"usage": map[string]int{
				"input_tokens":  1,
				"output_tokens": 2,
				"total_tokens":  3,
			},
		})
	}))
	defer server.Close()

	resp, err := (&OpenAIResponses{}).Response(context.Background(), llm.Auth{
		Attributes: map[string]string{
			attrBaseURL:    server.URL,
			llm.AttrAPIKey: "test-key",
		},
	}, llm.Request{
		Payload: []byte(`{"model":"gpt-test","input":"hi"}`),
		Format:  sdktranslator.FormatOpenAIResponse,
	})
	if err != nil {
		t.Fatalf("Response returned error: %v", err)
	}

	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestProviderFormatOpenAIResponses(t *testing.T) {
	if got := FormatForProviderType("openai_response"); got != sdktranslator.FormatOpenAIResponse {
		t.Fatalf("format = %q, want %q", got, sdktranslator.FormatOpenAIResponse)
	}
	if got := DefaultAPIPath(sdktranslator.FormatOpenAIResponse); got != "/v1/responses" {
		t.Fatalf("default path = %q, want /v1/responses", got)
	}
	if _, ok := ProviderForFormat(sdktranslator.FormatOpenAIResponse).(*OpenAIResponses); !ok {
		t.Fatalf("provider = %T, want *OpenAIResponses", ProviderForFormat(sdktranslator.FormatOpenAIResponse))
	}
}

func TestProviderFormatOpenAI(t *testing.T) {
	if got := FormatForProviderType("openai"); got != sdktranslator.FormatOpenAI {
		t.Fatalf("format = %q, want %q", got, sdktranslator.FormatOpenAI)
	}
	if got := DefaultAPIPath(sdktranslator.FormatOpenAI); got != "/v1/chat/completions" {
		t.Fatalf("default path = %q, want /v1/chat/completions", got)
	}
	if _, ok := ProviderForFormat(sdktranslator.FormatOpenAI).(*OpenAI); !ok {
		t.Fatalf("provider = %T, want *OpenAI", ProviderForFormat(sdktranslator.FormatOpenAI))
	}
}

func TestProviderFormatUnknown(t *testing.T) {
	if got := FormatForProviderType("codex"); got != sdktranslator.Format("") {
		t.Fatalf("format = %q, want empty", got)
	}
}

func TestAnthropicStream(t *testing.T) {
	var gotPath, gotKey, gotVersion, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n\ndata: {\"type\":\"message_start\"}\n\n"))
	}))
	defer server.Close()

	stream, err := (&Anthropic{}).Stream(context.Background(), llm.Auth{
		Attributes: map[string]string{
			attrBaseURL:    server.URL,
			llm.AttrAPIKey: "anthropic-key",
		},
	}, llm.Request{Payload: []byte(`{"model":"claude-test","messages":[]}`)})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}

	var chunks [][]byte
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error: %v", chunk.Err)
		}
		chunks = append(chunks, chunk.Payload)
	}

	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "anthropic-key" {
		t.Fatalf("x-api-key = %q, want anthropic-key", gotKey)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want default", gotVersion)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("accept = %q, want text/event-stream", gotAccept)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
}
