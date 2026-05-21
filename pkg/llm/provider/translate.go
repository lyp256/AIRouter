package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lyp256/airouter/internal/model"
	"github.com/lyp256/airouter/pkg/anthropic"
	"github.com/lyp256/airouter/pkg/llm"
	"github.com/lyp256/airouter/pkg/openai"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	_ "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator/builtin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TranslationRequest describes a client request being translated for an upstream provider.
type TranslationRequest struct {
	SourceFormat  sdktranslator.Format
	TargetFormat  sdktranslator.Format
	ClientModel   string
	ProviderModel string
	RawBody       []byte
	Stream        bool
}

// FormatForProviderType returns the wire format used by a provider type.
func FormatForProviderType(providerType string) sdktranslator.Format {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case model.ProviderTypeOpenAI:
		return sdktranslator.FormatOpenAI
	case "openai_chatcompletions", "openai_chat_completions", "openai_compatible":
		return sdktranslator.FormatOpenAI
	case model.ProviderTypeOpenAIResponse:
		return sdktranslator.FormatOpenAIResponse
	case model.ProviderTypeAnthropic:
		return sdktranslator.FormatClaude
	default:
		return sdktranslator.Format("")
	}
}

// DefaultAPIPath returns the default request path for a provider format.
func DefaultAPIPath(format sdktranslator.Format) string {
	switch format {
	case sdktranslator.FormatClaude:
		return "/v1/messages"
	case sdktranslator.FormatOpenAIResponse, sdktranslator.FormatCodex:
		return "/v1/responses"
	default:
		return "/v1/chat/completions"
	}
}

// ProviderForFormat returns the HTTP provider implementation for a translated format.
func ProviderForFormat(format sdktranslator.Format) llm.Provider {
	switch format {
	case sdktranslator.FormatClaude:
		return &Anthropic{}
	case sdktranslator.FormatOpenAIResponse:
		return &OpenAIResponses{}
	default:
		return &OpenAI{}
	}
}

// TranslateRequest converts the client request body to the upstream provider format.
func TranslateRequest(req TranslationRequest) []byte {
	body := sdktranslator.TranslateRequest(req.SourceFormat, req.TargetFormat, req.ProviderModel, req.RawBody, req.Stream)
	if req.TargetFormat == sdktranslator.FormatOpenAI && req.Stream {
		body, _ = sjson.SetBytes(body, "stream", true)
		body, _ = sjson.SetBytes(body, "stream_options.include_usage", true)
	}
	return body
}

// TranslateNonStream converts a non-streaming upstream response back to the client format.
func TranslateNonStream(ctx context.Context, req TranslationRequest, translatedBody, upstreamPayload []byte, param *any) []byte {
	return sdktranslator.TranslateNonStream(ctx, req.TargetFormat, req.SourceFormat, req.ClientModel, req.RawBody, translatedBody, upstreamPayload, param)
}

// TranslateStream converts a streaming upstream chunk back to the client format.
func TranslateStream(ctx context.Context, req TranslationRequest, translatedBody []byte, upstreamChunk []byte, param *any) [][]byte {
	return sdktranslator.TranslateStream(ctx, req.TargetFormat, req.SourceFormat, req.ClientModel, req.RawBody, translatedBody, bytes.Clone(upstreamChunk), param)
}

// TranslateStreamDone emits any translator-specific terminal chunks.
func TranslateStreamDone(ctx context.Context, req TranslationRequest, translatedBody []byte, param *any) [][]byte {
	return sdktranslator.TranslateStream(ctx, req.TargetFormat, req.SourceFormat, req.ClientModel, req.RawBody, translatedBody, []byte("data: [DONE]"), param)
}

// NeedsClientDoneMarker reports whether the handler should append an SSE DONE marker.
func NeedsClientDoneMarker(format sdktranslator.Format) bool {
	return format == sdktranslator.FormatOpenAI
}

func translateProviderRequest(req llm.Request, targetFormat sdktranslator.Format, stream bool) (llm.Request, []byte) {
	sourceFormat := sourceFormatOrTarget(req.Format, targetFormat)
	translatedBody := TranslateRequest(TranslationRequest{
		SourceFormat:  sourceFormat,
		TargetFormat:  targetFormat,
		ClientModel:   clientModel(req),
		ProviderModel: req.Model,
		RawBody:       req.Payload,
		Stream:        stream,
	})
	req.Payload = translatedBody
	req.Format = targetFormat
	return req, translatedBody
}

func translateProviderResponse(ctx context.Context, req llm.Request, targetFormat sdktranslator.Format, translatedBody []byte, resp llm.Response) llm.Response {
	sourceFormat := sourceFormatOrTarget(req.Format, targetFormat)
	var param any
	resp.Payload = TranslateNonStream(ctx, TranslationRequest{
		SourceFormat:  sourceFormat,
		TargetFormat:  targetFormat,
		ClientModel:   clientModel(req),
		ProviderModel: req.Model,
		RawBody:       req.Payload,
		Stream:        false,
	}, translatedBody, resp.Payload, &param)
	return resp
}

func translateProviderStream(ctx context.Context, req llm.Request, targetFormat sdktranslator.Format, translatedBody []byte, stream *llm.StreamResult) *llm.StreamResult {
	if stream == nil {
		return nil
	}
	sourceFormat := sourceFormatOrTarget(req.Format, targetFormat)
	translationReq := TranslationRequest{
		SourceFormat:  sourceFormat,
		TargetFormat:  targetFormat,
		ClientModel:   clientModel(req),
		ProviderModel: req.Model,
		RawBody:       req.Payload,
		Stream:        true,
	}
	out := make(chan llm.StreamChunk)
	go func() {
		defer close(out)
		var param any
		for chunk := range stream.Chunks {
			if chunk.Err != nil {
				select {
				case out <- chunk:
				case <-ctx.Done():
				}
				return
			}
			line := bytes.TrimSpace(chunk.Payload)
			if len(line) == 0 || isSSEMetadataLine(line) {
				continue
			}
			if !bytes.HasPrefix(line, []byte("data:")) && targetFormat == sdktranslator.FormatOpenAI {
				if bytes.HasPrefix(line, []byte("{")) || bytes.HasPrefix(line, []byte("[")) {
					select {
					case out <- llm.StreamChunk{Err: statusErr{code: http.StatusBadGateway, msg: string(line)}}:
					case <-ctx.Done():
					}
					return
				}
				continue
			}
			chunks := TranslateStream(ctx, translationReq, translatedBody, line, &param)
			for _, payload := range chunks {
				select {
				case out <- llm.StreamChunk{Payload: payload}:
				case <-ctx.Done():
					return
				}
			}
		}
		for _, payload := range TranslateStreamDone(ctx, translationReq, translatedBody, &param) {
			select {
			case out <- llm.StreamChunk{Payload: payload}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return &llm.StreamResult{Headers: stream.Headers, Chunks: out}
}

func sourceFormatOrTarget(source, target sdktranslator.Format) sdktranslator.Format {
	if strings.TrimSpace(source.String()) == "" {
		return target
	}
	return source
}

func clientModel(req llm.Request) string {
	if strings.TrimSpace(req.ClientModel) != "" {
		return req.ClientModel
	}
	return req.Model
}

func isSSEMetadataLine(line []byte) bool {
	return bytes.HasPrefix(line, []byte("event:")) ||
		bytes.HasPrefix(line, []byte("id:")) ||
		bytes.HasPrefix(line, []byte("retry:")) ||
		bytes.HasPrefix(line, []byte(":"))
}

// ParseUsage extracts OpenAI-style usage from a translated response payload.
func ParseUsage(format sdktranslator.Format, payload []byte) openai.Usage {
	data := SSEDataPayload(payload)
	if bytes.Equal(data, []byte("[DONE]")) || len(data) == 0 {
		return openai.Usage{}
	}
	switch format {
	case sdktranslator.FormatClaude:
		var resp anthropic.MessagesResponse
		if err := json.Unmarshal(data, &resp); err == nil && resp.Usage != nil {
			return openai.Usage{
				PromptTokens:     resp.Usage.InputTokens,
				CompletionTokens: resp.Usage.OutputTokens,
				TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
			}
		}
		var event anthropic.StreamEvent
		if err := json.Unmarshal(data, &event); err == nil {
			if event.Type == "message_start" && event.Message != nil && event.Message.Usage != nil {
				return openai.Usage{PromptTokens: event.Message.Usage.InputTokens}
			}
			if event.Type == "message_delta" && event.DeltaUsage != nil {
				return openai.Usage{
					PromptTokens:     event.DeltaUsage.InputTokens,
					CompletionTokens: event.DeltaUsage.OutputTokens,
					TotalTokens:      event.DeltaUsage.InputTokens + event.DeltaUsage.OutputTokens,
				}
			}
		}
	case sdktranslator.FormatOpenAIResponse:
		return parseResponsesUsage(data)
	default:
		var chatResp openai.ChatCompletionResponse
		if err := json.Unmarshal(data, &chatResp); err == nil && chatResp.Usage != nil {
			return *chatResp.Usage
		}
		var chunk openai.StreamChunk
		if err := json.Unmarshal(data, &chunk); err == nil && chunk.Usage != nil {
			return *chunk.Usage
		}
	}
	return openai.Usage{}
}

func parseResponsesUsage(data []byte) openai.Usage {
	root := gjson.ParseBytes(data)
	usage := root.Get("usage")
	if !usage.Exists() {
		usage = root.Get("response.usage")
	}
	if !usage.Exists() {
		return openai.Usage{}
	}
	inputTokens := int(usage.Get("input_tokens").Int())
	outputTokens := int(usage.Get("output_tokens").Int())
	totalTokens := int(usage.Get("total_tokens").Int())
	if totalTokens == 0 {
		totalTokens = inputTokens + outputTokens
	}
	return openai.Usage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      totalTokens,
	}
}

// MergeUsage merges partial usage updates emitted during streaming.
func MergeUsage(current, next openai.Usage) openai.Usage {
	if next.PromptTokens > 0 {
		current.PromptTokens = next.PromptTokens
	}
	if next.CompletionTokens > 0 {
		current.CompletionTokens = next.CompletionTokens
	}
	if next.TotalTokens > 0 {
		current.TotalTokens = next.TotalTokens
	} else {
		current.TotalTokens = current.PromptTokens + current.CompletionTokens
	}
	return current
}

// StreamChunkHasContent reports whether a translated stream payload contains visible model output.
func StreamChunkHasContent(format sdktranslator.Format, payload []byte) bool {
	data := SSEDataPayload(payload)
	switch format {
	case sdktranslator.FormatClaude:
		var event anthropic.StreamEvent
		return json.Unmarshal(data, &event) == nil &&
			event.Type == "content_block_delta" &&
			event.Delta != nil &&
			(event.Delta.Text != "" || event.Delta.Thinking != "")
	case sdktranslator.FormatOpenAIResponse:
		root := gjson.ParseBytes(data)
		switch root.Get("type").String() {
		case "response.output_text.delta", "response.reasoning_summary_text.delta":
			return root.Get("delta").String() != ""
		default:
			return false
		}
	default:
		var chunk openai.StreamChunk
		if json.Unmarshal(data, &chunk) != nil {
			return false
		}
		for _, choice := range chunk.Choices {
			if choice.Delta != nil && (choice.Delta.Content != "" || choice.Delta.ReasoningContent != "") {
				return true
			}
		}
	}
	return false
}

// SSEDataPayload extracts the data payload from an SSE frame.
func SSEDataPayload(payload []byte) []byte {
	data := bytes.TrimSpace(payload)
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			return bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		}
	}
	return data
}
