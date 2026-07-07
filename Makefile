.PHONY: build run dev clean cross-build build-windows

build:
	go build -o bin/server.exe cmd/server/main.go

run: build
	.\bin\server.exe

dev:
	go run cmd/server/main.go

clean:
	rm -f bin/server.exe
	rm -rf dist/

# 跨平台编译
cross-build:
	cd web && npm run build
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
	GOOS=darwin GOARCH=amd64 go build -o dist/KnowledgeClip-macos-intel cmd/server/main.go
	GOOS=darwin GOARCH=arm64 go build -o dist/KnowledgeClip-macos-arm64 cmd/server/main.go
	@echo "构建完成，产物在 dist/ 目录"

# 仅编译 Windows 版本（当前开发环境）
build-windows:
	cd web && npm run build
	go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe cmd/server/main.go
