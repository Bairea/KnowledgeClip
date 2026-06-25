.PHONY: build run dev clean

build:
	go build -o bin/server.exe cmd/server/main.go

run: build
	.\bin\server.exe

dev:
	go run cmd/server/main.go

clean:
	rm -f bin/server.exe
