# AIRouter 配置文件说明

配置文件默认路径 `configs/config.yaml`，也可通过 `-c` 参数指定路径。

所有配置项均支持环境变量覆盖（Viper AutomaticEnv）。

---

## 完整配置示例

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  type: "sqlite"
  path: "./data/airouter.db"

security:
  jwt_secret: "your-jwt-secret-key"
  jwt_expire: "24h"

logging:
  level: "info"
  format: "json"

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

cache:
  enabled: true
  type: "memory"
  ttl: "10m"
  size: 64

health_check:
  enabled: true
  full_check_interval: "5m"
  recovery_interval: "30s"
  timeout: "10s"
  healthy_threshold: 2
  unhealthy_threshold: 3
  leader_lease: "30s"
  leader_renew_interval: "10s"
```

---

## 配置项详细说明

### server — 服务监听

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `host` | string | `0.0.0.0` | 监听地址 |
| `port` | int | `8080` | 监听端口 |

### database — 数据库

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `type` | string | `sqlite` | 数据库类型：`sqlite` 或 `postgres` |
| `path` | string | `./data/airouter.db` | SQLite 数据库文件路径（type=sqlite 时） |
| `host` | string | | PostgreSQL 地址（type=postgres 时） |
| `port` | int | | PostgreSQL 端口 |
| `user` | string | | PostgreSQL 用户名 |
| `password` | string | | PostgreSQL 密码 |
| `database` | string | | PostgreSQL 数据库名 |

### security — 安全

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `jwt_secret` | string | | JWT Token 签名密钥 |
| `jwt_expire` | duration | `24h` | JWT Token 过期时间 |

### logging — 日志

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `level` | string | `info` | 日志级别：`debug`、`info`、`warn`、`error` |
| `format` | string | `json` | 日志格式：`json` 或 `text` |

### rate_limit — 限流

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `true` | 是否启用限流 |
| `default_rpm` | int | `60` | 默认每分钟最大请求数 |

### retry — 重试

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `true` | 是否启用重试 |
| `max_attempts` | int | `3` | 最大请求次数（含首次请求） |
| `initial_wait` | duration | `1s` | 首次重试等待时间 |
| `max_wait` | duration | `30s` | 最大等待时间上限 |
| `multiplier` | float | `2.0` | 指数退避乘数 |
| `retry_on_codes` | []int | `[429,500,502,503,504]` | 触发重试的上游 HTTP 状态码 |

### admin — 初始管理员

首次启动时自动创建，仅当数据库中无管理员用户时生效。

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `username` | string | `admin` | 管理员用户名 |
| `password` | string | `changeme` | 管理员密码（**请务必修改**） |
| `email` | string | `admin@example.com` | 管理员邮箱 |

### cache — 缓存

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `true` | 是否启用缓存 |
| `type` | string | `memory` | 缓存类型：`memory`（内存）或 `redis` |
| `ttl` | duration | `10m` | 默认缓存过期时间 |
| `size` | int | `64` | 内存缓存大小（MB），仅 type=memory 时生效 |
| `redis.addr` | string | | Redis 地址（如 `localhost:6379`），仅 type=redis 时生效 |
| `redis.password` | string | | Redis 密码 |
| `redis.db` | int | `0` | Redis 数据库编号 |

**缓存用途**：
- 模型配置缓存（`model:name:{name}:type:{type}`）
- 上游模型列表缓存（`upstreams:model:{modelID}`）
- 供应商/密钥信息缓存（`provider:{id}`、`provider_key:{id}`）
- 上游健康状态（`upstream:health:{upstreamID}`，TTL 1 小时）
- 分布式选主（`leader:health-check:*`）

### health_check — 健康检查

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `true` | 是否启用健康检查服务 |
| `full_check_interval` | duration | `5m` | 全量检查间隔，对所有启用上游执行探测 |
| `recovery_interval` | duration | `30s` | 恢复检查间隔，仅对不健康上游执行探测 |
| `timeout` | duration | `10s` | 单次探测 HTTP 超时时间 |
| `healthy_threshold` | int | `2` | 连续成功达到此阈值后标记为健康 |
| `unhealthy_threshold` | int | `3` | 连续失败达到此阈值后标记为不健康 |
| `leader_lease` | duration | `30s` | 分布式选主租约时长 |
| `leader_renew_interval` | duration | `10s` | 选主续约间隔 |

**工作机制**：
- 探测方式：向供应商 `/v1/models` 端点发送 GET 请求，HTTP 429（限流）不算不健康
- 按 `(供应商ID, 密钥ID)` 去重，相同凭据的上游只探测一次
- 健康状态存储在缓存中（`upstream:health:{upstreamID}`），不写入数据库
- 缓存中无记录的上游视为健康
- Redis 缓存模式下通过分布式选主保证只有一个实例执行检查
- 内存缓存模式下各实例独立运行检查

---

## 时间格式说明

duration 类型支持以下单位：

| 单位 | 示例 | 说明 |
|------|------|------|
| `ns` | `100ns` | 纳秒 |
| `us` / `µs` | `100us` | 微秒 |
| `ms` | `100ms` | 毫秒 |
| `s` | `30s` | 秒 |
| `m` | `5m` | 分钟 |
| `h` | `24h` | 小时 |
