# Shop

A digital goods e-commerce platform. This repository contains the complete
application: the Go backend, the customer storefront, and the admin panel.

## Tech Stack

| Layer | Stack |
| --- | --- |
| Backend | Go 1.26 · Gin · GORM · SQLite / PostgreSQL |
| Auth | JWT (separate admin / user realms) · Casbin RBAC · TOTP 2FA |
| Async | asynq on Redis (optional — the server runs without it) |
| Config | Viper (`config.yml`) |
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
├── Dockerfile                # single full-stack image
├── docker-compose.yml        # 一键自部署：app + redis
├── .env.example              # 环境变量模板（.env 由 setup.sh 生成，不入库）
├── setup.sh                  # 初始化：生成随机密钥与 .env
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

## 一键自部署（Docker Compose，推荐）

不需要装 Go / Node / Nginx，只要 Docker 就能跑（Docker 20.10+，Docker Compose v2）。

```bash
git clone https://github.com/ashleyrios539/dujiao-next-selfhost.git
cd dujiao-next-selfhost
./setup.sh              # 生成 .env：随机密钥、随机后台路径、强管理员密码
docker compose up -d    # 构建镜像并启动（首次构建含前端编译，约 5~15 分钟）
```

启动后：

- 商城首页：`http://<服务器IP>:8080`
- 后台：`http://<服务器IP>:8080<后台路径>`（`./setup.sh` 会打印后台路径，即 `.env` 里的 `WEB_ADMIN_PATH`）
- 用 `./setup.sh` 打印的管理员账号/密码登录后台，登录后请立即修改密码并开启 2FA。

常用操作：

```bash
docker compose ps                                # 查看状态
docker compose exec app tail -f /app/logs/app.log    # 查看运行日志（release 模式日志写文件，不走容器 stdout）
docker compose down                              # 停止（数据保留在 ./data/）
docker compose exec app ./dujiao-next admin list-admins     # 查看管理员列表
docker compose exec app ./dujiao-next admin reset-password --username <管理员用户名>   # 忘记密码时重置
git pull && docker compose build && docker compose up -d    # 升级（数据与配置保留）
```

说明：

- 数据全部保存在宿主机 `./data/`（SQLite 数据库、上传文件、日志、Redis 数据），备份整体拷贝该目录即可；`docker compose down` 不带 `-v` 不会删除数据。
- release 模式下应用日志写入 `./data/logs/app.log`，不打印到容器 stdout；查看运行日志用上面的 `docker compose exec app tail -f /app/logs/app.log`。
- 配置全部走 `.env` 环境变量，优先级高于 `config.yml`；需要完整自定义时，也可挂载自己的 `config.yml` 到 `/app/config.yml`。
- 默认附带内置 Redis 容器（数据在 `./data/redis`）。如需外置 Redis，改 `docker-compose.yml` 中 `app` 服务的 `REDIS_HOST` / `QUEUE_HOST`。`REDIS_ENABLED=false` 只关闭 Redis 缓存/限流等功能；**异步队列是否启用由 `QUEUE_ENABLED` 单独控制**（默认开启，依赖 Redis）。
- Telegram Bot 的 webhook 地址与密钥可在后台「Telegram Bot → 基础设置」里直接配置（保存即生效，无需改 `.env`）；需公网 HTTPS，未配置时回退到 `.env` 的 `TELEGRAM_WEBHOOK_*`。
- 容器内以 root 运行（与既有 Dockerfile 一致）。如对隔离性有更高要求，可给 `app` 服务加 `user: "10001:10001"`，并先执行 `chown -R 10001:10001 ./data`。
- 想省去「构建镜像」的时间，可改用预编译二进制部署，见下文「Quick Start (Deploy)」。

## Quick Start (Deploy)

> 不想自己编译、想用最省事的 Docker 方式，见上方「一键自部署（Docker Compose，推荐）」。以下为预编译二进制部署。

Download the latest `dujiao-next_*.tar.gz` release:

```bash
tar -xzf dujiao-next_*.tar.gz
cp config.yml.example config.yml
# edit config.yml: set jwt.secret, user_jwt.secret, and web.admin_path
./dujiao-next
```

## Quick Start (Develop)

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
