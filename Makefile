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

# 真实上游（需要 DEEPSEEK_API_KEY）
test-live:
	DEEPSEEK_API_KEY=$(DEEPSEEK_API_KEY) go test ./internal/gateway/ -run Live -v -timeout 180s

# 五个场景的对话质量检查：改完编排逻辑后跑一次
quality:
	@node scripts/quality.mjs

# prompt 注入防御（需要真实 Key）
injection:
	@node scripts/injection.mjs

# 界面上不该出现 undefined / NaN / null（十个页面各扫一遍）
junk:
	@node scripts/junk.mjs

# 真实输出的卫生检查：前缀、出戏、空发言、复读、超长（需要真实 Key）
sanity:
	@node scripts/sanity.mjs

# 长时间运行 / 并发
soak:
	@ROUNDS=100 node scripts/soak.mjs
concurrent:
	@USERS=20 ROUNDS=2 node scripts/concurrent.mjs
resilience:
	@node scripts/resilience.mjs

# 新用户走查（不是测试，输出给人看）
walk:
	@node scripts/walk.mjs

# 界面完整性：按钮/菜单项/图标都有活的逻辑
audit:
	@python3 scripts/audit-ui.py

# 备份 data/ 到 backups/（只保留最近 20 份）
backup:
	@./scripts/backup.sh 20

# 从备份恢复
restore:
	@./scripts/restore.sh $(FILE)

# 备份/恢复往返测试
test-backup:
	@./scripts/backup_test.sh

# 装一份可以直接看的演示数据（3 个群，真实对话）然后启动
demo:
	@./scripts/demo.sh

# 只装演示数据，不启动
demo-data:
	@./scripts/demo.sh --only

# 浏览器端到端
e2e:
	@python3 scripts/audit-ui.py
	@node scripts/e2e.mjs
	@node scripts/e2e-mobile.mjs
	@node scripts/multi-tab.mjs
	@node scripts/keyboard.mjs

# 清掉构建产物、本地数据和测试留下的临时文件
# （Chrome 的 profile 每个几百 MB，跑几十次浏览器测试就能把磁盘占满）
clean:
	@rm -rf bin data 2>/dev/null || true
	@rm -rf /tmp/ens-e2e-* /tmp/ens-tabs-* /tmp/kb-* /tmp/walk-* /tmp/dg*-* /tmp/nt-* /tmp/ul-* 2>/dev/null || true
	@echo "已清理：bin/ data/ 和临时 profile"

# 清空本地数据
reset:
	rm -rf data
