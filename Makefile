.PHONY: build run dev clean cross-build build-windows

build:
	go build -o bin/server.exe ./cmd/server/

run: build
	.\bin\server.exe

dev:
	go run ./cmd/server/

clean:
	rm -f bin/server.exe
	rm -rf dist/

# 跨平台编译
cross-build:
	cd web && npm run build
	mkdir -p dist
	rm -rf dist/configs dist/data dist/.browser-data
	GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe ./cmd/server/
	GOOS=darwin GOARCH=amd64 go build -o dist/KnowledgeClip-macos-intel ./cmd/server/
	GOOS=darwin GOARCH=arm64 go build -o dist/KnowledgeClip-macos-arm64 ./cmd/server/
	@echo "构建完成，产物在 dist/ 目录"

# 仅编译 Windows 版本（当前开发环境）
build-windows:
	cd web && npm run build
	mkdir -p dist
	rm -rf dist/configs dist/data dist/.browser-data
	go build -ldflags "-H windowsgui" -o dist/KnowledgeClip-windows.exe ./cmd/server/

# 复制 browser-act JS 脚本到 dist
copy-scripts:
	if exist scripts\browser-act xcopy /E /I /Y scripts\browser-act dist\scripts\browser-act
