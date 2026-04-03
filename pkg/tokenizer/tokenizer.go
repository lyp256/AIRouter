package tokenizer

import (
	"fmt"
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tokenizers = make(map[string]*tiktoken.Tiktoken)
	mu         sync.RWMutex
)

const (
	// DefaultEncoding 默认使用 GPT-3.5/GPT-4 广泛使用的编码
	DefaultEncoding = "cl100k_base"
)

// GetTokens 获取字符串的 Token 数量
func GetTokens(model string, text string) int {
	if text == "" {
		return 0
	}

	encoding := getEncodingForModel(model)
	tke, err := getTokenizer(encoding)
	if err != nil {
		// 回退到基于长度的估算（作为保底）
		return len(text) / 4
	}

	token := tke.Encode(text, nil, nil)
	return len(token)
}

// getEncodingForModel 根据模型名匹配编码
func getEncodingForModel(model string) string {
	model = strings.ToLower(model)

	// OpenAI o1/4o 家族使用 o200k_base
	if strings.Contains(model, "o1") || strings.Contains(model, "4o") {
		return "o200k_base"
	}

	// GPT-4/GPT-3.5 使用 cl100k_base
	if strings.Contains(model, "gpt-4") || strings.Contains(model, "gpt-3.5") {
		return "cl100k_base"
	}

	// DeepSeek, Claude 3/3.5 等大多也兼容 cl100k_base 的 BPE 逻辑
	return DefaultEncoding
}

func getTokenizer(encoding string) (*tiktoken.Tiktoken, error) {
	mu.RLock()
	if tke, ok := tokenizers[encoding]; ok {
		mu.RUnlock()
		return tke, nil
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()

	// 再次检查，避免双重加载
	if tke, ok := tokenizers[encoding]; ok {
		return tke, nil
	}

	tke, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("get encoding %s failed: %w", encoding, err)
	}

	tokenizers[encoding] = tke
	return tke, nil
}
