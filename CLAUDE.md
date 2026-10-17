# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

必须遵守的规则、交付前验证清单见下方导入的 AGENTS.md；本文件只补充常用命令和跨文件才能看懂的架构要点，不重复 AGENTS.md 的内容。

@AGENTS.md

## 常用命令

```bash
# 后端（:8080，仅 API，不挂 SPA）
go run ./cmd/server                      # 默认 -mode all（HTTP + 后台 worker）；-mode api / -mode worker 可拆分
go run ./cmd/server admin reset-password # 运维子命令：list-admins / reset-password / reset-2fa

# 测试
go test ./...
go test ./internal/architecture/...                          # 分层守卫
go test ./internal/modules/order/...                         # 单个模块
go test ./internal/modules/order/... -run TestXxx -v         # 单个用例
go test ./internal/app/httpserver/ -run TestAllAdminRoutesCoveredByBuiltinRoles  # RBAC 覆盖校验

# 前端（必须先 cd 进目录；dev server 把 /api、/uploads、/sitemap.xml、/robots.txt 代理到 :8080）
cd frontend/user  && pnpm install && pnpm run dev    # :5173
cd frontend/admin && pnpm install && pnpm run dev    # :5174
# pnpm run test：user 为 node --test tests/*.test.ts；admin 为 vitest + node --test
# pnpm run build 内含 vue-tsc 类型检查

# 发布前检查
python3 scripts/check-release-safety.py
python3 -m unittest discover -s scripts/tests -p '*_test.py'
```

全栈二进制：admin 必须用 `pnpm run build:fullstack`（注入运行时 `<base href="__DJ_ADMIN_BASE__/">` 占位符，服务端启动时替换），**不是** `build`——后者把 base 固定为 `/`，自定义 `web.admin_path` 会静默失效。完整步骤见 README「Building the Full-Stack Binary」，或 `goreleaser build --snapshot --single-target --clean`。

## 架构要点

- **装配链路**：`cmd/server` → `internal/app/container`（DI 容器）→ `internal/bootstrap/<module>/{adapters.go,wiring.go}`（把各模块的 gormstore / gateway 适配到对方 `contract/` 端口）→ `internal/app/httpserver`（Gin 路由与中间件）、`internal/app/jobs`（asynq worker）。新增模块或跨模块依赖时，接线写在 bootstrap，不要在模块内部直接 new 其他模块的实现。
- **模块分层**：`domain` → `application`（用例 + 端口）→ `infrastructure`（`gormstore` 等适配器）/ `transport`（HTTP handler）；`contract/` 既是本模块 application 依赖的端口，也是对其他模块暴露的唯一表面。跨多个模块的用例放 `internal/workflows/`。
- **事务**：store 通过 `WithinTransaction(func(tx contract.Transaction) error)` 开事务，闭包内其他 store 用 `WithTx(tx)` 绑定（SQLite 单连接死锁问题见 AGENTS.md）。
- **Redis / asynq 是可选的**：没有 Redis 时服务仍可启动，异步任务相关改动要考虑这种降级路径。
- **鉴权**：admin 与 user 是两套独立的 JWT 领域（`jwt.secret` / `user_jwt.secret`），admin 路由额外经过 Casbin。
- **配置加载顺序**：默认值 → `config.yml` → 同目录 `config.private.yml`（凭证、连接地址）→ 环境变量。可用 `DUJIAO_CONFIG_FILE` / `DUJIAO_PRIVATE_CONFIG_FILE` 指定路径。两个运行时文件都被 gitignore，只改对应的 `.example`。不要重新生成 `app.secret_key`——已存储的密文依赖它解密。
- **构建 tag**：无 tag = 仅 API；`fullstack` = `go:embed` 前端（`internal/web/dist/{admin,user}` 两个目录都必须存在，否则编译失败）；`release` = 生产环境的外链 URL 构建行为。

## 分支

开发在 `codex/dev`，生产在 `codex/deploy-aiccpay`；版本 tag 必须指向已合入生产分支的提交。
