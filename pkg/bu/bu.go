// Package bu 提供基本计量单位（BU）的定义和转换函数
// BU（Basic Unit）是系统中的抽象计量单位，用于统一表示价格、配额和费用
// 精度定义：1 BU = 10^9 纳 BU（最小单位）
// 换算关系：1000 纳 = 1 微，1000 微 = 1 毫，1000 毫 = 1 BU
package bu

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// 单位常量
const (
	// Nano 纳 BU（最小单位）
	Nano int64 = 1
	// Micro 微 BU = 1000 纳
	Micro int64 = 1000 * Nano
	// Milli 毫 BU = 1000 微 = 1,000,000 纳
	Milli int64 = 1000 * Micro
	// Unit 1 BU = 1000 毫 = 1,000,000 微 = 1,000,000,000 纳
	Unit int64 = 1000 * Milli
)

// FromFloat 将浮点数 BU 转换为 int64 纳 BU
// 例如：1.5 -> 1,500,000,000 纳 BU
func FromFloat(value float64) int64 {
	return int64(math.Round(value * float64(Unit)))
}

// ToFloat 将 int64 纳 BU 转换为浮点数 BU
// 例如：1,500,000,000 -> 1.5
func ToFloat(value int64) float64 {
	return float64(value) / float64(Unit)
}

// FromMilli 将毫 BU 转换为纳 BU
// 例如：1500 毫 -> 1,500,000,000 纳
func FromMilli(milli int64) int64 {
	return milli * Milli
}

// ToMilli 将纳 BU 转换为毫 BU
// 例如：1,500,000,000 纳 -> 1500 毫
func ToMilli(value int64) int64 {
	return value / Milli
}

// FromMicro 将微 BU 转换为纳 BU
// 例如：1,500,000 微 -> 1,500,000,000 纳
func FromMicro(micro int64) int64 {
	return micro * Micro
}

// ToMicro 将纳 BU 转换为微 BU
// 例如：1,500,000,000 纳 -> 1,500,000 微
func ToMicro(value int64) int64 {
	return value / Micro
}

// Format 格式化 BU 值为可读字符串
// 自动选择合适的单位（BU、毫、微、纳）
func Format(value int64) string {
	if value >= Unit {
		return fmt.Sprintf("%.2f BU", ToFloat(value))
	} else if value >= Milli {
		return fmt.Sprintf("%.2f mBU", float64(value)/float64(Milli))
	} else if value >= Micro {
		return fmt.Sprintf("%.2f µBU", float64(value)/float64(Micro))
	}
	return fmt.Sprintf("%d nBU", value)
}

// FormatShort 格式化 BU 值为简短字符串（最多2位小数）
func FormatShort(value int64) string {
	if value >= Unit {
		f := ToFloat(value)
		if f == math.Trunc(f) {
			return fmt.Sprintf("%.0f BU", f)
		}
		return fmt.Sprintf("%.2f BU", f)
	} else if value >= Milli {
		f := float64(value) / float64(Milli)
		if f == math.Trunc(f) {
			return fmt.Sprintf("%.0f mBU", f)
		}
		return fmt.Sprintf("%.2f mBU", f)
	} else if value >= Micro {
		f := float64(value) / float64(Micro)
		if f == math.Trunc(f) {
			return fmt.Sprintf("%.0f µBU", f)
		}
		return fmt.Sprintf("%.2f µBU", f)
	}
	return fmt.Sprintf("%d nBU", value)
}

// Parse 解析 BU 字符串为纳 BU 值
// 支持格式：1.5BU, 1.5 BU, 1500mBU, 1500 mBU, 1500000µBU, 1500000nBU
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	var multiplier int64 = Unit
	var numStr string

	switch {
	case strings.HasSuffix(s, "nbu"):
		multiplier = Nano
		numStr = strings.TrimSuffix(s, "nbu")
	case strings.HasSuffix(s, "µbu"), strings.HasSuffix(s, "ubu"):
		multiplier = Micro
		numStr = strings.TrimSuffix(s, "µbu")
		if numStr == s {
			numStr = strings.TrimSuffix(s, "ubu")
		}
	case strings.HasSuffix(s, "mbu"):
		multiplier = Milli
		numStr = strings.TrimSuffix(s, "mbu")
	case strings.HasSuffix(s, "bu"):
		multiplier = Unit
		numStr = strings.TrimSuffix(s, "bu")
	default:
		numStr = s
	}

	numStr = strings.TrimSpace(numStr)
	value, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid BU value: %s", s)
	}

	return int64(math.Round(value * float64(multiplier))), nil
}

// Add 加法，返回纳 BU 值
func Add(a, b int64) int64 {
	return a + b
}

// Sub 减法，返回纳 BU 值
func Sub(a, b int64) int64 {
	return a - b
}

// Mul 乘法，纳 BU * 倍数
func Mul(value int64, multiplier float64) int64 {
	return int64(math.Round(float64(value) * multiplier))
}

// Div 除法，纳 BU / 倍数
func Div(value int64, divisor float64) int64 {
	return int64(math.Round(float64(value) / divisor))
}

// CalculateCost 根据 token 数量和单价计算费用
// pricePerK 是每千 token 的价格（纳 BU）
// tokens 是 token 数量
// 返回总费用（纳 BU）
func CalculateCost(pricePerK int64, tokens int) int64 {
	if pricePerK == 0 || tokens == 0 {
		return 0
	}
	// pricePerK / 1000 * tokens
	return int64(math.Round(float64(pricePerK) * float64(tokens) / 1000))
}
