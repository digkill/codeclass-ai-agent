.PHONY: build run dev tidy

build:
	go build -o ai-agent .

run: build
	./ai-agent

dev:
	go run .

tidy:
	GOPROXY="https://goproxy.cn,direct" go mod tidy
