.PHONY: run build clean test

# 本地启动：没有 Key 也能跑（自动降级 mock）
run:
	go run ./cmd/server

# 带真实 Key 启动
run-real:
	DEEPSEEK_API_KEY=$(DEEPSEEK_API_KEY) go run ./cmd/server

build:
	go build -o bin/ensemble ./cmd/server

# 交叉编译
release:
	GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w" -o bin/ensemble-linux-amd64 ./cmd/server
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/ensemble-darwin-arm64 ./cmd/server
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/ensemble-windows-amd64.exe ./cmd/server

test:
	go test ./... -count=1

clean:
	rm -rf bin data

# 清空本地数据
reset:
	rm -rf data
