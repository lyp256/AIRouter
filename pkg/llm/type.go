package llm

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

type Request struct {
	// Model is the upstream model identifier after translation.
	Model string
	// ClientModel is the model identifier from the inbound client request.
	ClientModel string
	// Payload is the inbound JSON payload before provider-specific translation.
	Payload []byte
	// Format represents the inbound payload schema.
	Format translator.Format
}

// StreamChunk represents a single streaming payload unit emitted by provider executors.
type StreamChunk struct {
	// Payload is the raw provider chunk payload.
	Payload []byte
	// Err reports any terminal error encountered while producing chunks.
	Err error
}

// StreamResult wraps the streaming response, providing both the chunk channel
// and the upstream HTTP response headers captured before streaming begins.
type StreamResult struct {
	// Headers carries upstream HTTP response headers from the initial connection.
	Headers http.Header
	// Chunks is the channel of streaming payload units.
	Chunks <-chan StreamChunk
}

// Response wraps either a full provider response or metadata for streaming flows.
type Response struct {
	// Payload is the provider response in the executor format.
	Payload []byte
	// Headers carries upstream HTTP response headers.
	Headers http.Header
	// StatusCode is the upstream HTTP status code.
	StatusCode int
}

// Auth encapsulates the runtime state and metadata associated with a single credential.
type Auth struct {
	APIPath string
	// http 或 socks 代理
	ProxyURL   string
	Attributes map[string]string
}

const (
	AttrAPIKey       = "api_key"
	AttrAPIKeyHeader = "api_key_header"
)

type Provider interface {
	// 发送流式请求
	Stream(ctx context.Context, auth Auth, req Request) (*StreamResult, error)
	Response(ctx context.Context, auth Auth, req Request) (Response, error)
}
