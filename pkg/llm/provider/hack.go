package provider

import (
	"sync"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/translator/builtin"
)

type registry struct {
	mu        sync.RWMutex
	requests  map[translator.Format]map[translator.Format]translator.RequestTransform
	responses map[translator.Format]map[translator.Format]translator.ResponseTransform
}

func init() {
	v := builtin.Registry()
	reg := *(*registry)(unsafe.Pointer(v))

	// 目前 github.com/router-for-me/CLIProxyAPI 中没有
	// openai-response 的 provider 只有 codex,需要重新注
	// 册一个 codex 的别名来实现 openai-response 的转换
	for _, toMap := range reg.requests {
		if _, ok := toMap[translator.FormatOpenAIResponse]; ok {
			continue
		}
		if fn, ok := toMap[translator.FormatCodex]; ok {
			toMap[translator.FormatOpenAIResponse] = fn
		}
	}
	for _, toMap := range reg.responses {
		if _, ok := toMap[translator.FormatOpenAIResponse]; ok {
			continue
		}
		if fn, ok := toMap[translator.FormatCodex]; ok {
			toMap[translator.FormatOpenAIResponse] = fn
		}
	}
}
