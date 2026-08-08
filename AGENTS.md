# AGENTS.md — 项目说明

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

### 本分支的核心改动

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

### 网站 / Telegram Bot 渠道可见性（本分支后续新增）

| 模块 | 文件 | 说明 |
|---|---|---|
| 商品字段 | `internal/modules/catalog/product/domain/product.go` | `products` 新增 `bot_visible` / `web_visible` 两个**双向独立**开关（默认 true，GORM AutoMigrate 自动加列） |
| 列表过滤 | `internal/modules/catalog/product/contract/repository.go` + `store/gormstore/product_store.go` | `ListFilter` 新增 `WebVisible` / `BotVisible`（*bool），store 层分页前过滤 |
| 网站公开查询 | `internal/modules/catalog/product/application/query.go` | `ListPublic`/`ListPublicExact`/`GetPublicBySlug` 强制 `web_visible=true`（仅 bot 商品在网站直达 slug 返回 404） |
| bot 查询 | 同上 | 新增 `ListPublicForBot` / `GetPublicBySlugForBot`：按 `bot_visible=true` 过滤，可返回 `web_visible=false` 的仅 bot 商品 |
| bot 端口 | `internal/app/container/telegram_purchase_ports.go` | `ListProducts`/`GetProductBySlug` 改用 bot 专属查询 |
| 管理端 | `internal/modules/catalog/product/transport/http/admin_handler.go` + `frontend/admin/src/views/admin/components/ProductEditModal.vue` | 创建/编辑/快捷更新接受 `bot_visible`/`web_visible`；商品编辑弹窗新增"展示渠道"区块（两个开关，三语 i18n） |

约定：`web_visible=false` 只在**网站前台**隐藏（列表+详情），管理端、订单、bot 均不受影响；`bot_visible=false` 只在 bot `/shop` 隐藏。价格/测活/挑卡配置两端共用同一份，无渠道专属定价。

### Telegram Bot 直连 epusdt 支付 + txt 发货（本分支后续新增）

| 模块 | 文件 | 说明 |
|---|---|---|
| epusdt 网关 | `internal/modules/payment/infrastructure/gateway/epusdt/epusdt.go` | `CreateResult` 新增 `ReceiveAddress/ActualAmount/Token/Network/ExpirationTime`，从 GMPay `create-transaction` 响应 `data` 提取（`receive_address`/`actual_amount`/`token`/`network`/`expiration_time`） |
| epusdt 适配 | `internal/modules/payment/infrastructure/gateway/adapters/epusdt/adapter.go` | 把上述字段写入 `GatewayCreateResult.Payload` **顶层**（`receive_address` 等），`token`/`network` 缺失时回退渠道配置 |
| bot 支付端口 | `internal/app/container/telegram_purchase_ports.go` | `fillEpusdtPaymentInfo`/`fillEpusdtRechargeInfo` 从 `Payment.ProviderPayload` 读取 epusdt 付款字段透传给 bot；`payloadString`/`payloadInt64` 为通用读取助手 |
| bot 在线支付 | `internal/modules/telegram/webhook/application/purchase_service.go` | `payOnline` 只走 epusdt 渠道（`filterEpusdtChannels` 匹配 `epusdt/usdt/usdt-trc20/trx/tron/trc20`），创建订单后在聊天内直接展示应付 USDT、TRC20 收款地址、网络、过期时间；新增 `shop:paycheck:` 刷新支付状态回调 |
| bot 充值 | 同上 | `/recharge` 同样只走 epusdt，聊天内展示充值 USDT 金额与收款地址 |
| bot 商品简介 | `internal/app/container/telegram_purchase_ports.go` + `purchase_service.go` | `ShopProduct.Description` 由 `DescriptionJSON` 填充，`renderDetail` 展示（与网站同源自动同步） |
| txt 发货 | `internal/modules/telegram/notify/infrastructure/botapi/client.go` + `internal/app/container/native_bot_notifier.go` | 新增 `SendDocumentBytes`（sendDocument multipart 内存文件）；发货后把卡密 payload 以 `卡密_<订单号>.txt` 文件推送到用户私聊 |
| 二维码+复制 | `purchase_service.go` + `botapi/client.go` + `webhook/contract/ports.go` + `webhook/infrastructure/botapi_adapter.go` | epusdt 付款/充值页把地址渲染为 Markdown 代码块（点按即复制），并附带**进程内生成**的收款地址二维码图片（`buildQRCodePNG` 用 `boombuler/barcode`，新增直接依赖，不依赖外部二维码服务）；`BotAPIClient` 新增 `SendPhotoBytes`（sendPhoto multipart，支持 caption + reply_markup） |

要点：epusdt 付款关键字段放在 `ProviderPayload` **顶层**（非 `data` 子对象），bot 端读取不依赖具体网关响应结构；GMPay 创建响应可能不含 `network`，adapter 已回退渠道配置 `network`（tron）。
付款/充值页**只显示应付 USDT 一行**（不显示 store 币种行，避免币种混淆）；充值输入提示明确为「请输入充值 USDT 金额」。**充值付款页展示后立即清空会话**：用户再输入数字不会重复创建充值订单，任意文本输入自动回退主菜单（/start 页面）；付款/充值页均带「🏠 返回主页」按钮（callback `menu`，非 `shop:` 前缀，由主 Service 处理）。

### 付款/充值单条二维码图片消息 + 挑卡多语言化 + 语言切换立即生效（本分支后续新增）

| 模块 | 文件 | 说明 |
|---|---|---|
| 单条图片消息 | `purchase_service.go` + `botapi/client.go` | epusdt 付款/充值改为**一条 sendPhoto**：二维码为主图、说明文字（金额/地址/复制提示/网络/过期/操作提示）放进 **caption**、按钮挂到图片消息上；`sendEpusdtQR` 生成二维码失败或图片发送失败时**退化为纯文本消息**（保证付款信息仍可见，不向上抛错避免 webhook 重试造成重复下单） |
| 挑卡选项多语言化 | `purchase_service.go` | 品牌/卡类型选项名随用户语言本地化（与网页端一致：品牌 `random` 走 i18n、Visa/Mastercard/Discover/AMEX/JCB 固定不翻译；卡类型 `D/PD/C` 走 i18n）。新增 `pickBrandName`/`pickCardTypeName`，作用于 `brandKeyboard`/`cardTypeKeyboard`/`renderDetail` 摘要/`confirmOrder` 确认单摘要；`GetPickStock` 里的中文名仅作未知 key 回退 |
| 语言切换立即生效 | `service.go` + `purchase_service.go` | `startKeyboard` 标签多语言化（`purchase.menu_shop/binstock/wallet/orders/help`）；`toggleLanguage` 切换后**立即以新语言重渲当前购买步骤**（`renderCurrentStep`，顺带刷新最新数据），无会话或重渲失败则发送**新语言主菜单**（`sendMainMenu` 回调由 `WithPurchase` 注入，含 hint + 快捷键盘）；`/menu` 的 `switch_language` 菜单项也接入切换（原来只回复提示文本） |

### 帮助中心后台在线编辑 → bot 端完整呈现（本分支后续新增）

背景：后台 `TelegramBotHelpCenter.vue` 页面、`telegram_bot.go` schema 的 `Help` 段（`Title/Intro/CenterHint/SupportHint/Items`，每项 `Key/Enabled/Order/Summary/Title/Content/ShowSupportLink`）早已可在线编辑且正确 round-trip，bot 每次请求无缓存读最新配置；但原 `sendHelpCenter` 只渲染 `Title/Intro/Summary 列表/CenterHint`，**静默丢弃**了 `item.Title/Content/ShowSupportLink`、`Help.SupportHint` 且不按 `Order` 排序 —— 后台改这些字段在 bot 端看不到效果。

| 模块 | 文件 | 说明 |
|---|---|---|
| 帮助中心列表 | `webhook/application/help.go`（新建） | `sendHelpCenter` 从 `service.go` 迁入并增强：按 `item.Order` 升序稳定排序、跳过 `!Enabled` 或空 `Summary`/`Key` 项，把每项 `Summary` 做成 **inline 按钮**（`callback_data=help:detail:<key>`，两列），正文追加 `CenterHint` 与 `SupportHint`；`!Help.Enabled` 仍回退 `mainMenuHint` |
| 帮助条目详情 | `webhook/application/help.go` | 新增 `sendHelpDetail`：渲染该项 `Title`+`Content`；`ShowSupportLink=true` 且 `Basic.SupportURL` 非空 → 附「💬 联系客服」**URL 按钮**；`ShowSupportLink=true` 但 `SupportURL` 空 → 正文后补 `SupportHint` 兜底；恒附「↩️ 返回帮助中心」按钮（`callback_data=help`） |
| 回调路由 | `webhook/application/service.go` | `handleCallbackQuery` 路由新增 `help:detail:` 前缀分支 → `sendHelpDetail`；alert 文案对 `help`/`menu`/`switch_language`/`help:detail:*` 一律静默 |
| 文案 | `purchase_service.go` | `purchaseTexts` 新增 `help.back`/`help.contact_support`/`help.item_disabled`（zh-CN/zh-TW/en-US） |
| 测试 | `webhook/application/service_test.go`（新建） | 7 个用例：列表渲染+Order 排序、Help 关闭回退、跳过禁用项、详情回调（含返回按钮）、ShowSupportLink+URL、ShowSupportLink 无 URL 兜底 SupportHint、未知/停用 key 提示 |

要点：`callback_data` 上限 64 字节，`help:detail:<key>` 远低于此；`key` 由 schema `TrimSpace`。后台编辑后 bot **无需重启**即生效（`HandleUpdate` → `GetTelegramBotConfig` → 无缓存 GORM SELECT）。未改 schema/store/admin 前端（审计确认它们已正确）；未改 `NormalizeHelpItems` 的 12 条上限（潜在问题，当前默认 4 条）。

### 购买会话文本输入步骤的「退出命令」拦截（本分支后续新增，bug 修复）

背景：bot 在「卡头库存输入 BIN」「充值输入金额」「商品配置输入 BIN/国家码」等**文本输入**步骤里，会话常驻 `purchaseService`。原 `handleMessage` 只识别 `/shop`、`/recharge` 两个命令能重新进入，其它文本（含 `/start`、`/menu`、`/help`、`/cancel`）一律被当成本步骤的输入处理（如 `/start` 进 BIN 库存步骤会被 `isBinInput` 判否后反复回提示），**用户被困住、找不到退出路径**。同理充值金额步骤 `/start` 被当金额解析失败回提示；配置步骤 `/start` 走 `sendHelp` 仍不退出。

| 模块 | 文件 | 说明 |
|---|---|---|
| 退出命令拦截 | `webhook/application/purchase_service.go` `handleMessage` | 在任何会话文本处理之前拦截：`/cancel` → 直接 `cancel()`（清会话+提示）；`/start`、`/menu`、`/help` → 清空会话后返回 `handled=false` 交还主 Service 处理（`/start`/`/menu`/`/help` 由主 Service 正常响应）。这样所有「文本输入」步骤都能用命令退出 |
| 入口提示加取消按钮 | `purchase_service.go` `enterBinStock`/`enterRecharge` | 卡头库存、充值金额的入口提示原本只有纯文本、无键盘；现附带「❌ 取消」inline 按钮（`callback_data=shop:cancel`），提供可见退出路径 |
| 测试 | `webhook/application/purchase_service_test.go` | 9 个用例：BIN库存/充值/配置三处 `/start` 退出并清会话、BIN库存 `/cancel` 取消、BIN库存 `/menu`+`/help` 退出、无效非命令文本仍按 BIN 回提示（不误退出）、两处入口提示带取消按钮 |

要点：纯按钮步骤（浏览分类/商品、挑卡模式/品牌/卡类型、选渠道等）本就返回 `handled=false` 让主 Service 处理命令，不受影响；本次只补齐**文本输入**步骤的出口。epusdt 付款/充值二维码展示后会话已清空（`renderPaymentResult`/`createRechargeWithChannel` 内 `delete(s.sessions)`），不存在展示后被困问题。

### Bot 购买流程重构为购买类型按钮 + 首位数字挑卡 + reply 键盘测活 + txt 发货修复（本分支后续新增）

背景：原商品页是「配置面板」（基础价 + 当前选择明细 + 挑卡/测活 inline 开关 + 立即购买）。重构为「商品初始页文本两档价 + 8 个购买类型按钮（随机/挑头/3头/4头/5头/6头/CREDIT/DEBIT）」；测活改由 reply 键盘选择；首位数字（头=卡号首位）为端到端新增维度。同时修复 txt 发货链路（父/子订单 ID 不匹配导致静默不发卡密）。

| 模块 | 文件 | 说明 |
|---|---|---|
| txt 发货修复 | `app/container/native_bot_notifier.go` + `fulfillment/application/service.go` + `procurement/application/callback.go` | 断链：上游传父订单 ID 但履约记录在子订单上，`GetByOrderID(parentID)` 查不到 → 静默 return。改传子订单 ID，notifier 据此查履约 payload 并解析父订单 OrderNo 作为 txt 文件名 |
| txt 发货测试 | `app/container/native_bot_notifier_test.go`（新建） | 4 用例：子订单 payload + 父订单号文件名、无履约静默跳过、无父订单用自身 OrderNo、无 token 跳过 |
| 首位库存聚合 | `cardsecret/contract/{types,ports}.go` + `cardsecret/infrastructure/gormstore/store.go` | 新增 `BinHeadCount`/`CountByBinHead`（`GROUP BY substr(bin_prefix,1,1)`）；`buildPickQuery` 按位数区分 6 位精确 `=` vs 1-5 位前缀 `LIKE prefix%` |
| 首位加价 | `catalog/product/domain/product.go` | 新增 `PickPriceKeyHead3/4/5/6` 加入 `pickPriceKeys`（`NormalizePickPrices` 自动保留，无需 GORM 迁移） |
| 首位下单 | `order/application/order_service_validate.go` | `validPickBin` 放宽 1-6 位；BIN 加价按位数取 key（6 位→`bin`，1 位→`headN`）；6 位 BIN 与国家互斥，1 位首位可与国家共存（交集过滤） |
| bot 端口 | `app/container/telegram_purchase_ports.go` + `telegram/webhook/contract/purchase.go` | 新增 `CountByBinHead`/`CountAvailableByBinHead` 端口 + `ShopBinHead` DTO |
| bot 购买流程 | `telegram/webhook/application/purchase_service.go` | 重写 `renderDetail`（文本两档价，不显示当前选择/基础价/测活开关）+ `detailKeyboard`（8 购买类型按钮，各带库存+毛料价）；新增 `purchaseKind`/`selectBuyType`/`enterCheckOrConfirm`/`renderCheckChoice`/`handleCheckChoice`；`renderPickCountry` 翻页（一页6）+ `countryKeyboard` emoji 国旗；`buildPurchaseItems`/`validateOrder` 按 `pickKind` 构造 |
| reply 键盘 | `telegram/webhook/application/helpers.go` | 新增 `replyKeyboard`/`replyKeyboardButton`/`replyKeyboardRemove` 类型（传输层 `SendMessage`/`SendPhotoBytes` 已 markup-agnostic，无需改 adapter/client） |
| emoji 国旗 | `internal/shared/countries/countries.go` | 新增 `EmojiFlag(code)`（regional indicator 拼接，纯计算） |
| 网页 DEBIT/CREDIT 合并 | `frontend/user/src/composables/useProductDetail.ts` + `i18n/locales/*.json` | `pickTypeOptions` 从 D/PD/C 三项合并为 DEBIT(提交D)/CREDIT(提交C) 两项；库存匹配已把 D 当 PD 超集 |
| 管理端首位加价 | `frontend/admin/src/views/admin/components/ProductEditModal.vue` + `frontend/admin/src/i18n/index.ts` | `pickPriceKeys` 加 `head3/4/5/6`；三语加 `head3/4/5/6` 加价标签 |

要点：DEBIT 提交 `PickCardTypes=["D"]`（库存匹配 D+PD）、CREDIT 提交 `["C"]`，加价分别取 `pick_prices["D"]`/`["C"]`——**管理端/后端无需为 DEBIT 改动**（复用 D 项）。首位挑卡走 BIN 路径：`PickBin=首位`(1位) + `PickCountry=国家`，后端按位数 LIKE 匹配、按首位取 `headN` 加价。`pickMode` 旧流程（type 模式选品牌/卡类型）保留兼容（`pickKind` 为空时回退）。测活选择用 reply 键盘（输入框弹出，点一下发文字），选择后发 `replyKeyboardRemove` 移除并进确认页。无需 GORM 迁移：`PickBin varchar(6)` 可存1位或6位，`PickPrices` 是 `jsonmap.JSON` 新 key 自动保留。

### 网页端首位挑卡 + 管理端删品牌加价（本分支后续新增）

背景：bot 端早已有「3/4/5/6头」首位挑卡，但网页端「挑卡种类」模式仍是品牌 chips（visa/mastercard/discover/amex/jcb）；管理端「挑卡加价」还保留已废弃的品牌加价框。本次让网页端与 bot 对齐首位维度，并清理管理端品牌加价。

| 模块 | 文件 | 说明 |
|---|---|---|
| 网页端首位挑卡 | `frontend/user/src/composables/useProductDetail.ts` + `views/ProductDetail.vue` + `templates/vault/ProductDetail.vue` + `i18n/locales/*.json` | 「挑卡种类」模式品牌 chips 替换为首位 chips(3/4/5/6头)；提交 `pickBin=首位`(1位) + `pickCountry` + `pickCardTypes`，`pickBrands` 恒空；加价取 `pick_prices["head<N>"]` + 种类 max；首位库存复用 `pick-stock?bin=<digit>`（全商品，与 bot 一致），不新增后端接口 |
| 管理端删品牌加价 | `frontend/admin/src/views/admin/components/ProductEditModal.vue` + `i18n/index.ts` | `pickPriceKeys` 去 visa/mastercard/discover/amex/jcb（13→8）；后端常量 `PickPriceKeyVisa` 等、`pickPriceKeys` 列表、`ValidPickBrand` 全部保留（兼容旧 bot type 流程与既有数据），仅前端隐藏输入 |
| 后端首位+种类叠加加价 | `internal/modules/order/application/order_service_validate.go` | 1 位首位 BIN 与种类同时提交时叠加加价（`PickUnitSurcharge(prices, [headKey], cardTypes)`）；6 位 BIN 仍不叠加（挑头语义） |

要点：**后端仅改 1 处加价叠加逻辑**，其余后端（validPickBin/buildPickQuery/PickPriceKeyHead/库存接口）早已就绪。前端无单测，靠 `pnpm run build`（vue-tsc）验证两模板解构与 composable 返回对象一致。新增测试：`TestBuildOrderResultPickHeadAndCardTypeSurcharge`（head4+D=13.50）、`TestBuildOrderResultPickHeadWithEmptyBrandsValid`（空品牌合法）。

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

本项目是开源数字商品销售系统的定制分支，核心新增功能是 **CheckDx 卡片测活** 与 **挑卡**：

- 商家可为商品开启"支持测活"并设置"测活价格"
- 用户在商品页可**自选是否测活**（勾选"开启测活"），两档价格展示（不测活价 / 测活价），界面**禁止出现"加价"字样**
- 勾选测活的订单，付款后发货前调用 CheckDx API 批量检测；**只交付活卡**，死卡/未知卡/无法解析的卡标记为 `invalid` 状态
- 未勾选测活的订单**直接发货不检测**
- **挑卡**：商家上传 BIN 库自动标注卡密的国家/品牌/种类；开启挑卡的商品，用户**必须选国家**、可选按品牌（Visa/Mastercard/Discover/其他）与种类（D/PD/C）挑卡；挑选属性可设商品级加价（同组多选只按最高单价计一次）；按所选组合从库存过滤出卡，组合不足无法下单

## 仓库与部署

- 本地源码：`C:\Users\Administrator\Documents\Default Project\dujiao-next`
- GitHub 远端（公开）：`https://github.com/ashleyrios539/dujiaodxcheck_xhenmo01.git`（分支 `main`）
- 本地 git remote：`origin` 指向上游仓库，`github` 指向上述远端
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
| 前端 | `frontend/user/src/composables/useProductDetail.ts`、`views/ProductDetail.vue`、`templates/vault/ProductDetail.vue`、`stores/cart.ts`、`composables/useCheckout.ts` | 国家必选 + 首位/种类挑卡（挑卡种类模式：3/4/5/6头 chips + DEBIT/CREDIT chips）+ 实时可发数 + 加价展示 + 快照携带 |
| 管理端 | `frontend/admin/src/views/admin/CardBins.vue`、`CardSecrets.vue`、`components/ProductEditModal.vue` | BIN 库上传/列映射/种类规则、卡密属性列与过滤、商品挑卡加价表（`pickPriceKeys = bin,head3-6,D,PD,C`） |

### 挑卡关键约定
- 卡密属性来源：**BIN 库**（上传 CSV → `card_bins` 表），导入卡密时取卡号前 6 位自动标注国家/品牌/种类；未命中属性留空，仅作普通卡售卖。
- 品牌归一化：`VISA→visa`、`MASTERCARD/MC→mastercard`、`DISCOVER→discover`、其余（含空）→`other`。
- 种类三值：`D`（含预付）、`PD`（纯D不含预付）、`C`（纯C）。判定逻辑：`Type` 列命中显式映射（默认 `CREDIT/CHARGE→C`）直接得 `C`；其余非信用卡按「预付标记列」（默认 `Category`）是否命中 `PREPAID` 区分——命中 → `D`（含预付），未命中 → `PD`（纯D）。
- 挑卡加价：商品级属性单价表（`bin`/`head3`/`head4`/`head5`/`head6`/`D`/`PD`/`C`），同一属性组（首位/种类）多选时只按该组**最高单价**计一次，并入商品单价参与优惠计算。品牌加价（visa/mastercard/discover/amex/jcb）后端常量保留兼容但管理端不再配置；网页端「挑卡种类」模式用首位 chips 取代品牌 chips。
- 首位挑卡（3/4/5/6头=卡号首位）：提交 `PickBin=首位`(1位) + `PickCountry`，后端按 `bin_prefix LIKE 'N%'` 匹配、按 `head<N>` 取加价；1 位首位可与国家共存（交集过滤），6 位 BIN 与国家互斥。网页端 type 模式可同时选首位与 DEBIT/CREDIT，加价叠加（`head<N>` + `D`/`C`）；bot 端 8 按钮互斥（选了 N 头不再选种类），不触发叠加。首位库存展示为全商品总量（不分国家，与 bot 一致），精确「首位+国家」库存由后端下单/履约时保证。
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
