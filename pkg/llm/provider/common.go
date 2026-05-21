package provider

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lyp256/airouter/pkg/llm"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

const (
	attrBaseURL        = "base_url"
	attrAPIVersion     = "api_version"
	attrAnthropicBeta  = "anthropic_beta"
	headerAttrPrefix   = "header:"
	headersAttrPrefix  = "headers."
	defaultMaxScanSize = 52_428_800
)

type prepareRequestFunc func(*http.Request, llm.Auth) error

type statusErr struct {
	code       int
	msg        string
	retryAfter *time.Duration
}

func (e statusErr) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("status %d", e.code)
}

func (e statusErr) StatusCode() int            { return e.code }
func (e statusErr) RetryAfter() *time.Duration { return e.retryAfter }

func doResponse(ctx context.Context, auth llm.Auth, req llm.Request, defaultPath string, prepare prepareRequestFunc) (llm.Response, error) {
	httpResp, err := doHTTPRequest(ctx, auth, req.Payload, defaultPath, prepare, false)
	if err != nil {
		return llm.Response{}, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return llm.Response{}, err
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return llm.Response{}, statusErr{code: httpResp.StatusCode, msg: string(body)}
	}
	return llm.Response{
		Payload:    body,
		Headers:    httpResp.Header.Clone(),
		StatusCode: httpResp.StatusCode,
	}, nil
}

func doStream(ctx context.Context, auth llm.Auth, req llm.Request, defaultPath string, prepare prepareRequestFunc) (*llm.StreamResult, error) {
	httpResp, err := doHTTPRequest(ctx, auth, req.Payload, defaultPath, prepare, true)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(httpResp.Body)
		_ = httpResp.Body.Close()
		return nil, statusErr{code: httpResp.StatusCode, msg: string(body)}
	}

	out := make(chan llm.StreamChunk)
	go func() {
		defer close(out)
		defer httpResp.Body.Close()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, defaultMaxScanSize)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			select {
			case out <- llm.StreamChunk{Payload: bytes.Clone(line)}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case out <- llm.StreamChunk{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return &llm.StreamResult{
		Headers: httpResp.Header.Clone(),
		Chunks:  out,
	}, nil
}

func doHTTPRequest(ctx context.Context, auth llm.Auth, payload []byte, defaultPath string, prepare prepareRequestFunc, stream bool) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL := strings.TrimSpace(auth.Attributes[attrBaseURL])
	if baseURL == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing provider baseURL"}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL(baseURL, requestPath(auth, defaultPath)), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")
	}
	if prepare != nil {
		if err := prepare(httpReq, auth); err != nil {
			return nil, err
		}
	}
	return newHTTPClient(ctx, auth, 0).Do(httpReq)
}

func requestPath(auth llm.Auth, defaultPath string) string {
	if path := strings.TrimSpace(auth.APIPath); path != "" {
		return path
	}
	return defaultPath
}

func endpointURL(baseURL, endpoint string) string {
	baseURL = strings.TrimSpace(baseURL)
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return strings.TrimRight(baseURL, "/")
	}
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return strings.TrimRight(baseURL, "/") + endpoint
}

func newHTTPClient(ctx context.Context, auth llm.Auth, timeout time.Duration) *http.Client {
	httpClient := &http.Client{}
	if timeout > 0 {
		httpClient.Timeout = timeout
	}

	if proxyURL := strings.TrimSpace(auth.ProxyURL); proxyURL != "" {
		transport, _, err := proxyutil.BuildHTTPTransport(proxyURL)
		if err != nil {
			log.Errorf("%v", err)
		} else if transport != nil {
			httpClient.Transport = transport
			return httpClient
		}
	}

	if ctx != nil {
		if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
			httpClient.Transport = rt
		}
	}
	return httpClient
}

func applyAttributeHeaders(req *http.Request, attrs map[string]string) {
	if req == nil || len(attrs) == 0 {
		return
	}
	for key, value := range attrs {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || isProviderAttr(key) {
			continue
		}
		headerName, ok := customHeaderName(key)
		if !ok {
			headerName = key
		}
		if headerName == "" {
			continue
		}
		if http.CanonicalHeaderKey(headerName) == "Host" {
			req.Host = value
		}
		req.Header.Set(headerName, value)
	}
}

func customHeaderName(key string) (string, bool) {
	lower := strings.ToLower(key)
	switch {
	case strings.HasPrefix(lower, headerAttrPrefix):
		return strings.TrimSpace(key[len(headerAttrPrefix):]), true
	case strings.HasPrefix(lower, headersAttrPrefix):
		return strings.TrimSpace(key[len(headersAttrPrefix):]), true
	default:
		return "", false
	}
}

func isProviderAttr(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case attrBaseURL, llm.AttrAPIKey, llm.AttrAPIKeyHeader, attrAPIVersion, attrAnthropicBeta:
		return true
	default:
		return false
	}
}

func attr(attrs map[string]string, key string) string {
	if attrs == nil {
		return ""
	}
	return strings.TrimSpace(attrs[key])
}
