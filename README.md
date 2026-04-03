# AIRouter

统一的大模型 API 代理系统，支持多个供应商的统一代理、密钥管理、负载均衡和使用统计。

## 功能特性

- **多供应商支持**：OpenAI、Anthropic、Azure 等兼容接口
- **上游模型管理**：对外模型可映射到多个供应商的上游模型，支持跨供应商负载均衡
- **密钥管理**：多 API Key 管理，自动故障转移
- **用户管理**：独立用户系统，API Key 认证，配额管理
- **使用统计**：请求日志、Token 统计、成本计算
- **Web 管理界面**：Vue 3 + Tailwind CSS
- **管理员聊天**：管理后台内置 AI 聊天功能，支持思考过程显示（DeepSeek R1、Claude Extended Thinking 等推理模型）

## 核心概念

### 数据模型关系

```
Provider (供应商) 1 ←───→ N ProviderKey (供应商密钥)
        ↑                          ↑
        │                          │ 1
        │ 1                        │
        └──────── Upstream (上游模型) ───────→ N Model (对外模型)
```

- **供应商（Provider）**：模型供应商，如 OpenAI、Anthropic
- **供应商密钥（ProviderKey）**：供应商的 API Key
- **对外模型（Model）**：系统对外暴露的模型名称，包含 `provider_type` 属性（openai/anthropic/openai_compatible）
- **上游模型（Upstream）**：实际调用的供应商模型，一个对外模型可包含多个上游模型，实现负载均衡
  - 上游模型的供应商类型必须与所属模型的 `provider_type` 匹配

### 模型类型约束

- 模型创建时必须指定 `provider_type`，创建后不可修改
- `(name, provider_type)` 为组合唯一索引，支持同名不同类型的模型
- 添加上游模型时，只能选择与模型类型匹配的供应商

### 负载均衡

负载均衡在**上游模型（Upstream）**级别实现：
- 支持权重：同模型的上游模型按权重随机选择（平滑加权轮询）

### BU 计量单位

系统使用抽象计量单位 BU（Basic Unit），统一表示价格、配额和费用：

- **最小单位**: 纳 BU（1 nBU = 10^-9 BU）
- **换算**: 1000 纳 = 1 微，1000 微 = 1 毫，1000 毫 = 1 BU
- **存储格式**: int64 纳 BU/K tokens
- **显示格式**: BU/M tokens（前端输入/显示）
- **换算公式**: 显示值 = 存储值 × 10^-6

BU 作为抽象单位，后续可与人民币、美元等货币换算。

## 快速开始

```bash
# 初始化数据库并运行后端
make dev

# 前端开发
make web-install  # 安装前端依赖
make web-dev      # 启动前端开发服务器
```

运行 `make help` 查看所有可用命令。

## 配置

配置文件位于 `configs/config.yaml`，关键配置项：

| 配置项 | 说明 |
|--------|------|
| `security.jwt_secret` | JWT 签名密钥 |
| `admin` | 初始管理员账户 |

## 项目结构

```
AIRouter/
├── cmd/airouter/         # 入口程序
├── internal/             # 后端核心代码
│   ├── api/              # API 层
│   ├── config/           # 配置管理
│   ├── model/            # 数据模型
│   ├── provider/         # 供应商客户端
│   ├── service/          # 业务逻辑
│   └── store/            # 数据存储
├── pkg/                  # 可导出包
├── web/                  # 前端项目
└── configs/              # 配置文件
```

## 文档

详细设计文档请参阅 [docs/system-design.md](docs/system-design.md)，包含：

- 系统架构设计
- 数据模型设计
- API 设计（对外 API + 管理 API）
- 技术选型
- 核心功能模块说明

## 开发进度

- [x] 阶段一：基础框架
- [x] 阶段二：用户认证与 API 代理核心
- [x] 阶段三：供应商管理
- [x] 阶段四：路由与负载均衡
- [x] 阶段五：用户 API 接口
- [x] 阶段六：管理 API 与前端
- [x] 阶段七：高级功能
- [x] 数据模型重构：引入 Upstream 概念
- [x] 模型类型属性：provider_type 字段，同名不同类型模型支持
- [ ] 阶段八：部署与文档（暂缓）

## License

MIT