.PHONY: build test clean all linux macos windows

# 输出目录
OUTPUT_DIR := bin

# 可执行文件名
BINARY := agent

# 默认目标
all: test linux

# 构建目标（先测试后编译所有平台）
build: test linux macos windows

# Linux 构建目标
linux:
	@mkdir -p $(OUTPUT_DIR)/linux
	# 针对 Linux 服务器编译
	GOOS=linux GOARCH=amd64 go build -o $(OUTPUT_DIR)/linux/$(BINARY) main.go

# macOS 构建目标
macos:
	@mkdir -p $(OUTPUT_DIR)/macos
	# 针对 macOS 编译
	GOOS=darwin GOARCH=amd64 go build -o $(OUTPUT_DIR)/macos/$(BINARY) main.go

# Windows 构建目标
windows:
	@mkdir -p $(OUTPUT_DIR)/windows
	# 针对 Windows 编译
	GOOS=windows GOARCH=amd64 go build -o $(OUTPUT_DIR)/windows/$(BINARY).exe main.go

# 测试目标
test:
	go test -v ./...

# 清理目标
clean:
	@rm -rf $(OUTPUT_DIR)

# 运行目标（用于本地测试）
run:
	go run main.go
