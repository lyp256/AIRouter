package main

import "github.com/lyp256/airouter/cmd/airouter/cmd"

// 版本信息，由构建时注入
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func init() {
	// 将版本信息传递给 cmd 包
	cmd.SetVersion(Version, BuildTime)
}

func main() {
	cmd.Execute()
}
