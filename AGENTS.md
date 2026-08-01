# AGENTS.md — Dujiao-Next 自动测活版项目说明

本文件供 AI 助手（opencode 等）在新会话中快速了解本项目。请先阅读本文件再动手修改。

## 分销/代理商（渠道价批发采购，不开子站）

> 本项目除了官方自带的"分销子站 + 加价"模式外，还实现了**第二种代理商形态：渠道价批发采购，不开放子站点**。
> 运营方在管理端给商品/SKU 设"渠道价"（批发进价，可低于零售价、不得低于成本价），代理商在**主站**的
> 代理中心（分销中心 → 批发采购）按渠道价批量下单付款，平台发货到其名下。代理不拥有独立子站。

关键开关：`config.yml` → `reseller` 段
```yaml
reseller:
  enabled: true              # 开总分销功能
  sub_sites_enabled: false   # 关闭子站：域名解析永远按主站，代理仅走主站批发采购
  self_apply_enabled: true
  subdomain_base: ""         # 不开子站则无需配置泛解析
```

### 本分支的核心改动（对比官方 dujiao-next）

| 模块 | 文件 | 说明 |
|---|---|---|
| 配置 | `internal/config/config.go` | `ResellerConfig` 新增 `SubSitesEnabled`（mapstructure `sub_sites_enabled`） |
| 租户 | `internal/modules/reseller/contract/tenant.go` | `TenantContext` 新增 `WholesalePurchase` 标记、`IsWholesalePurchase()`、`HasResellerPricing()`；新增构造器 `ResellerPurchaseContext` |
| 域名解析 | `internal/modules/reseller/application/domain_resolver.go` | `!SubSitesEnabled` 时一律返回主站租户（不查 reseller_domains、不 404） |
| 子站操作拦截 | `internal/modules/reseller/application/management.go` | 关闭子站后 `AssignSystemSubdomain` / `SubmitUserCustomDomain` 返回 `ErrSubdomainBaseMissing` |
| 商品定价 | `internal/modules/reseller/domain/product_setting.go` + `constants.go` | 新增 `PricingModeChannelPrice = "channel_price"` + `ChannelPriceAmount` 字段 |
| 计价 | `internal/modules/reseller/application/pricing_rules.go` | `ApplyPricingRule` 支持 channel_price；新增 `ValidateChannelUnitAmount`（允许低于零售、不得低于成本）、`ValidateResolvedUnitAmount`（按模式分派校验） |
| 订单计价 | `internal/modules/order/application/reseller_pricing.go` | 门闩改用 `HasResellerPricing()`；批发单 `Base=成交额、Profit=0、ProfitEligible=false`（不记佣金） |
| 批发接口 | `internal/modules/reseller/contract/purchase.go` + `application/product_setting.go` 内 `PurchaseService` | `GET /reseller/catalog`、`POST /reseller/purchases/preview`、`POST /reseller/purchases` |
| 订单网关 | `internal/app/container/reseller_order_gateway.go` | 把分销批发端口适配到 `OrderService.CreateOrder/PreviewOrder`（构造 `ResellerPurchaseContext` 租户） |
| 公开配置 | `internal/modules/settings/transport/http/public/handler.go` | 主站 `/public/config` 的 `tenant` 下发 `reseller_enabled / reseller_sub_sites_enabled / reseller_self_apply_enabled` |
| 用户端 | `frontend/user/src/views/reseller/ResellerWholesale.vue` | 批发采购页（商品+渠道价+数量+测活勾选+预览+下单跳 `/pay`）；路由 `/reseller/wholesale` |
| 用户端显隐 | `frontend/user/src/views/reseller/ResellerConsoleLayout.vue`、`router/index.ts`、`stores/app.ts` | 关闭子站时隐藏"域名/店铺设置/商品定价"导航并守卫重定向；`appStore.resellerSubSitesEnabled` |
| 管理端 | `frontend/admin/src/views/admin/ResellerProductSettings.vue`、`layouts/AdminLayout.vue` | 定价模式新增"渠道价（批发进价）"+ 输入框；菜单按 `tenant.reseller_sub_sites_enabled` 隐藏域名/站点配置 |

### 架构约束（必须遵守，改代码会触发失败）

- **文件预算**：`internal/architecture/reseller_vertical_slice_test.go` 限制每个包的文件数
  （reseller `application` ≤ 17、`transport/http/user` ≤ 8、`presenter` ≤ 4、`shared` ≤ 1，含 `_test.go`）。
  新增代码**只能并入既有文件**，不能新建 `.go` 文件。改完必跑 `go test ./internal/architecture/...`。
- **分层**：`application` 不得 import Gin/asynq/infrastructure/transport/bootstrap；只有 `gormstore` 能 import GORM；
  分销模块内部通过 `contract/` 通信；订单与分销的跨模块调用走端口接口 + bootstrap/container 接线。
- **分销订单语义**：批发单 `ResellerID` 有值但 `ProfitEligible=false`，`accounting_profit.go` 不会入账佣金；
  子站零售单的利润 = 分销成交价 - 零售底价（加价差）。改 `reseller_pricing.go` 时两者口径都要自洽。

### 已知注意点（本次开发踩过/留意的）

1. **Windows 低精度时钟会导致测试偶发失败**：reseller `integrationtest` 原来用
   `time.Now().UnixNano()` 拼内存库 DSN，同一毫秒内两个测试会碰撞到同一 `cache=shared` 内存库。
   已改为 `module_test.go` 的 `uniqueInMemoryDSN()`（进程内原子计数）+ `seedAdminResellerManagementProfile`
   用原子计数生成用户邮箱。**新增集成测试请复用这两个助手，不要再用 UnixNano。**
2. **PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会破坏中文编码**（读用系统 ANSI 码页 → 写回 UTF8 变乱码）。
   改含中文的文件必须用编辑工具，不要用 `Set-Content` 重写整文件。
3. **presenter 必须回显 `channel_price_amount`**：`presenter/reseller.go` 的 `ResellerProductSettingResp` 和
   `AdminResellerProductSettingResp` 都要带该字段，否则管理端打开已设置的渠道价规则会显示 0.00 并在保存时清零。
4. **`deploy_ubuntu.sh` 检测到已存在 `config.yml` 会跳过生成**：已有服务器升级需手动补 `reseller:` 段，否则分销功能不生效。
5. **前端 i18n**：用户端 `frontend/user/src/i18n/locales/*.json` 与 `frontend/user/src/utils/resellerProductSettings.ts`、
   管理端 `frontend/admin/src/i18n/index.ts` 各自维护定价模式文案，新增模式要三处同步（zh-CN / zh-TW / en-US）。
6. **环境相关**：`internal/app/httpserver/middleware` 的合规测试用 CGO 版 `gorm.io/driver/sqlite`，本机无 CGO 会报错，
   属环境预存问题，与分销改动无关；后端统一用纯 Go 驱动 `glebarez/sqlite`。

## 项目概览

本项目是开源数字商品销售系统 **Dujiao-Next** 的定制分支，核心新增功能是 **CheckDx 卡片测活** 与 **挑卡**：

- 商家可为商品开启"支持测活"并设置"测活价格"
- 用户在商品页可**自选是否测活**（勾选"开启测活"），两档价格展示（不测活价 / 测活价），界面**禁止出现"加价"字样**
- 勾选测活的订单，付款后发货前调用 CheckDx API 批量检测；**只交付活卡**，死卡/未知卡/无法解析的卡标记为 `invalid` 状态
- 未勾选测活的订单**直接发货不检测**
- **挑卡**：商家上传 BIN 库自动标注卡密的国家/品牌/种类；开启挑卡的商品，用户**必须选国家**、可选按品牌（Visa/Mastercard/Discover/其他）与种类（D/PD/C）挑卡；挑选属性可设商品级加价（同组多选只按最高单价计一次）；按所选组合从库存过滤出卡，组合不足无法下单

## 仓库与部署

- 本地源码：`C:\Users\Administrator\Documents\Default Project\dujiao-next`
- GitHub 远端（公开）：`https://github.com/ashleyrios539/dujiaodxcheck_xhenmo01.git`（分支 `main`）
- 本地 git remote：`origin` 指向官方 `dujiao-next/dujiao-next`，`github` 指向上述远端
- 远程一键入口：`install.sh`（拉取并执行 `deploy_ubuntu.sh`），公开 URL：
  `https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/install.sh`
- 部署/更新命令：
  ```bash
  # 全新安装 或 更新（脚本自动识别模式）
  sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/install.sh)"
  ```
- **脚本运行模式（重要）**：
  - `deploy_ubuntu.sh` 启动时检测 `$INSTALL_DIR/config.yml`（默认 `/opt/dujiao/config.yml`）是否存在：
    - 不存在 → 全新部署：交互向导（站点域名/后台路径/管理员账号/密码），自动生成强随机密钥与 config.yml
    - 存在 → **更新模式**：先展示"拉取最新→重新构建→重启，数据全部保留"，`read -p "确认更新？[Y/n]"` 确认后才修改；
      按 `n` 取消则不做任何改动。更新模式**不**重新生成管理员密码、不覆盖 config.yml、不删数据库
  - 交互仅在有 TTY 时启用；`-y`/`--yes` 跳过交互用默认值/环境变量（非交互）
  - 数据保留范围：`db/dujiao.db`（商品/订单/卡密/站点设置含 `card_check_config`）、`config.yml`、`uploads/`、管理员账号密码
- 服务器日常更新：改完本地代码 `git push github main`，服务器上跑上面的一键命令即可（更新模式确认后生效）
- 详细部署见 `DEPLOY_UBUNTU.md`

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
| 测活客户端 | `internal/upstream/cardcheck/` | CheckDx /v1 HTTP 客户端（balance/submit/result轮询/cancel退点）+ 卡密解析 `ParseCard` |
| 交付测活编排 | `internal/modules/fulfillment/application/check.go` | `runCardCheck`（选卡→测活→死卡标invalid）+ `CardChecker` 端口 |
| 交付主流程 | `internal/modules/fulfillment/application/service.go` | `CreateAuto` 中按 `orderItem.CardCheckEnabled` 判定是否测活 |
| 价格计算 | `internal/modules/order/application/order_service_validate.go` | `buildOrderResult`：测活价并入 basePrice 后参与优惠计算 |
| 订单项字段 | `internal/modules/order/domain/order_item.go` | `CardCheckEnabled`（用户选择快照，持久化） |

## 挑卡功能关键位置

| 模块 | 文件 | 作用 |
|---|---|---|
| 卡属性 | `internal/modules/cardsecret/domain/secret.go` + `bin.go` | `card_secrets` 新增 `country`/`brand`/`card_type`；`card_bins` 表 + 品牌归一化 `NormalizePickBrand` / 种类映射 `NormalizeCardType` |
| BIN 库服务 | `internal/modules/cardsecret/application/bins.go` | CSV 导入（列映射 + 种类规则）、统计、列表；卡密导入自动标注 `annotateCardSecrets`（取卡号前6位查 BIN） |
| BIN 库路由 | `internal/modules/cardsecret/transport/http/bin_admin_handler.go` | 管理后台 `/admin/card-bins/*` |
| 挑卡选卡过滤 | `internal/modules/cardsecret/contract` + `gormstore/store.go` | `PickFilter`（country/brands/cardTypes）+ `ListAvailableByProductFiltered`/`CountPickAttrs` |
| 商品挑卡配置 | `internal/modules/catalog/product/domain/product.go` | `PickEnabled` + `PickPrices`（品牌/种类加价表）+ `PickUnitSurcharge`（同组多选只按最高单价计一次） |
| 计价与快照 | `internal/modules/order/application/order_service_validate.go` | `buildOrderResult`：国家必选校验 + 挑卡加价并入 basePrice；订单项 `PickCountry`/`PickBrands`/`PickCardTypes` 快照 |
| 预留过滤 | `internal/modules/order/application/order_service.go` | 预留卡密按订单项挑卡快照过滤，组合不足 `ErrCardSecretInsufficient` |
| 交付过滤 | `internal/modules/fulfillment/application/check.go` + `service.go` | 测活 pool 与未测活直取均按挑卡快照过滤 |
| 挑卡库存接口 | `internal/modules/catalog/product/transport/http/public_handler.go` | `GET /public/products/:slug/pick-stock`（SKU/国家/品牌/种类聚合 + 国家字典） |
| 国家字典 | `internal/shared/countries/` | ISO 3166-1 alpha-2 → 中文名静态表 |
| 前端 | `frontend/user/src/composables/useProductDetail.ts`、`views/ProductDetail.vue`、`templates/vault/ProductDetail.vue`、`stores/cart.ts`、`composables/useCheckout.ts` | 国家必选 + 品牌/种类挑卡 + 实时可发数 + 加价展示 + 快照携带 |
| 管理端 | `frontend/admin/src/views/admin/CardBins.vue`、`CardSecrets.vue`、`components/ProductEditModal.vue` | BIN 库上传/列映射/种类规则、卡密属性列与过滤、商品挑卡加价表 |

### 挑卡关键约定
- 卡密属性来源：**BIN 库**（上传 CSV → `card_bins` 表），导入卡密时取卡号前 6 位自动标注国家/品牌/种类；未命中属性留空，仅作普通卡售卖。
- 品牌归一化：`VISA→visa`、`MASTERCARD/MC→mastercard`、`DISCOVER→discover`、其余（含空）→`other`。
- 种类三值：`D`（含预付）、`PD`（纯D不含预付）、`C`（纯C），默认映射 `PREPAID/GIFT→D`、`DEBIT→PD`、`CREDIT/CHARGE→C`（上传 UI 可改）。
- 挑卡加价：商品级属性单价表（visa/mastercard/discover/other/D/PD/C），同一属性组（品牌/种类）多选时只按该组**最高单价**计一次，并入商品单价参与优惠计算。
- 下单校验：商品开启挑卡时**国家必选**、格式两位大写；品牌/种类值合法；预留库存不足直接 `ErrCardSecretInsufficient` 无法下单。
| 商品字段 | `internal/modules/catalog/product/domain/product.go` | `CardCheckEnabled`（支持测活）、`CardCheckFee`（测活价格） |
| 测活全局设置 | `internal/modules/settings/schema/integration/cardcheck.go` | `card_check_config`：enabled/kami/interface/buffer（缓冲**比例** %）/timeout/poll；管理后台「设置 → 测活设置」页（`frontend/admin/src/views/admin/components/SettingsCardCheckTab.vue`）编辑 |
| 测活交付编排 | `internal/modules/fulfillment/application/check.go` | `runCardCheck`/`checkItemCard`：按轮迭代测活，每轮「需求数量 + 需求数量×缓冲比例」取卡，活卡达到购买数量即停，不足按剩余数量按比例继续补检；`cardCheckPool` 按 id 去重取卡 |
| 商城测活交互 | `frontend/user/src/composables/useProductDetail.ts`、`useCheckout.ts`、`stores/cart.ts` | 勾选开关、两档价格、购物车携带、结算标识 |
| 管理端测活配置 | `frontend/admin/src/views/admin/components/ProductEditModal.vue`、`Products.vue` | 开关+测活价格输入、列表快捷切换 |

### 卡密状态
`internal/modules/cardsecret/domain/secret.go`：`available` / `reserved` / `used` / `invalid`（invalid = 测活死卡，不再上架）。

### CheckDx 计费要点
按条扣"点数"；任务结束必须调 `POST /v1/cancel` 结束并退回未检测卡的点数（`CheckCards` 已处理）。接口 `interface` 用短名 `post1`~`post6`（如 `post5`），可在 CheckDx 网页端"设置/接口"查看维护状态。

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
