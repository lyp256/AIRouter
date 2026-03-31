# AIRouter Makefile
# 项目配置
APP_NAME := airouter
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GO_VERSION := $(shell go version | awk '{print $$3}')

# 目录配置
CMD_DIR := ./cmd/airouter
BIN_DIR := ./bin
WEB_DIR := ./web
DATA_DIR := ./data

# 构建标志
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 默认目标
.PHONY: all
all: build

# ============================================
# 构建相关
# ============================================

.PHONY: build
build: web-build ## 编译后端（包含前端嵌入）
	@echo ">>> 编译后端..."
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)
	@echo ">>> 编译完成: $(BIN_DIR)/$(APP_NAME)"

.PHONY: build-server
build-server: ## 仅编译后端（不含前端，开发用）
	@echo ">>> 编译后端（不嵌入前端）..."
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(APP_NAME) $(CMD_DIR)
	@echo ">>> 编译完成: $(BIN_DIR)/$(APP_NAME)"

.PHONY: build-linux
build-linux: web-build ## 交叉编译 Linux amd64（包含前端嵌入）
	@echo ">>> 交叉编译 Linux amd64..."
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(APP_NAME)-linux-amd64 $(CMD_DIR)
	@echo ">>> 编译完成: $(BIN_DIR)/$(APP_NAME)-linux-amd64"

.PHONY: build-arm64
build-arm64: web-build ## 交叉编译 Linux arm64（包含前端嵌入）
	@echo ">>> 交叉编译 Linux arm64..."
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/$(APP_NAME)-linux-arm64 $(CMD_DIR)
	@echo ">>> 编译完成: $(BIN_DIR)/$(APP_NAME)-linux-arm64"

.PHONY: build-all
build-all: build-linux build-arm64 ## 交叉编译所有平台

.PHONY: vendor
vendor: ## 更新 vendor 目录
	go mod vendor

# ============================================
# 运行相关
# ============================================

.PHONY: run
run: ## 运行后端服务
	go run $(CMD_DIR)

.PHONY: run-built
run-built: build ## 运行编译后的服务
	./$(BIN_DIR)/$(APP_NAME)

# ============================================
# 测试相关
# ============================================

.PHONY: test
test: ## 运行测试
	go test -v ./...

.PHONY: test-coverage
test-coverage: ## 运行测试并生成覆盖率报告
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo ">>> 覆盖率报告已生成: coverage.html"

.PHONY: lint-install
lint-install: ## 安装 golangci-lint 工具
	@echo ">>> 安装 golangci-lint..."
	@which golangci-lint > /dev/null && echo ">>> golangci-lint 已安装" || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo ">>> 安装完成"

.PHONY: lint
lint: lint-install ## 运行代码检查（自动安装 golangci-lint）
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix: lint-install ## 运行代码检查并自动修复问题
	golangci-lint run --fix ./...

# ============================================
# 前端相关
# ============================================

.PHONY: web-install
web-install: ## 安装前端依赖
	@echo ">>> 安装前端依赖..."
	cd $(WEB_DIR) && npm install
	@echo ">>> 前端依赖安装完成"

.PHONY: web-dev
web-dev: ## 启动前端开发服务器
	cd $(WEB_DIR) && npm run dev

.PHONY: web-build
web-build: ## 构建前端生产版本
	@echo ">>> 构建前端..."
	cd $(WEB_DIR) && npm run build
	@echo ">>> 前端构建完成: $(WEB_DIR)/dist"

.PHONY: web-preview
web-preview: ## 预览前端构建结果
	cd $(WEB_DIR) && npm run preview

# ============================================
# 数据库相关
# ============================================

.PHONY: db-init
db-init: ## 初始化数据库目录
	@mkdir -p $(DATA_DIR)
	@echo ">>> 数据库目录已创建: $(DATA_DIR)"

.PHONY: db-reset
db-reset: ## 重置数据库（删除并重新初始化）
	@echo ">>> 重置数据库..."
	rm -rf $(DATA_DIR)/*.db
	@mkdir -p $(DATA_DIR)
	@echo ">>> 数据库已重置"

# ============================================
# Docker 相关
# ============================================

.PHONY: docker-build
docker-build: ## 构建 Docker 镜像
	docker build -t $(APP_NAME):$(VERSION) .
	docker tag $(APP_NAME):$(VERSION) $(APP_NAME):latest

.PHONY: docker-run
docker-run: ## 运行 Docker 容器
	docker run -d --name $(APP_NAME) \
		-p 8080:8080 \
		-v $(PWD)/data:/app/data \
		-v $(PWD)/configs:/app/configs \
		$(APP_NAME):latest

.PHONY: docker-stop
docker-stop: ## 停止 Docker 容器
	docker stop $(APP_NAME) || true
	docker rm $(APP_NAME) || true

.PHONY: docker-compose-up
docker-compose-up: ## 使用 docker-compose 启动服务
	docker-compose up -d

.PHONY: docker-compose-down
docker-compose-down: ## 使用 docker-compose 停止服务
	docker-compose down

# ============================================
# 清理相关
# ============================================

.PHONY: clean
clean: ## 清理构建产物
	@echo ">>> 清理构建产物..."
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html
	@echo ">>> 清理完成"

.PHONY: clean-all
clean-all: clean ## 清理所有生成文件（包括前端）
	@echo ">>> 清理前端构建产物..."
	rm -rf $(WEB_DIR)/dist
	rm -rf $(WEB_DIR)/node_modules
	@echo ">>> 清理完成"

# ============================================
# 开发辅助
# ============================================

.PHONY: fmt
fmt: ## 格式化代码
	go fmt ./...

.PHONY: vet
vet: ## 运行 go vet
	go vet ./...

.PHONY: check
check: fmt vet lint ## 代码检查（格式化 + vet + lint）

.PHONY: dev
dev: db-init run ## 开发模式（初始化数据库并运行）

.PHONY: dev-full
dev-full: web-install db-init ## 完整开发环境准备
	@echo ">>> 开发环境准备完成"
	@echo ">>> 运行 'make run' 启动后端"
	@echo ">>> 运行 'make web-dev' 启动前端"

# ============================================
# 帮助
# ============================================

.PHONY: help
help: ## 显示帮助信息
	@echo "AIRouter Makefile 帮助"
	@echo ""
	@echo "使用方法: make [目标]"
	@echo ""
	@echo "可用目标:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "常用命令:"
	@echo "  make build       - 编译后端"
	@echo "  make run         - 运行后端服务"
	@echo "  make web-dev     - 启动前端开发服务器"
	@echo "  make test        - 运行测试"
	@echo "  make dev         - 初始化数据库并运行"