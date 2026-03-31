// Package web 提供前端静态文件的嵌入支持
package web

import "embed"

//go:embed all:dist
var DistFS embed.FS
