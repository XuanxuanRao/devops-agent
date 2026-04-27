# =====================================================================
# devops-agent Makefile
#
# 常用目标：
#   make build        编译 agent 到 bin/agent
#   make run          使用 ./config.yaml 直接运行 agent（不落盘）
#   make test         运行全部单元测试
#   make test-race    带 -race 运行测试
#   make cover        生成覆盖率报告到 coverage.out / coverage.html
#   make fmt          gofmt + goimports（若存在）格式化全部 Go 源码
#   make vet          go vet
#   make lint         golangci-lint（需本地已安装）
#   make tidy         go mod tidy
#   make clean        清理 bin/ 与覆盖率产物
#   make build-linux  交叉编译 linux/amd64 产物（适合部署到服务器）
#
# 可覆盖变量：
#   make build VERSION=1.2.3
#   make run CONFIG=./other.yaml
# =====================================================================

# ---- 可覆盖的变量 ----
BINARY       := agent
CMD_PKG      := ./cmd/agent
BIN_DIR      := bin
BIN          := $(BIN_DIR)/$(BINARY)
CONFIG       ?= ./config.yaml

# 版本信息：优先使用环境变量 VERSION，其次取 git describe，最后回落 dev
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT       := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS      := -s -w \
                -X 'main.version=$(VERSION)' \
                -X 'main.commit=$(COMMIT)' \
                -X 'main.buildTime=$(BUILD_TIME)'

GOFLAGS      ?=
GOTESTFLAGS  ?= -count=1

# 覆盖率文件
COVER_OUT    := coverage.out
COVER_HTML   := coverage.html

# 让目标始终被执行（不依赖同名文件时间戳）
.PHONY: help build run test test-race cover fmt vet lint tidy clean build-linux build-darwin all

# 默认目标：打印帮助
.DEFAULT_GOAL := help

help: ## 显示本 Makefile 支持的目标
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make <target>\n\nTargets:\n"} \
	      /^[a-zA-Z_-]+:.*##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ---- 构建 ----
build: ## 编译 agent 到 bin/agent
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) $(CMD_PKG)
	@echo "built $(BIN) ($(VERSION) / $(COMMIT))"

build-linux: ## 交叉编译 linux/amd64 产物到 bin/agent-linux-amd64
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
		go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/$(BINARY)-linux-amd64 $(CMD_PKG)

build-darwin: ## 交叉编译 darwin/arm64 产物到 bin/agent-darwin-arm64
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
		go build $(GOFLAGS) -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/$(BINARY)-darwin-arm64 $(CMD_PKG)

# ---- 运行 ----
run: ## 直接用 go run 启动 agent（读取 $(CONFIG)）
	go run $(CMD_PKG) -config $(CONFIG)

# ---- 测试 ----
test: ## 运行全部单元测试
	go test $(GOTESTFLAGS) ./...

test-race: ## 带 race detector 运行测试
	go test $(GOTESTFLAGS) -race ./...

cover: ## 生成覆盖率报告到 coverage.out / coverage.html
	go test $(GOTESTFLAGS) -coverprofile=$(COVER_OUT) ./...
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@echo "coverage report: $(COVER_HTML)"

# ---- 代码质量 ----
fmt: ## gofmt + goimports 格式化（goimports 存在则一并运行）
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || \
	  echo "goimports not installed, skip (install: go install golang.org/x/tools/cmd/goimports@latest)"

vet: ## go vet 静态检查
	go vet ./...

lint: ## 运行 golangci-lint（需本地已安装）
	@command -v golangci-lint >/dev/null 2>&1 || { \
	  echo "golangci-lint not installed"; \
	  echo "install: brew install golangci-lint  或  https://golangci-lint.run/usage/install/"; \
	  exit 1; \
	}
	golangci-lint run ./...

tidy: ## 整理 go.mod / go.sum
	go mod tidy

# ---- 清理 ----
clean: ## 清理构建与覆盖率产物
	rm -rf $(BIN_DIR) $(COVER_OUT) $(COVER_HTML)
	rm -f ./$(BINARY)

# ---- 组合 ----
all: fmt vet test build ## 一键走完 fmt + vet + test + build
