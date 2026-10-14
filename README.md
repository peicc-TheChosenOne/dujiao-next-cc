# Dujiao-Next

Dujiao-Next is a digital goods e-commerce platform. This repository contains the complete
application: the Go backend, the customer storefront, and the admin panel.

## ❤️ Brand Partners (Sponsors)

<table>

<tr>
<td width="180"><a href="https://www.vmrack.net/?ref_code=5iXmGUMf5f5"><img src="assets/partners/vmrack.jpeg" alt="CCTK.AI" width="150"></a></td>
<td><a href="https://www.vmrack.net/?ref_code=5iXmGUMf5f5">Vmrack.com</a> 全球自动化云基础设施服务商 提供先进的云服务器、裸金属、CDN、媒体处理、对象存储和网络解决方案，助力企业轻松上云。
⚡️官方合作闪购款，仅需76刀/年，三网优化线路，助力您业务起飞，<a href="https://www.vmrack.net/vps/flash-deals/2082383856451452928?ref_code=5iXmGUMf5f5">👉点我直达</a>
</td>
</tr>

<tr>
<td width="180"><a href="https://www.99cdn.com/"><img src="assets/partners/99cdn.jpg" alt="openmodel" width="150"></a></td>
<td>99CDN 自建 CDN 平台，自主管理节点 · 智能流量调度 · 多级缓存加速。 <a href="https://www.99cdn.com/">99CDN</a> 是 EasyLink 旗下的商业化自建 CDN 与 DNS 智能调度平台，支持边缘缓存、分片缓存、多级回源、GTM 调度与边缘计算能力。</td>
</tr>

<tr>
<td width="180"><a href="https://niub.me"><img src="assets/partners/niub.png" alt="openmodel" width="150"></a></td>
<td> <a href="https://niub.me">NIUB — 数字服务，一站直达(DujiaoNext自营旗舰店)</a> 正在寻找更便捷的 AI 服务、社交账号或数字礼品卡？NIUB（niub.me）专注提供多种虚拟商品与数字服务，让不同类型的数字需求都能在一个站点完成选购。
我们重视清晰的商品信息、明确的交付方式和负责任的售后支持。每件商品的账号类型、适用地区、有效期限、使用条件与售后范围，均以对应商品页面说明为准。
访问 NIUB，探索更多数字服务与虚拟商品。</td>
</tr>



</table>

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26 · Gin · GORM · SQLite / PostgreSQL |
| Auth | JWT (separate admin / user realms) · Casbin RBAC · TOTP 2FA |
| Async | asynq on Redis (optional — the server runs without it) |
| Config | Viper (`config.yml` + ignored `config.private.yml` + environment overrides) |
| Frontend | Vue 3 · Vite · TypeScript · Tailwind CSS v4 · pnpm 10 |
| Admin UI | shadcn-vue / reka-ui |

## Repository Layout

```
.
├── cmd/server/               # entry point; also hosts the `admin` operator subcommands
├── internal/
│   ├── app/                  # composition root
│   │   ├── container/        # dependency-injection container
│   │   ├── httpserver/       # Gin router, route groups, middleware
│   │   └── jobs/             # asynq worker service and consumers
│   ├── bootstrap/            # per-module wiring (adapters.go + wiring.go)
│   ├── modules/              # 35 business modules — one vertical slice per domain
│   ├── workflows/            # use cases that span several modules
│   ├── platform/             # framework-facing infrastructure
│   │   ├── database/gormdb/  # connection, auto-migration
│   │   └── http/             # response envelope, Gin helpers
│   ├── shared/               # dependency-free primitives (money, jsonmap, serial …)
│   ├── authz/                # Casbin RBAC: policy model, built-in role seeds
│   ├── web/                  # SPA embedding and mounting (build-tag gated)
│   ├── architecture/         # architecture guard tests — no production code
│   ├── cache/ config/ constants/ crypto/ i18n/ logger/ queue/ version/
│   └── admincmd/ htmltext/ persistence/ telegramidentity/ testkit/ upstream/
├── frontend/
│   ├── admin/                # admin panel SPA        (dev :5174)
│   └── user/                 # customer storefront SPA (dev :5173)
├── config.yml.example
├── config.private.yml.example # credentials and connection-address template
├── Dockerfile                # single full-stack image
└── .goreleaser.yaml
```

Runtime directories created on first start: `db/` (SQLite), `uploads/`, `logs/`.

## Architecture

A modular monolith. Each domain under `internal/modules/<name>/` is a vertical slice with its
own layers:

| Layer | Holds | May import |
| --- | --- | --- |
| `domain/` | entities, value objects, business invariants | nothing from the other layers |
| `application/` | use cases, port interfaces | `domain`, `contract` |
| `infrastructure/` | GORM stores, gateways, queue adapters | `domain`, `application` ports |
| `transport/` | HTTP handlers, presenters | `application` contracts |
| `contract/` | port interfaces the application layer depends on, and the module's public surface for other modules | — |

**These rules are enforced by tests, not convention.** `internal/architecture/` parses every
import in the tree and fails the build on violations. The main ones:

- `domain` must not reach into `application`, `infrastructure`, or `transport`
- `application` must not import Gin or asynq — no transport libraries in use cases
- only a module's `infrastructure/gormstore` adapter may import GORM
- `transport` depends on application contracts, never on concrete stores
- `internal/shared` stays free of modules, GORM, Gin, and asynq
- `internal/platform` must not depend on business modules

Run them with the rest of the suite: `go test ./internal/architecture/...`

Modules never import each other's internals — they talk through `contract/`, and the wiring
lives in `internal/bootstrap/<module>/`.

### RBAC

Every `/api/v1/admin/...` route passes through Casbin. The permission catalog is generated
from the live route table, but the **built-in roles are hand-maintained** in
`internal/authz/bootstrap.go`. Adding an admin route without adding it to a role seed leaves
that route reachable only by the super admin. `internal/app/httpserver/rbac_coverage_test.go`
checks that every registered route is covered.

## Build Tags

| Tag | Effect |
| --- | --- |
| *(none)* | API only. No SPAs mounted — the default for local development. |
| `fullstack` | Embeds `internal/web/dist/{admin,user}` into the binary via `go:embed`. |
| `release` | Production behavior for outbound URL building. |

`go:embed all:dist/admin all:dist/user` requires **both** directories to exist, so a
`fullstack` build fails outright if the frontends were not built first. A plain `go build`
does not compile `embed_fullstack.go` — after touching `internal/web/`, verify with
`go build -tags release,fullstack ./cmd/server`.

## Run Modes

```bash
./dujiao-next                 # all    — HTTP server + background worker (default)
./dujiao-next -mode api       # HTTP server only
./dujiao-next -mode worker    # background worker only
```

Operator subcommands ship in the same binary, so a container needs no extra tooling:

```bash
./dujiao-next admin list-admins
./dujiao-next admin reset-password
./dujiao-next admin reset-2fa
```

## Frontend Notes

Two independent SPAs, both built with Vite and embedded at release time.

**Mount points.** The storefront is served at `/`; the admin panel at `web.admin_path`
(default `/admin`). `/api`, `/uploads`, and `/health` are reserved prefixes — an unmatched
path under them returns 404 instead of falling through to the SPA shell. Adding a new
top-level backend prefix means updating `reservedPaths` in `internal/web/handler.go`.

**The admin base path is resolved at runtime, not at build time.** Since `web.admin_path` is
configurable, `pnpm run build:fullstack` only injects a `<base href="__DJ_ADMIN_BASE__/">`
placeholder, which the server rewrites on startup. Consequences for admin code:

- native `<a href>` and `window.location` navigation must go through `adminUrl()` in
  `src/utils/adminBase.ts`
- `<router-link :to>` and `router.push()` must **not** — vue-router already carries the base,
  and prefixing again yields `/admin/admin/...`

**Storefront templates.** The customer frontend ships more than one look, selected by the
`storefront_template` site setting (`classic`, `vault`). Template pages live in
`src/templates/<name>/` and fall back to `src/views/` when a page has no template-specific
version; see `src/templates/registry.ts`. Append `?template=vault` to preview one locally.

**i18n.** Both frontends and all API responses are localized — Simplified Chinese, Traditional
Chinese, and English. Do not hard-code user-facing strings on either side.

## Quick Start (Deploy)

### Official one-click installer (Ubuntu / Debian)

On a fresh Ubuntu 22.04+ or Debian 12+ server, download and run the official
interactive installer:

```bash
curl -fsSL https://raw.githubusercontent.com/peicc-TheChosenOne/dujiao-next-cc/codex/deploy-aiccpay/scripts/dujiao-next-manager.sh \
  -o /tmp/dujiao-next-manager.sh
sudo bash /tmp/dujiao-next-manager.sh install
```

The installer deploys the release binary with systemd, an isolated local Redis,
Nginx, SQLite, and a Let's Encrypt certificate. After installation, reopen the
management menu with:

```bash
sudo dujiao-next-manager
```

Common automation-friendly commands are also available:

```bash
sudo dujiao-next-manager status
sudo dujiao-next-manager logs app
sudo dujiao-next-manager restart
sudo dujiao-next-manager configure-domain
sudo dujiao-next-manager configure-admin-path
sudo dujiao-next-manager renew-cert
sudo dujiao-next-manager admin-reset-password
sudo dujiao-next-manager admin-reset-2fa
sudo dujiao-next-manager uninstall
```

The first release supports a single non-wildcard domain on Ubuntu/Debian only.
It does not adopt an existing manual installation. If SMTP is skipped, configure
it in the admin panel before enabling email-verification registration. Application
data lives in `/opt/dujiao-next`; installer state is stored in
`/etc/dujiao-next/install-state.json`. TLS failures leave only the ACME challenge
endpoint enabled, and `install` can be rerun after DNS or firewall repair. Safe
uninstall creates and verifies a `0600` recovery archive under
`/var/backups/dujiao-next` before deleting managed data.

### Manual binary installation

Download the latest `dujiao-next_*.tar.gz` from [Releases](https://github.com/peicc-TheChosenOne/dujiao-next-cc/releases):

```bash
tar -xzf dujiao-next_*.tar.gz
cp config.yml.example config.yml
cp config.private.yml.example config.private.yml
# set ordinary runtime options in config.yml; fill credentials and connection addresses in config.private.yml
# use three independent strong keys for app.secret_key, jwt.secret and user_jwt.secret
./dujiao-next
```

Full instructions: https://dujiao-next.com/deploy/

Or with Docker:

```bash
# Replace vX.Y.Z with the release tag checked out in this source directory.
docker build --build-arg APP_VERSION=vX.Y.Z -t dujiao-next:vX.Y.Z .
docker run -d -p 8080:8080 -v $PWD/config.yml:/app/config.yml:ro -v $PWD/config.private.yml:/app/config.private.yml:ro dujiao-next:vX.Y.Z
```

## Quick Start (Develop)

The backend loads defaults, `config.yml`, its sibling `config.private.yml`, then environment
variables, in that order. Both runtime files stay outside Git and Docker build contexts;
only the `.example` templates are committed. Existing single-file installations still work.
Missing optional private files are allowed; an invalid private file or an explicitly selected
missing file stops startup without logging its contents.

Set `DUJIAO_CONFIG_FILE` and `DUJIAO_PRIVATE_CONFIG_FILE` when selecting files outside the
working directory. Do not use the Git branch to select credentials automatically: local
development and production deployments keep their own runtime files. Never regenerate
`app.secret_key` during an upgrade; stored secrets require the existing key for decryption.

Run the backend and the two frontends separately for hot reload:

```bash
go mod tidy && go run ./cmd/server   # :8080 — API only, no SPAs mounted

cd frontend/user  && pnpm install && pnpm run dev   # :5173
cd frontend/admin && pnpm install && pnpm run dev   # :5174
```

Both dev servers proxy `/api`, `/uploads`, `/sitemap.xml`, and `/robots.txt` to
`localhost:8080`. In production everything is same-origin, so these proxies are a
development-only concern.

> Use `pnpm` via corepack. `pnpm --dir X` does not read the `packageManager` field of the
> target directory and will pick the wrong version — `cd` into the package first.

## Building the Full-Stack Binary

Run `python3 scripts/check-release-safety.py` before preparing a release. It rejects runtime
files in Git inputs, public IP literals in deployment templates, production frontend `.env`
files, and an expanded release archive allowlist. An optional `--archives dist` also checks
built archives. These checks do not replace a review of history or server-side settings.

Development is on `codex/dev`; production is on `codex/deploy-aiccpay`. Version tags must
point to commits merged into production. The release workflow reruns the complete CI
workflow before publishing. Configuration, database data and uploaded files are managed on
the target server and are never replaced by the release archive.

```bash
goreleaser build --snapshot --single-target --clean
```

This builds both frontends, embeds them, and compiles with `-tags fullstack` — the same path
CI uses for releases. The manual equivalent:

```bash
(cd frontend/admin && pnpm run build:fullstack)   # injects the <base> placeholder
(cd frontend/user  && pnpm run build)
rm -rf internal/web/dist && mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
go build -tags release,fullstack -o dujiao-next ./cmd/server
```

Note that admin uses `build:fullstack`, not `build`. Plain `build` produces a bundle pinned to
`/`, which silently breaks a custom `web.admin_path`.

For automatic reseller domains, enable Cloudflare for SaaS on the platform zone and
configure `reseller.domain_binding` using the example files. Keep the API token in
`config.private.yml`, restricted to the platform zone's SSL and Certificates permission.
The queue synchronizes hostnames every minute. Submitting a root domain reserves the
root and `www` together; both hostname and certificate states must be active before
administrator approval opens the store. Defaults limit each reseller to five root/www
pairs and the platform to 100 hostnames; pending validation expires after seven days.
The provider quota is checked before remote creation. Increasing the configured
limit may incur additional Cloudflare charges. Temporary provider outages preserve approved
stores. Disabling a binding immediately closes the store and schedules remote cleanup.

Customers point both CNAME records to the configured entry hostname. Apex records need
DNS-provider CNAME flattening or ALIAS support. Cloudflare-proxied customer records use
Cloudflare for SaaS O2O; do not promise a fixed DNS/certificate completion time.

Set the SaaS fallback origin to a proxied hostname that points at the server, and point
the entry CNAME to it. Custom hostnames reach the origin with their own SNI, which a
platform certificate cannot match, so use the SaaS zone's Full (not strict) mode and
accept only Cloudflare connections at the origin: `deploy/nginx-cloudflare-saas.conf.example`
is a catch-all server that allows Cloudflare's published networks only, restores the
visitor IP from `CF-Connecting-IP`, and passes the original Host to the application.
Authenticated Origin Pulls can be added on top. Keep `reseller.trusted_forwarded_host:
false` and local-only application trusted proxies. DNS migration must preserve existing
records and DNSSEC settings. Test gray-cloud and orange-cloud customer domains before
retiring the previous edge service.

## Testing

```bash
go test ./...                              # full suite
go test ./internal/architecture/...        # dependency and layering guards
go test ./internal/modules/order/...       # one module

cd frontend/user  && pnpm run build        # includes vue-tsc type checking
cd frontend/admin && pnpm run build
```

Health check endpoint: `GET /health`

## Notes on Data Access

SQLite runs with `MaxOpenConns=1`. A store opens a transaction through
`WithinTransaction(func(tx contract.Transaction) error)`, and every query inside the closure
must go through that `tx` handle or a store bound to it via `WithTx(tx)`. Reaching for the
global DB handle instead asks for a second connection that will never be granted, deadlocking
the process — including indirectly, by calling a service that queries on its own. Read any
settings you need *before* opening the transaction, and keep outbound HTTP calls (payment
gateways and the like) outside it.

## Online Documentation

- https://dujiao-next.com
