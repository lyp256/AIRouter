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
- `pkg/anthropic/types.go` - Anthropic 协议类型（支持 `thinking` 内容块和 `thinking_delta` 增量）

### 前端模块
- `web/src/api/types.ts` - 全局类型定义（User、Model、FilterOptions 等）
- `web/src/api/chat.ts` - 聊天 API + 会话管理（sessionStorage 存储）
- `web/src/api/model.ts` - 模型管理（`list()` 调用 `/v1/models`，`adminList()` 调用 `/api/admin/models`）
- `web/src/api/stats.ts` - 统计 API（DashboardStats、UsageTrend、ModelStats、UserStats）
- `web/src/stores/chat.ts` - 聊天 Pinia Store（后台会话状态、流式内容管理）
- `web/src/utils/format.ts` - BU 单位格式化工具（`formatBU`、`formatPricePerM`、`storageToDisplay`、`displayToStorage`）

### 数据模型

**Provider（供应商）**: name, type, base_url, api_path, enabled

**ProviderKey（供应商密钥）**: provider_id, name, key（加密）, status, quota_limit

**Model（对外模型）**: name, provider_type, description, input_price, output_price, context_window, enabled
- `provider_type` 为必填字段，可选值：openai, anthropic, openai_compatible
- `(name, provider_type)` 为组合唯一索引，支持同名不同类型的模型
- 价格字段使用 int64 存储（纳 BU/K tokens），前端显示为 BU/M tokens

**Upstream（上游模型）**: model_id, provider_id, provider_key_id, provider_model, weight, priority, enabled
- 上游模型的供应商类型必须与所属模型的 `provider_type` 匹配

**UserKey（用户密钥）**: user_id, key（加密）, rate_limit, quota_limit, status

**UsageLog（使用日志）**: user_id, user_key_id, upstream_id, provider_key_id, model, input_tokens, output_tokens, cost, latency, status
- 使用 ID 关联外部数据，通过 JOIN 查询获取完整信息
- `upstream_id` 关联上游模型，可获取 provider_model、model_id、provider_id
- 查询时通过 JOIN 获取 username、provider_type、provider_name 等关联字段

**关系**: Provider 1:N ProviderKey, Upstream 1:1 ProviderKey, Upstream N:1 Provider, Model 1:N Upstream

### BU 计量单位

系统使用抽象计量单位 BU（Basic Unit），统一表示价格、配额和费用：

- **最小单位**: 纳 BU（Nano，nBU）
- **换算关系**: 1000 纳 = 1 微，1000 微 = 1 毫，1000 毫 = 1 BU
- **1 BU** = 10^9 纳 BU
- **后端存储**: int64 纳 BU/K tokens（每千 token 价格）
- **前端显示**: BU/M tokens（每百万 token 价格）
- **换算**: 存储 × 10^6 = 显示值（纳 BU/K → BU/M）

工具包: `pkg/bu/bu.go` 提供单位转换函数。

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
- Anthropic 请求使用原生 API 处理
- 使用中文注释和中文界面
- 供应商 `base_url` 不包含路径，路径由上游模型的 `api_path` 或默认值决定
- 代码或设计变更需同步更新相关文档（CLAUDE.md、README.md、system-design.md）
- ChatMessage 支持 `reasoning_content` 字段（推理模型思考过程）
- Anthropic 协议支持 `thinking` 内容块和 `thinking_delta` 增量（Claude Extended Thinking）
- 对外 API 支持混合认证：API Key 或 JWT+KeyID（用于管理后台聊天）
- 提交代码前需要使用 `make check ` 检查，检查通过后才可以提交，编写的 golang 代码必须需要符合 golangci-lint 的规范