# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

AIRouter 是一个大模型 API 统一代理系统，提供多供应商统一代理、密钥管理、负载均衡和使用统计。

## 常用命令

```bash
make build        # 编译后端
make test         # 运行测试
make lint         # 代码检查（自动安装 golangci-lint）
make lint-fix     # 代码检查并自动修复
make fmt          # 格式化代码
make check        # 完整代码检查（格式化 + vet + lint）
make dev          # 初始化数据库并运行
make web-install  # 安装前端依赖
make web-build    # 构建前端生产版本
```



## 架构概览

### 端口与路由
- **端口 8080**: 统一服务端口
  - `/v1/*` - 对外 API（支持 API Key 或 JWT+KeyID 认证）
  - `/api/admin/*` - 管理 API（JWT 认证）
  - `/health` - 健康检查（无需认证）

### API 协议
- `/v1/chat/completions` - OpenAI Chat Completions API
- `/v1/messages` - Anthropic Messages API
- `/v1/embeddings` - Embeddings API
- `/v1/models` - 模型列表

### 管理 API
- `/api/admin/auth/*` - 认证（登录、登出、获取当前用户）
- `/api/admin/providers` - 供应商管理（仅管理员）
- `/api/admin/models` - 模型管理（仅管理员）
- `/api/admin/upstreams` - 上游模型管理（仅管理员）
- `/api/admin/users` - 用户管理（仅管理员）
- `/api/admin/user-keys` - 用户密钥管理（混合权限）
- `/api/admin/stats/*` - 统计分析（仅管理员）

### 核心模块
- `internal/api/handler/` - 请求处理器
- `internal/api/middleware/` - 中间件（JWT、API Key、限流、日志、权限）
- `internal/service/` - 业务服务（upstream_selector、retry、quota、health、metrics）
- `internal/provider/` - 供应商客户端（OpenAI 兼容、Anthropic 原生）
- `pkg/openai/types.go` - OpenAI 协议类型（支持 `reasoning_content`）
- `pkg/anthropic/types.go` - Anthropic 协议类型

### 数据模型

**Provider（供应商）**: name, type, base_url, api_path, enabled

**ProviderKey（供应商密钥）**: provider_id, name, key（加密）, status, quota_limit

**Model（对外模型）**: name, description, input_price, output_price, context_window, enabled

**Upstream（上游模型）**: model_id, provider_id, provider_key_id, provider_model, weight, priority, enabled

**UserKey（用户密钥）**: user_id, key（加密）, rate_limit, quota_limit, status

**关系**: Provider 1:N ProviderKey, Upstream 1:1 ProviderKey, Upstream N:1 Provider, Model 1:N Upstream

## 配置

配置文件: `configs/config.yaml`

关键配置:
- `security.encryption_key` - 32字节 AES 加密密钥
- `security.jwt_secret` - JWT 签名密钥
- `retry` - 重试配置
- `admin` - 初始管理员账户

## 开发规范

- API Key 存储使用 AES-GCM 加密
- 上游模型选择支持权重和优先级
- 流式请求使用 SSE 协议
- Anthropic 请求支持原生 API 和 OpenAI 兼容模式转换
- 使用中文注释和中文界面
- 供应商 `base_url` 不包含路径，路径由上游模型的 `api_path` 或默认值决定
- 代码变更需同步更新相关文档（CLAUDE.md、README.md、system-design.md）
- ChatMessage 支持 `reasoning_content` 字段（推理模型思考过程）
- 对外 API 支持混合认证：API Key 或 JWT+KeyID（用于管理后台聊天）
- 开发调试和本地运行前后端优先使用 MCP 调试工具（debugmcp，端口 65001）
- 本地运行前后端优先使用 debugmcp MCP 服务以调试方式启动前端和后端，调试启动配置一般在 .vscode/launch.json 文件中保存