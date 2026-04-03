# CLAUDE.md - AIRouter Development Guide

## 常用命令
```bash
make build          # 编译后端（含前端嵌入）
make test           # 运行全量测试
make lint-fix       # 运行代码检查并自动修复
make fmt            # 格式化代码
make check          # 完整检查（fmt + vet + lint），提交前必跑
make dev            # 开发模式：初始化 DB 并运行后端
make web-install    # 安装前端依赖
make web-build      # 构建前端生产版本
make web-dev        # 启动前端开发服务器
```

## 核心架构与模块
- **后端 (Go)**:
  - `internal/api/handler/`: 请求处理器 (auth, proxy, stats, etc.)
  - `internal/api/middleware/`: 认证 (JWT, API Key), 限流, 权限
  - `internal/service/`: 核心逻辑 (upstream_selector, upstream_health, quota, retry)
  - `internal/provider/`: 供应商客户端适配器 (OpenAI, Anthropic)
  - `pkg/bu/`: BU (Basic Unit) 计量单位转换工具
- **前端 (Vue 3 + TS + Tailwind)**:
  - `web/src/api/`: API 定义 (chat, model, stats, etc.)
  - `web/src/stores/`: Pinia 状态管理 (chat session, user auth)
  - `web/src/utils/format.ts`: BU 单位格式化与转换逻辑

## 数据模型精要
- **Model (对外模型)**: `(name, provider_type)` 组合唯一。`provider_type`：`openai`, `anthropic`, `openai_compatible`。
- **Upstream (上游模型)**: 负载均衡基本单位。关联 `ModelID`, `ProviderID`, `ProviderKeyID`。
- **BU 单位**: 
  - **后端存储**: `int64` 纳 BU/K tokens (每千 token 价格)
  - **前端显示**: BU/M tokens (每百万 token 价格)
  - **换算**: 存储值 (nBU/K) * 10^6 = 显示值 (BU/M)；1 BU = 10^9 nBU。
- **健康状态**: 存储于缓存 (`upstream:health:{id}`)，TTL 1h。缓存未命中视为健康。

## 开发规范与约束
- **API 认证**: 支持 API Key 或 `JWT + X-Key-ID` 混合认证。
- **流式传输**: 统一使用 SSE 协议。
- **模型支持**: 
  - OpenAI 协议支持 `reasoning_content` (DeepSeek R1 等)。
  - Anthropic 协议支持 `thinking` 内容块及 `thinking_delta` 增量事件。
- **健康检查**: 两级机制（5min 全量，30s 恢复）。分布式环境通过 Redis `leader:health-check:*` 选主。
- **提交规范**: 任何代码变更后必须通过 `make check`。符合 `golangci-lint` 规范。使用中文注释和界面。
- **文档同步**: 变更核心逻辑或设计需同步更新 `CLAUDE.md`, `README.md` 及 `docs/system-design.md`。
