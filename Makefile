.PHONY: build run dev clean

# 完整构建：前端 + 后端，产出 Windows GUI 单二进制 bin/KnowledgeClip.exe
build:
	cd web && npm run build
	go build -ldflags "-s -w -H=windowsgui" -o bin/KnowledgeClip.exe ./cmd/server/

run: build
	.\bin\KnowledgeClip.exe

# 开发模式（go run，不做 GUI 打包）
dev:
	go run ./cmd/server/

clean:
	rm -f bin/KnowledgeClip.exe
	rm -rf dist/
