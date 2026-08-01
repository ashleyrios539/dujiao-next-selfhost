# AGENTS.md — Dujiao-Next 自动测活版项目说明

本文件供 AI 助手（opencode 等）在新会话中快速了解本项目。请先阅读本文件再动手修改。

## 项目概览

本项目是开源数字商品销售系统 **Dujiao-Next** 的定制分支，核心新增功能是 **CheckDx 卡片测活**：

- 商家可为商品开启"支持测活"并设置"测活价格"
- 用户在商品页可**自选是否测活**（勾选"开启测活"），两档价格展示（不测活价 / 测活价），界面**禁止出现"加价"字样**
- 勾选测活的订单，付款后发货前调用 CheckDx API 批量检测；**只交付活卡**，死卡/未知卡/无法解析的卡标记为 `invalid` 状态
- 未勾选测活的订单**直接发货不检测**

## 仓库与部署

- 本地源码：`C:\Users\Administrator\Documents\Default Project\dujiao-next`
- GitHub 远端（公开）：`https://github.com/ashleyrios539/dujiaodxcheck_xhenmo01.git`（分支 `main`）
- 本地 git remote：`origin` 指向官方 `dujiao-next/dujiao-next`，`github` 指向上述私有远端
- 部署：Ubuntu 一键脚本 `deploy_ubuntu.sh`（自包含，全新服务器可 `curl | sudo bash` 直跑），详见 `DEPLOY_UBUNTU.md`
- 服务器日常更新：改完本地代码 `git push github main`，服务器上重新 `sudo bash deploy_ubuntu.sh`

## 技术栈与架构

- 后端：Go 1.26 + Gin + GORM（SQLite 纯 Go 驱动 `glebarez/sqlite`，无 CGO）
- 前端：Vue 3 + Vite + TypeScript + Tailwind（`frontend/admin` 管理端、`frontend/user` 商城端）
- 模块化单体：`internal/modules/<name>/` 垂直切片（domain / application / infrastructure / transport / contract）
- 分层规则由 `internal/architecture/*_test.go` 强制，改代码必须遵守：
  - `application` 不得 import Gin/asynq、infrastructure、transport、bootstrap
  - 只有 `infrastructure/gormstore` 可 import GORM
  - `internal/shared` / `internal/platform` 不得依赖业务模块
- 订单交付：支付成功 → asynq 任务 `order:auto_fulfill` → `FulfillmentService.CreateAuto`（`internal/modules/fulfillment/application/service.go`）
- 测试后必须跑架构测试：`go test ./internal/architecture/...`

## 测活功能关键位置

| 模块 | 文件 | 作用 |
|---|---|---|
| 测活客户端 | `internal/upstream/cardcheck/` | CheckDx HTTP 客户端（verify/get_post/get_card/history轮询/xiaofei退点）+ 卡密解析 `ParseCard` |
| 交付测活编排 | `internal/modules/fulfillment/application/check.go` | `runCardCheck`（选卡→测活→死卡标invalid）+ `CardChecker` 端口 |
| 交付主流程 | `internal/modules/fulfillment/application/service.go` | `CreateAuto` 中按 `orderItem.CardCheckEnabled` 判定是否测活 |
| 价格计算 | `internal/modules/order/application/order_service_validate.go` | `buildOrderResult`：测活价并入 basePrice 后参与优惠计算 |
| 订单项字段 | `internal/modules/order/domain/order_item.go` | `CardCheckEnabled`（用户选择快照，持久化） |
| 商品字段 | `internal/modules/catalog/product/domain/product.go` | `CardCheckEnabled`（支持测活）、`CardCheckFee`（测活价格） |
| 测活全局设置 | `internal/modules/settings/schema/integration/cardcheck.go` | `card_check_config`：enabled/kami/interface/country/buffer/timeout/poll |
| 商城测活交互 | `frontend/user/src/composables/useProductDetail.ts`、`useCheckout.ts`、`stores/cart.ts` | 勾选开关、两档价格、购物车携带、结算标识 |
| 管理端测活配置 | `frontend/admin/src/views/admin/components/ProductEditModal.vue`、`Products.vue` | 开关+测活价格输入、列表快捷切换 |

### 卡密状态
`internal/modules/cardsecret/domain/secret.go`：`available` / `reserved` / `used` / `invalid`（invalid = 测活死卡，不再上架）。

### CheckDx 计费要点
按条扣"点数"；任务结束必须调 `xiaofei` 结束并退回未检测卡的点数（`CheckCards` 已处理）。接口 `interface` 值需在 CheckDx 网页端获取（含维护状态）。

## 常用命令

```bash
# 后端
go build ./...                            # 编译
go test ./internal/architecture/...       # 架构约束测试（必跑）
go test ./internal/modules/order/...      # 订单模块测试
go vet ./...

# 前端（需 Node 24 + pnpm 10）
cd frontend/admin && pnpm run build       # 管理端（含 vue-tsc 类型检查）
cd frontend/user  && pnpm run build       # 商城端（含 vue-tsc 类型检查）

# fullstack 集成构建（仅 Linux bash；Windows 上 build:fullstack 不可用）
cd frontend/admin && pnpm run build:fullstack
cd frontend/user  && pnpm run build
rm -rf internal/web/dist && mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
CGO_ENABLED=0 go build -trimpath -tags release,fullstack -o dujiao-next ./cmd/server
```

## 注意事项

- **隐私/安全**：仓库公开。不要把 `config.yml`、`.env`、密钥、CheckDx 卡密提交进仓库（`config.yml` 已在 `.gitignore`）
- 数据库迁移用 GORM `AutoMigrate`（`internal/bootstrap/database/migrations/registry.go`），新增实体字段需在此注册
- 交付测活发生在事务外，死卡标记用独立 `BatchUpdateStatus`，避免长事务持锁
- 改动涉及订单/交付/商品三处时，务必跑 `go test ./internal/architecture/...` 和对应模块测试
