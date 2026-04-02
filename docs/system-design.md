# AIRouter 系统设计文档

## 1. 项目概述

AIRouter 是一个大模型 API 统一代理系统，提供以下核心能力：

- **多协议支持**：OpenAI、Anthropic 协议统一代理
- **多供应商管理**：OpenAI、Anthropic、Azure 及国内兼容厂商
- **上游模型管理**：对外模型可映射到多个供应商的上游模型，支持跨供应商负载均衡
- **密钥管理**：多 API Key 管理、自动故障转移
- **用户系统**：独立用户管理、API Key 认证、配额控制
- **使用统计**：请求日志、Token 统计、成本计算
- **Web 管理**：Vue 3 + Tailwind CSS 管理界面

---

## 2. 系统架构

### 2.1 整体架构图

```
┌──────────────────────────────────────────────────────────────────────┐
│                           Web Management                              │
│                        (Vue.js Frontend SPA)                          │
└─────────────────────────────────┬────────────────────────────────────┘
                                  │
                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│                          Admin API Gateway                            │
│                    (Bearer Token: admin_token)                        │
└─────────────────────────────────┬────────────────────────────────────┘
                                  │
┌─────────────────────────────────┼────────────────────────────────────┐
│                                 │                                     │
│  ┌──────────────────────────────┼────────────────────────────────┐   │
│  │                      User API Gateway                         │   │
│  │                  (Bearer Token: user_key)                     │   │
│  └──────────────────────────────┬────────────────────────────────┘   │
│                                 │                                     │
│                                 ▼                                     │
│  ┌───────────────────────────────────────────────────────────────┐   │
│  │                      API Gateway Layer                         │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │   │
│  │  │ Auth Middleware │ │ Rate Limiter │ │   Logger    │           │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │   │
│  └──────────────────────────────┬────────────────────────────────┘   │
│                                 │                                     │
│                                 ▼                                     │
│  ┌───────────────────────────────────────────────────────────────┐   │
│  │                      Core Services Layer                       │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │   │
│  │  │ Model Router │ │ Upstream Sel │ │ Provider Mgr │           │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │   │
│  │  │Load Balancer │ │  Failover    │ │Usage Tracker │           │   │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │   │
│  └──────────────────────────────┬────────────────────────────────┘   │
│                                 │                                     │
│                                 ▼                                     │
│  ┌───────────────────────────────────────────────────────────────┐   │
│  │                    Provider Adapters Layer                     │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐          │   │
│  │  │ OpenAI  │  │Anthropic│  │  Azure  │  │ 国内厂商 │          │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘          │   │
│  └──────────────────────────────┬────────────────────────────────┘   │
│                                 │                                     │
└─────────────────────────────────┼────────────────────────────────────┘
                                  │
                                  ▼
                      External LLM Provider APIs
```

### 2.2 单端口设计

服务统一在 **端口 8080** 提供，根据路径前缀区分认证方式：

| 路径前缀 | 认证方式 | 说明 |
|---------|---------|------|
| `/v1/*` | API Key | 对外 API |
| `/api/admin/*` | JWT | 管理 API |
| `/health` | 无需认证 | 健康检查 |

### 2.3 权限控制

系统支持基于角色的权限控制（RBAC），用户分为 `admin`（管理员）和 `user`（普通用户）两种角色。

#### API 权限矩阵

| API 路径 | 权限要求 | 说明 |
|---------|---------|------|
| `/api/admin/auth/login` | 无需认证 | 登录 |
| `/api/admin/auth/me` | 已认证 | 获取当前用户信息 |
| `/api/admin/providers/*` | 仅管理员 | 供应商管理 |
| `/api/admin/models/*` | 仅管理员 | 模型管理 |
| `/api/admin/upstreams/*` | 仅管理员 | 上游模型管理 |
| `/api/admin/users/*` | 仅管理员 | 用户管理 |
| `/api/admin/user-keys` | 混合权限 | 用户密钥管理 |
| `/api/admin/stats/*` | 仅管理员 | 统计分析 |

#### 用户密钥管理权限说明

| 操作 | 管理员 | 普通用户 |
|------|--------|---------|
| 查询密钥列表 | 可查询任意用户（需指定 user_id） | 只能查询自己的密钥 |
| 创建密钥 | 可为任意用户创建 | 只能为自己创建 |
| 更新/删除/重新生成密钥 | 可操作任意密钥 | 只能操作自己的密钥 |

#### 权限中间件

- `RequireAdmin()` - 要求管理员权限的中间件，用于保护敏感 API
- `IsAdmin(c)` - 辅助函数，检查当前用户是否为管理员

#### 前端权限控制

前端同样实现了基于角色的权限控制：

**路由守卫**：
- 路由配置中 `requiresAdmin: true` 标记需要管理员权限的页面
- 路由守卫检查用户角色，非管理员访问受限页面时重定向到仪表盘

**菜单过滤**：
- 侧边栏菜单根据用户角色动态过滤
- 管理员菜单（供应商管理、模型管理、用户管理、统计分析）仅对管理员可见

**用户状态管理**：
- `isAdmin` - 计算属性，判断当前用户是否为管理员
- `hasPermission(role)` - 方法，检查用户是否有指定角色权限

### 2.4 请求处理流程

```
用户请求 → API Key 认证中间件 → 限流中间件
    → 模型路由器查找模型配置 → 获取上游模型列表
    → 上游模型选择器按权重/优先级选择上游模型
    → 供应商客户端转发请求 → 记录使用日志
```

---

## 3. 数据模型设计

### 3.1 模型关系图

```
Provider (供应商) 1 ←───→ N ProviderKey (供应商密钥)
        ↑                          ↑
        │                          │ 1
        │ 1                        │
        └──────── Upstream (上游模型) ───────→ N Model (对外模型)
```

关系说明：
- **Provider 1:N ProviderKey**：一个供应商可有多个 API Key
- **Upstream 1:1 ProviderKey**：一个上游模型关联一个供应商密钥
- **Upstream N:1 Provider**：一个上游模型关联一个供应商（冗余关联，便于查询）
- **Model 1:N Upstream**：一个对外模型包含多个上游模型，Upstream 之间负载均衡

#### 数据库表清单

系统共使用 7 个数据库表：

| 表名 | 说明 |
|------|------|
| `users` | 用户信息 |
| `user_keys` | 用户 API Key |
| `providers` | 供应商信息 |
| `provider_keys` | 供应商密钥 |
| `models` | 对外模型 |
| `upstreams` | 上游模型 |
| `usage_logs` | 使用日志 |

### 3.2 User（用户）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 用户 ID |
| `Username` | string | 用户名 |
| `Email` | string | 邮箱 |
| `Password` | string | 密码（bcrypt 加密存储） |
| `Role` | string | 角色：admin, user |
| `Status` | string | 状态：active, disabled |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

### 3.3 UserKey（用户密钥）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 密钥 ID |
| `Name` | string | 密钥名称 |
| `Key` | string | 用户 API Key（AES-GCM 加密存储） |
| `UserID` | string | 所属用户 |
| `Permissions` | []string | 权限列表：models:*, models:gpt-4 |
| `RateLimit` | int | 速率限制（请求/分钟） |
| `QuotaLimit` | int64 | 配额限制 |
| `QuotaUsed` | int64 | 已使用配额 |
| `ExpiredAt` | time.Time | 过期时间 |
| `Status` | string | 状态：active, disabled, expired |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

### 3.4 Provider（供应商）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 供应商 ID |
| `Name` | string | 供应商名称 |
| `Type` | string | 类型：openai, anthropic, openai_compatible |
| `BaseURL` | string | API 基础地址（不包含路径） |
| `APIPath` | string | API 路径，留空使用默认路径 |
| `Description` | string | 描述 |
| `Enabled` | bool | 是否启用 |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

### 3.5 ProviderKey（供应商密钥）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 密钥 ID |
| `ProviderID` | string | 所属供应商 |
| `Name` | string | 密钥名称/标识 |
| `Key` | string | API Key（AES-GCM 加密存储） |
| `Status` | string | 状态：active, disabled, error |
| `QuotaLimit` | int64 | 配额限制 |
| `QuotaUsed` | int64 | 已使用配额 |
| `LastUsedAt` | time.Time | 最后使用时间 |
| `LastErrorAt` | time.Time | 最后错误时间 |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

### 3.6 Model（对外大模型）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 模型 ID |
| `Name` | string | 模型名称（对外展示） |
| `ProviderType` | string | 供应商类型：openai, anthropic, openai_compatible（必填，创建后不可修改） |
| `Description` | string | 模型描述 |
| `InputPrice` | int64 | 输入价格（纳 BU/1K tokens） |
| `OutputPrice` | int64 | 输出价格（纳 BU/1K tokens） |
| `ContextWindow` | int | 上下文窗口大小 |
| `Enabled` | bool | 是否启用 |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

**唯一约束**：`(Name, ProviderType)` 组合唯一索引，支持同名不同类型的模型。

**类型约束**：
- 模型创建时必须指定 `ProviderType`，创建后不可修改
- 添加上游模型时，供应商类型必须与模型的 `ProviderType` 匹配

**BU 计量单位**：
- 最小单位：纳 BU（Nano，nBU）
- 换算关系：1000 纳 = 1 微，1000 微 = 1 毫，1000 毫 = 1 BU
- 1 BU = 10^9 纳 BU
- 所有价格、费用、配额字段统一使用 int64 存储纳 BU 值
- 前端显示单位：BU/M tokens（每百万 token 的价格）
- 后端存储单位：纳 BU/K tokens（每千 token 的价格，乘以 10^6 得到存储值）

### 3.7 Upstream（上游模型）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 上游模型 ID |
| `ModelID` | string | 关联对外模型 |
| `ProviderID` | string | 关联供应商 |
| `ProviderKeyID` | string | 关联供应商密钥 |
| `ProviderModel` | string | 供应商实际模型名 |
| `Weight` | int | 权重（负载均衡用） |
| `Priority` | int | 优先级 |
| `Status` | string | 状态：active, disabled, error（运行时状态存储在缓存中，数据库仅保留初始值） |
| `Enabled` | bool | 是否启用 |
| `CreatedAt` | time.Time | 创建时间 |
| `UpdatedAt` | time.Time | 更新时间 |

### 3.8 UsageLog（使用日志）

使用 ID 关联外部数据，通过 JOIN 查询获取完整信息，避免数据冗余。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | string | 日志 ID |
| `UserID` | string | 用户 ID |
| `UserKeyID` | string | 用户密钥 ID |
| `UpstreamID` | string | 上游模型 ID（可获取 provider_model、model_id、provider_id） |
| `ProviderKeyID` | string | 供应商密钥 ID |
| `Model` | string | 对外模型名称（保留用于索引优化） |
| `InputTokens` | int | 输入 token 数 |
| `OutputTokens` | int | 输出 token 数 |
| `Cost` | int64 | 费用（纳 BU） |
| `Latency` | int | 延迟(ms) |
| `FirstTokenLatency` | int | 首 Token 延迟(ms)，仅流式请求有效 |
| `Status` | string | 状态：success, error |
| `ErrorMessage` | string | 错误信息 |
| `RequestID` | string | 请求 ID |
| `CreatedAt` | time.Time | 创建时间 |

**关联查询字段**（通过 JOIN 获取）：
- `Username` - 用户名（来自 users 表）
- `ProviderType` - 协议类型（来自 models 表）
- `ProviderModel` - 供应商模型（来自 upstreams 表）
- `ProviderName` - 供应商名称（来自 providers 表）

---

## 4. API 设计

### 4.1 对外 API（支持两种认证方式）

对外 API 支持**混合认证**，用户可以选择以下任一方式：

| 认证方式 | 请求头 | 说明 |
|---------|--------|------|
| API Key 认证 | `Authorization: Bearer <api_key>` | 传统方式，直接使用用户密钥 |
| JWT + KeyID 认证 | `Authorization: Bearer <jwt_token>` + `X-Key-ID: <key_id>` | 管理后台使用，JWT 验证身份后关联用户密钥 |

**JWT + KeyID 认证流程**：
1. 验证 JWT Token，获取用户 ID
2. 根据 X-Key-ID 查询用户密钥
3. 验证密钥归属于当前用户
4. 验证通过后使用该密钥处理请求

#### OpenAI 协议

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/chat/completions` | Chat Completions |
| POST | `/v1/completions` | Completions（旧版） |
| POST | `/v1/embeddings` | Embeddings |
| GET | `/v1/models` | 模型列表 |
| GET | `/v1/models/{model}` | 模型详情 |

#### Anthropic 协议

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/messages` | Messages API |

### 4.2 管理 API（JWT 认证）

#### 认证

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/admin/auth/login` | 登录（无需认证） |
| POST | `/api/admin/auth/logout` | 登出 |
| GET | `/api/admin/auth/me` | 当前用户信息 |

#### 用户管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/users` | 用户列表 |
| POST | `/api/admin/users` | 创建用户 |
| PUT | `/api/admin/users/{id}` | 更新用户 |
| DELETE | `/api/admin/users/{id}` | 删除用户 |

#### 用户密钥管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/user-keys?user_id=xxx` | 查询用户密钥 |
| GET | `/api/admin/user-keys/me` | 获取当前用户密钥列表（用于聊天功能） |
| POST | `/api/admin/user-keys` | 创建密钥 |
| PUT | `/api/admin/user-keys/{id}` | 更新密钥（限流、配额、过期时间、状态） |
| DELETE | `/api/admin/user-keys/{id}` | 删除密钥 |
| POST | `/api/admin/user-keys/{id}/regenerate` | 刷新密钥 |

#### 供应商管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/providers` | 供应商列表 |
| POST | `/api/admin/providers` | 创建供应商 |
| GET | `/api/admin/providers/{id}` | 供应商详情 |
| PUT | `/api/admin/providers/{id}` | 更新供应商 |
| DELETE | `/api/admin/providers/{id}` | 删除供应商 |

#### 供应商密钥管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/providers/{id}/keys` | 供应商密钥列表 |
| POST | `/api/admin/providers/{id}/keys` | 创建密钥 |
| PUT | `/api/admin/provider-keys/{id}` | 更新密钥 |
| DELETE | `/api/admin/provider-keys/{id}` | 删除密钥 |

#### 模型管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/models` | 模型列表 |
| POST | `/api/admin/models` | 创建模型 |
| GET | `/api/admin/models/{id}` | 模型详情（含上游模型） |
| PUT | `/api/admin/models/{id}` | 更新模型 |
| DELETE | `/api/admin/models/{id}` | 删除模型 |
| POST | `/api/admin/models/{id}/toggle` | 切换模型启用状态 |

#### 上游模型管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/upstreams` | 上游模型列表 |
| GET | `/api/admin/upstreams/{id}` | 上游模型详情 |
| GET | `/api/admin/models/{id}/upstreams` | 模型的上游模型列表 |
| POST | `/api/admin/models/{id}/upstreams` | 为模型添加上游模型（供应商类型必须匹配） |
| PUT | `/api/admin/upstreams/{id}` | 更新上游模型 |
| DELETE | `/api/admin/upstreams/{id}` | 删除上游模型 |
| POST | `/api/admin/upstreams/{id}/toggle` | 切换上游模型启用状态 |

**类型匹配约束**：创建上游模型时，供应商的 `type` 必须与模型的 `provider_type` 一致，否则返回 400 错误。

#### 使用统计

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/stats/dashboard` | 仪表盘统计（今日请求数、Token 数、消费、成功率） |
| GET | `/api/admin/stats/trend?days=N` | 使用趋势（默认 7 天，最大 30 天） |
| GET | `/api/admin/stats/models?days=N` | 按模型统计 |
| GET | `/api/admin/stats/users?days=N` | 按用户统计 |
| GET | `/api/admin/stats/filter-options` | 筛选选项（模型、协议类型、厂商、供应商密钥、状态） |
| GET | `/api/admin/stats/logs?model=&provider_type=&provider_name=&provider_key_id=&status=` | 使用日志列表 |

### 4.3 其他

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查（无需认证） |

---

## 5. 技术选型

### 5.1 后端技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| Web 框架 | Gin | 高性能、轻量 |
| 数据库 | SQLite (默认) / PostgreSQL | SQLite 便于开发，PG 支持生产 |
| 缓存 | 内存缓存 (可选 Redis) | 上游健康状态、模型配置缓存、分布式选主 |
| 配置 | Viper | 支持多种配置格式 |
| 日志 | Zap | 结构化日志 |
| HTTP 客户端 | Resty | 供应商 API 调用 |
| 加密 | AES-GCM | API Key 加密存储 |
| JWT | jwt-go | 用户认证 |

### 5.2 前端技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| 框架 | Vue 3 | Composition API |
| 构建工具 | Vite | 快速开发体验 |
| 语言 | TypeScript | 类型安全 |
| 状态管理 | Pinia | Vue 3 官方推荐 |
| 路由 | Vue Router | SPA 路由 |
| UI 组件 | Tailwind CSS + Headless UI | 现代化 UI |
| 图表 | ECharts | 统计图表 |
| HTTP | Axios | API 请求 |

---

## 6. 项目目录结构

### 6.1 后端目录结构

```
airouter/
├── cmd/airouter/main.go           # 入口文件
├── internal/
│   ├── config/config.go           # 配置管理
│   ├── model/model.go             # 数据模型
│   ├── store/sqlite/sqlite.go     # SQLite 存储
│   ├── service/                   # 核心服务
│   │   ├── upstream_selector.go   # 上游模型选择器
│   │   ├── upstream_health.go     # 上游健康检查（两级检查 + 分布式选主）
│   │   ├── retry.go               # 重试服务
│   │   ├── quota.go               # 配额管理
│   │   └── metrics.go             # Prometheus 指标
│   ├── provider/                  # 供应商适配器
│   │   ├── client.go              # OpenAI 兼容客户端
│   │   └── anthropic.go           # Anthropic 客户端
│   ├── api/
│   │   ├── handler/               # 请求处理器
│   │   │   ├── auth.go            # 认证
│   │   │   ├── proxy.go           # 代理
│   │   │   ├── provider.go        # 供应商
│   │   │   ├── model.go           # 模型
│   │   │   ├── user.go            # 用户
│   │   │   └── stats.go           # 统计
│   │   └── middleware/            # 中间件
│   │       ├── jwt.go             # JWT 认证
│   │       ├── apikey.go          # API Key 认证
│   │       ├── auth_selector.go   # 认证选择器
│   │       ├── permission.go      # 权限中间件
│   │       ├── ratelimit.go       # 限流
│   │       └── common.go          # 通用中间件
│   └── crypto/crypto.go           # AES-GCM 加密
├── pkg/
│   ├── openai/types.go            # OpenAI 协议类型
│   └── anthropic/types.go         # Anthropic 协议类型
├── configs/config.yaml            # 配置文件
├── web/                           # 前端项目
├── Makefile                       # 构建脚本
├── Dockerfile                     # Docker 配置
└── README.md
```

### 6.2 前端目录结构

```
web/
├── public/
│   └── favicon.svg
├── src/
│   ├── api/                       # API 请求
│   │   ├── index.ts               # axios 封装
│   │   ├── auth.ts                # 认证
│   │   ├── chat.ts                # 聊天 API（流式响应 + 会话管理）
│   │   ├── user.ts                # 用户管理
│   │   ├── provider.ts            # 供应商管理
│   │   ├── model.ts               # 模型管理（含上游模型）
│   │   ├── stats.ts               # 使用统计
│   │   └── types.ts               # 全局类型定义（含 FilterOptions）
│   ├── utils/
│   │   └── format.ts              # BU 格式化工具函数
│   ├── components/
│   │   └── Sidebar.vue            # 侧边栏
│   ├── views/                     # 页面
│   │   ├── Login.vue              # 登录页
│   │   ├── Dashboard.vue          # 仪表盘
│   │   ├── Chat.vue               # AI 聊天（支持思考过程显示）
│   │   ├── Providers.vue          # 供应商管理
│   │   ├── Models.vue             # 模型管理（含上游模型管理）
│   │   ├── Users.vue              # 用户管理
│   │   ├── Keys.vue               # 密钥管理
│   │   ├── Statistics.vue         # 使用统计
│   │   └── Settings.vue           # 设置
│   ├── router/index.ts            # 路由配置
│   ├── stores/
│   │   ├── user.ts                # 用户状态管理
│   │   └── chat.ts                # 聊天状态管理（后台会话、流式响应）
│   ├── App.vue
│   └── main.ts
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
└── tailwind.config.js
```

---

## 7. 核心功能模块

### 7.1 上游模型选择器 (upstream_selector.go)

- 支持 **权重（weight）** 和 **优先级（priority）** 两种策略
- 按优先级分组，同优先级内按权重负载均衡
- 支持 **故障转移**：上游模型失败自动切换
- 返回上游模型 + 供应商 + 供应商密钥 + 解密后的 API Key

### 7.2 重试服务 (retry.go)

- 可配置的重试策略
- 支持 **指数退避** 和 **抖动**
- 可配置重试状态码

### 7.3 配额管理 (quota.go)

- 用户配额限制
- 供应商密钥配额限制
- 配额检查和告警

### 7.4 上游健康检查 (upstream_health.go)

- 健康状态存储在缓存中（key: `upstream:health:{upstreamID}`），不写入数据库
- 缓存未命中视为健康（默认 active）
- 两级检查机制：
  - **全量检查**（默认 5 分钟间隔）：检查所有启用的上游模型
  - **恢复检查**（默认 30 秒间隔）：仅检查不健康的上游模型以快速恢复
- 按 `(providerID, providerKeyID)` 去重，减少重复探测
- 探测方式：向供应商 `/v1/models` 端点发送 GET 请求，HTTP 429 不算不健康
- 连续成功/失败次数达到阈值后转换状态
- 分布式选主：通过缓存 `leader:health-check:full` 和 `leader:health-check:recovery` key 实现
  - Redis 缓存模式下只有一个实例执行健康检查
  - 内存缓存模式下各实例独立运行
- 代理请求失败时即时标记上游为不健康（写入缓存）
- 代理请求成功时即时恢复健康状态

### 7.5 Prometheus 指标 (metrics.go)

- 请求计数
- 延迟统计
- 错误率统计

### 7.6 BU 计量单位 (pkg/bu/bu.go)

系统使用抽象计量单位 BU（Basic Unit），统一表示价格、配额和费用：

**单位定义**：
- 最小单位：纳 BU（Nano，nBU）
- 换算关系：1000 纳 = 1 微，1000 微 = 1 毫，1000 毫 = 1 BU
- 1 BU = 10^9 纳 BU

**存储与显示**：
- 后端存储：int64 纳 BU/K tokens（每千 token 价格）
- 前端显示：BU/M tokens（每百万 token 价格）
- 换算：存储值 × 10^-6 = 显示值

**后端工具函数**：
- `FromFloat(value float64) int64` - BU 转纳 BU
- `ToFloat(value int64) float64` - 纳 BU 转 BU
- `CalculateCost(pricePerK int64, tokens int) int64` - 计算费用
- `Format(value int64) string` - 格式化显示

**前端工具函数** (`web/src/utils/format.ts`)：
- `formatBU(nano)` - 格式化 BU 显示（通用，用于成本、消费等）
- `formatPricePerM(storageValue)` - 格式化价格显示（BU/M tokens）
- `storageToDisplay(storageValue)` - 存储（纳 BU/K）转显示（BU/M）
- `displayToStorage(displayValue)` - 显示（BU/M）转存储（纳 BU/K）

### 7.7 加密存储 (crypto.go)

- AES-GCM 加密算法
- API Key 加密存储

### 7.8 混合认证机制 (apikey.go)

对外 API 支持两种认证方式：
- **API Key 认证**：传统方式，直接使用 `Authorization: Bearer <api_key>`
- **JWT + KeyID 认证**：管理后台使用，通过 `Authorization: Bearer <jwt_token>` + `X-Key-ID: <key_id>` 认证

JWT + KeyID 认证流程：
1. 验证 JWT Token，获取用户 ID
2. 根据 X-Key-ID 查询用户密钥
3. 验证密钥归属于当前用户（防止使用他人密钥）
4. 检查密钥状态、过期时间、配额
5. 验证通过后使用该密钥处理请求

前端聊天功能使用此机制，用户可选择自己的密钥调用模型。

### 7.9 前端协议自动选择

前端聊天页面根据模型的 `provider_type` 自动选择 API 协议：

| 模型类型 | API 端点 | 协议格式 |
|---------|---------|---------|
| `anthropic` | `/v1/messages` | Anthropic Messages API |
| `openai` | `/v1/chat/completions` | OpenAI Chat Completions API |
| `openai_compatible` | `/v1/chat/completions` | OpenAI Chat Completions API |

**实现方式**：
- 管理后台通过 `modelApi.adminList()` 获取完整模型信息（含 `provider_type`）
- `/v1/models` 端点返回 OpenAI 兼容格式的模型列表，`modelApi.list()` 直接调用此端点
- 前端根据当前选中模型的类型，动态选择调用对应的 API 端点
- 支持 Anthropic 原生协议的流式响应处理

### 7.10 会话管理

聊天页面支持多会话管理，会话数据存储在 `sessionStorage` 中：

**会话存储**：
- `ChatSession` 类型定义在 `api/chat.ts`，包含 id、name、model、modelId、keyId、messages 等
- `saveChatSession()` / `getChatSessions()` / `deleteChatSession()` 管理会话 CRUD
- `stores/chat.ts` 中的 `completeAndSaveBackgroundSession()` 复用 `saveChatSession()` 保存后台完成的会话

**后台会话**：
- `stores/chat.ts` (Pinia Store) 管理正在进行的聊天会话状态
- 支持切换页面后继续在后台处理流式响应
- 返回聊天页面时自动恢复后台会话状态（消息、流式内容、参数）

### 7.10 思考过程显示

管理后台聊天功能支持显示推理模型的思考过程：

**OpenAI 协议**：
- 支持 `reasoning_content` 字段（DeepSeek R1 等模型）
- 流式响应中 `delta.reasoning_content` 包含思考内容

**Anthropic 协议**：
- 支持 `thinking` 类型内容块（Claude Extended Thinking）
- 流式响应中 `thinking_delta` 类型的增量事件包含思考内容
- 类型定义：
  - `ContentBlock.type = "thinking"` - 思考内容块
  - `StreamDelta.type = "thinking_delta"` - 思考增量事件
  - `StreamDelta.thinking` - 思考内容文本

**前端实现**：
- 流式响应时实时显示思考过程，带"正在思考..."动画
- 完成后思考内容可折叠展示
- 支持 Markdown 渲染

---

## 8. 配置说明

配置文件路径: `configs/config.yaml`

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  type: "sqlite"
  path: "./data/airouter.db"

security:
  encryption_key: "your-32-byte-encryption-key!!"  # 32字节 AES 加密密钥
  jwt_secret: "your-jwt-secret-key"                # JWT 签名密钥
  jwt_expire: "24h"

rate_limit:
  enabled: true
  default_rpm: 60

retry:
  enabled: true
  max_attempts: 3
  initial_wait: "1s"
  max_wait: "30s"
  multiplier: 2.0
  retry_on_codes: [429, 500, 502, 503, 504]

admin:
  username: "admin"
  password: "changeme"
  email: "admin@example.com"

health_check:
  enabled: true
  full_check_interval: "5m"     # 全量检查间隔
  recovery_interval: "30s"      # 不健康上游恢复检查间隔
  timeout: "10s"                # 探测超时
  healthy_threshold: 2          # 连续成功次数阈值
  unhealthy_threshold: 3        # 连续失败次数阈值
  leader_lease: "30s"           # 选主租约时长
  leader_renew_interval: "10s"  # 选主续约间隔
```

---

## 9. 开发进度

| 阶段 | 状态 | 说明 |
|------|------|------|
| 阶段一：基础框架 | ✅ 已完成 | 项目初始化、配置、数据模型、存储 |
| 阶段二：用户管理与认证 | ✅ 已完成 | 用户 CRUD、JWT、中间件、限流 |
| 阶段三：供应商管理 | ✅ 已完成 | 适配器、密钥管理 |
| 阶段四：路由与负载均衡 | ✅ 已完成 | 模型路由、上游模型选择、故障转移、重试 |
| 阶段五：用户 API 接口 | ✅ 已完成 | OpenAI + Anthropic 协议、流式响应 |
| 阶段六：管理 API 与前端 | ✅ 已完成 | 完整管理 API、前端页面 |
| 阶段七：高级功能 | ✅ 已完成 | 统计、配额、健康检查、Prometheus |
| 数据模型重构 | ✅ 已完成 | 引入 Upstream 概念，支持跨供应商负载均衡 |
| 模型类型属性 | ✅ 已完成 | 模型添加 provider_type 属性，支持同名不同类型模型，上游模型供应商类型约束 |
| 阶段八：部署与文档 | ⏸️ 暂缓 | Dockerfile 已完成，其他暂缓 |

---

## 10. 开发注意事项

1. **安全性**
   - API Key 使用 AES-GCM 加密存储
   - 用户密码使用 bcrypt 加密
   - 管理 API 使用 JWT 认证

2. **可扩展性**
   - 供应商适配器采用插件式设计
   - 存储层抽象接口，支持切换数据库
   - 支持多协议（OpenAI、Anthropic）

3. **可靠性**
   - 上游模型故障自动切换
   - 请求超时处理
   - 优雅关闭

4. **兼容性**
   - 用户 API 完全兼容 OpenAI / Anthropic 格式
   - 便于现有应用无缝迁移

5. **开发规范**
   - 使用中文注释和中文界面
   - 供应商 `api_path` 留空使用默认路径
   - 供应商 `base_url` 不包含路径部分