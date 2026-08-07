# Telegram Bot 配置教程与功能说明

> 本文档对应仓库最新改动：**原生 Telegram Webhook Bot**（提交 `5de7d99`，不再依赖外部 licensed client）。
> Bot 直接在 Go 服务进程内接收 Telegram 推送并回复，无需购买任何第三方许可证。

---

## 一、功能总览

系统里的 "Telegram" 相关能力一共有 5 块，配置入口各不相同：

| 功能 | 作用 | 配置位置 |
|---|---|---|
| **① Bot 消息机器人**（原生 webhook） | 用户与 Bot 私聊：`/start` 欢迎语、`/help` 帮助中心、`/menu` 内联菜单 | 后台「Telegram Bot」各页面 + `config.yml` 的 `telegram_webhook` |
| **② Bot 客户端**（Channel Client） | 存放 Bot Token（AES-256-GCM 加密入库），是 ①③④ 的凭证来源 | 后台「Telegram Bot → Bot 客户端」 |
| **③ Telegram 登录** | 用户可用 Telegram 账号一键登录/注册商城账号 | 后台「设置 → Telegram」+ `config.yml` 的 `telegram_auth` |
| **④ 消息群发**（Broadcast） | 向绑定了 Telegram 的用户批量推送消息 | 后台「Telegram Bot → 消息群发」 |
| **⑤ 事件通知**（Notify） | 订单/支付等事件推送到指定 Telegram chat | 后台「设置 → 通知」的 Telegram 渠道 |

其中 **① 是本次修改新增的核心功能**，②③④⑤ 为原有功能，一并保留。

---

## 二、Bot 消息机器人功能（原生 webhook 版）

### 2.1 工作流程

```
Telegram 用户 ──发消息──> Bot（Telegram 服务器）
                              │  POST（带 X-Telegram-Bot-Api-Secret-Token 头）
                              ▼
             你的服务器  POST /api/v1/telegram/webhook
                              │  校验 secret_token → 解析 Update
                              ▼
            internal/modules/telegram/webhook 模块处理并回复
```

- 服务**启动时**自动调用 Telegram `setWebhook`，把回调指向 `webhook_url`；
- 后台保存 Bot 配置后**自动重新应用** webhook 并同步运行时状态；
- Bot 未启用或 `webhook_url` 为空时，启动/保存时会清除 webhook 并标记状态为 `disabled`。

### 2.2 支持的命令

| 命令 | 行为 |
|---|---|
| `/start` | 发送欢迎语（后台配置，可多语言），并附带六键快捷菜单：**开始购物 / 卡头库存 / 我的钱包 / 我的订单 / 语言切换 / 帮助中心**；未配置欢迎语时只发主菜单提示 |
| `/shop` | **进入在线购买**：在聊天内浏览分类→商品→选 SKU/数量→输入挑头 BIN→勾选测活→确认下单→余额或在线支付（仅私聊可用，群组内会提示改用私聊） |
| `/recharge` | **进入钱包充值**：输入金额 → 选支付渠道 → 生成收银台链接 → 支付后自动到账 |
| `/help` | 发送帮助中心（标题 + 简介 + 各条 FAQ 摘要 + 提示） |
| `/menu` | 发送主菜单提示 + 内联键盘菜单 |
| 其他文本（私聊） | 回复主菜单提示 |
| 其他文本（群组） | 忽略，不打扰群聊 |

Bot 命令菜单（Telegram 输入框的 `/` 菜单）会在启用时通过 `SetMyCommands` 自动注册（含 `/shop`）。

### 2.2.1 聊天内购买（`/shop`）

用户发送 `/shop` 后，在对话内完成完整购买闭环，**无需跳转网页**：

```
/shop
   │  自动识别 Telegram 身份 → 解析/绑定商城账号（复用商城同一账号体系）
   ▼
选择分类 ──> 选择商品 ──> 商品配置面板（分步引导，与网页商品页一致）
   │  ├─ 选 SKU / 调整数量
   │  ├─ 挑卡方式（三选一）
   │  │    ├─ 随机：选国家（按库存降序，可回复双字母代码）
   │  │    ├─ 挑头(BIN)：直接输入 6 位 BIN（无需国家），实时显示可用库存
   │  │    └─ 挑卡种类：选国家 → 品牌 → 卡类型(D/PD/C)
   │  └─ 测活开关（两档价格：不测活价 / 测活价，均含挑卡加价）
   ▼
订单确认（展示单价 / 测活费 / 挑卡加价 / 合计）
   ▼
余额支付 或 在线支付（USDT/TRC20：聊天内直接显示应付金额+收款地址）
   ▼
支付成功 → 自动发货 → 卡密以 txt 文件推送到本对话
```

- 挑卡（随机/BIN/种类）与测活均**复用网页商城的计价与校验逻辑**（后端 `order` 模块），订单快照（`PickCountry`/`PickBrands`/`PickCardTypes`/`PickBin`/`CardCheckEnabled`）可在网页个人中心查看。
- **挑卡选项多语言**：品牌/卡类型按钮文案随用户语言显示（与网页端一致——品牌「随机」和卡类型 D/PD/C 走 i18n，Visa/Mastercard 等品牌名固定），切换语言后选项与摘要即时更新。
- 国家选择支持两种方式：点击内联键盘中的国家按钮，或**直接回复双字母国家代码**（如 `US`）；国家按可用库存降序排列。
- 挑头(BIN)模式不需要选国家；随机与挑卡种类模式**必须选国家**。
- 未绑定 Telegram 的用户首次 `/shop` 会自动创建并绑定商城账号；网页端也可在个人中心绑定 Telegram。
- 支付使用网页商城余额或在线支付渠道，支付/发货进度在网页商城个人中心可见。

### 2.3 内联菜单（`/menu`）

内置 7 个菜单项（key 固定，label 可在后台改多语言）：

| Key | 默认标签（zh-CN） | 含义 |
|---|---|---|
| `shop_home` | 🛍️ 开始购物 | **直接进入 bot 内购买流程**（等同 `/shop`） |
| `my_orders` | 📦 我的订单 | 查看订单（引导至网页个人中心） |
| `my_wallet` | 💰 我的钱包 | **直接查询并展示当前商城余额** |
| `affiliate` | 📣 推广返利 | 分销推广（引导至网页） |
| `gift_card` | 🎁 礼品卡兑换 | 兑换礼品卡（引导至网页） |
| `switch_language` | 🌐 切换语言 | 切换语言（引导至网页） |
| `contact_support` | ❓ 帮助中心 | 打开帮助中心 |

> `shop_home` 与 `my_wallet` 已在 bot 内直接可用（不需要跳转网页）；其余内置项点击后回复引导提示。

### 2.5.1 bot 内常用功能

- **我的订单**：列出最近 10 条订单（订单号/金额/状态），点击订单查看详情，已发货订单直接展示**卡密**（自动发货商品）。
- **卡头库存**：输入 6 位 BIN，汇总展示各商品中该卡头的可用库存与合计。
- **语言切换**：中/英双向切换（当前中文显示「English」，英文显示「中文」），偏好写入商城账号（`users.locale`），bot 与网页共用。**切换后立即生效**：处于购买流程中会以新语言重渲当前页面（并刷新最新数据），否则直接发送新语言的主菜单（提示 + 快捷键盘按钮即时切换语言）。`/menu` 里的「切换语言」菜单项同样生效。
- **充值**：`/recharge` 或「我的钱包」结果中的「💳 充值」按钮 → 输入金额 → 选渠道（USDT/Epusdt 等）→ 生成收银台链接，支付后**自动到账**（复用既有支付回调链路）。
- **在线支付（epusdt 直连）**：购买确认页点「在线支付」后，**一条消息**完成支付信息展示：主图是收款地址**二维码**，图片说明（caption）里是应付 USDT 金额、TRC20 收款地址（代码块，**点按即复制**）、复制提示、网络、过期时间与操作提示，按钮（🔄 刷新支付状态 / 📦 我的订单 / 🏠 返回主页）挂在图片消息上，无需跳转收银台；支付成功后自动发货，卡密以 **txt 文件**推送到本对话。
- **充值（epusdt 直连）**：`/recharge`（或钱包页「💳 充值」）→ 输入 **USDT 金额**（提示语明确为「请输入充值 USDT 金额」）→ **一条消息**展示：二维码主图 + caption 里的应付 USDT、收款地址（点按即复制）、复制提示、网络、过期时间与操作提示，按钮（🔄 查看到账状态 / 🏠 返回主页）挂在图片消息上，支付后自动到账。付款页展示后会话即结束，**再次输入数字不会重复创建充值订单**，输入其他内容自动回退主菜单。
- **发货主动推送（txt 文件）**：订单发货完成后，bot 自动把卡密以 **`.txt` 文件**（文件名如 `卡密_订单号.txt`）推送到用户私聊（原生 webhook 直发，替代外部 channel client 回调链路）。

每个菜单项可配置 **action 类型**（后台「菜单配置」页）：

| 类型 | 行为 |
|---|---|
| `builtin` | 内置动作：点击后回复对应提示文本（如"请在商城个人中心查看订单"） |
| `url` | 按钮直接跳转外部链接 |
| `web_app` | 按钮打开 Telegram Web App（Mini App） |
| `command` | 按钮触发命令回调（callback_data = 命令值） |

菜单上限 20 项，每行 2 个按钮；内置 7 项无法删除，只能启停/改顺序/改 label。

### 2.4 帮助中心（`/help`）

- 上限 12 条 FAQ，每条可配置多语言「摘要 / 标题 / 正文」，并按 `order` 升序排列；
- 默认内置 4 条：怎么下单、订单问题、钱包充值、联系客服；
- 发送 `/help` 会显示帮助中心**列表**：标题 + 简介 + 每条 FAQ 的摘要按钮（可点击）+ 中心提示 + 客服兜底提示；
- 点击任意摘要按钮 → 进入该条**详情**：展示该条标题 + 正文；若该条开启了「详情页附带客服链接」且基础设置里配置了支持链接，详情底部会出现「💬 联系客服」按钮（点击跳转支持链接）；若未配置支持链接，则正文后补充客服兜底提示；底部恒有「↩️ 返回帮助中心」按钮可回到列表；
- 后台「Telegram Bot → 帮助中心」页编辑保存后，bot **无需重启**即在下一次 `/help` 生效。

### 2.5 多语言

所有文本（欢迎语、菜单 label、帮助中心、内置提示）都支持 `zh-CN` / `zh-TW` / `en-US` 三种语言，Bot 默认语言在「基础设置 → 默认语言」选择；文本缺失时自动回退到默认语言，再回退到任一非空语言。

---

## 三、配置教程

### 第 0 步：创建 Telegram Bot（BotFather）

1. 在 Telegram 里找 **@BotFather**，发送 `/newbot`；
2. 按提示设置 Bot 名称和用户名（用户名需以 `bot` 结尾，如 `my_shop_bot`）；
3. 记下返回的 **Bot Token**（形如 `123456789:AAxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`）；
4. （可选，仅登录功能需要）在 **@BotFather → Bot Settings → Web Login** 里申请 Web Login 并获取 Client Secret，把商城域名 origin 和回调地址加入 Allowed URLs。

### 第 1 步：修改 `config.yml`（服务器上）

找到 `telegram_webhook` 段并填写：

```yaml
telegram_webhook:
  webhook_url: "https://你的商城域名/api/v1/telegram/webhook"  # 必须是公网可访问的 HTTPS
  secret_token: "用 openssl rand -hex 32 生成的随机字符串"       # 防止别人伪造请求
```

> ⚠️ Telegram 强制要求 webhook 使用 **HTTPS**（除非是 localhost 调试）。
> `secret_token` 会通过 `X-Telegram-Bot-Api-Secret-Token` 请求头校验，务必配置。

### 第 2 步：填写 Bot Token（后台「Bot 客户端」）

1. 登录后台 → 左侧 **Telegram Bot → Bot 客户端**；
2. 点击「创建」，填写：
   - **名称**（必填）：如 `我的商城机器人`
   - **channel_type**：固定为 `telegram_bot`
   - **bot_token**：第 0 步拿到的 Token
   - **callback_url**：可选
   - **描述**：可选
3. 保存。Token 会以 AES-256-GCM 加密存储（密钥由 `app.secret_key` 派生），不会明文入库/回显。

> 该 Token 是 Bot 消息功能与消息群发的凭证来源。如果之前配置过旧的 Bot 客户端，确认状态为「启用」。

### 第 3 步：启用并配置 Bot（后台「基础设置」）

后台 → **Telegram Bot → 基础设置**：

1. 打开 **启用** 开关；
2. 选择 **默认语言**（zh-CN / zh-TW / en-US）；
3. 填写 **基本信息**：显示名称、简介（多语言）、支持链接、封面图；
4. 配置 **欢迎语**（开启开关 + 多语言文本）；
5. 点击 **保存** —— 保存后后端会自动调用 `setWebhook` 并同步状态。

> 保存时若 `config.yml` 里 `webhook_url` 为空，Bot 会处于 `disabled` 状态，请在配置文件中补上。

### 第 4 步：按需配置菜单与帮助中心

- **菜单配置**：后台 → Telegram Bot → 菜单配置，调整 7 个内置项的顺序/开关/多语言 label，或新增自定义项（选择 action 类型与值）；
- **帮助中心**：后台 → Telegram Bot → 帮助中心，增删改 FAQ 条目。

### 第 5 步：验证

1. Telegram 里打开你的 Bot，点 **Start** 或发送 `/start` → 应收到欢迎语 + 主菜单提示；
2. 发送 `/menu` → 应弹出内联键盘；
3. 发送 `/help` → 应显示帮助中心；
4. 点击内联按钮 → 应收到对应提示（或跳转链接 / 打开 Mini App）；
5. 回到后台 → **Telegram Bot → 概览/连接状态**，应显示：

```
Connected: true
Bot Version: native-webhook@<你的bot用户名>
Webhook Status: active
License Status: native（无需购买许可证）
```

### 第 6 步（可选）：开启 Telegram 登录

后台 → **设置 → Telegram**：

| 字段 | 说明 |
|---|---|
| 启用 | 开关 |
| Bot 用户名 | 不含 `@`，如 `my_shop_bot` |
| Bot Token | Telegram 登录/通知使用的 Token（与 Bot 客户端里的是两套，可相同） |
| Mini App URL | Telegram Mini App 页面地址（需与 BotFather 的 Web App URL 一致） |
| 登录过期秒数 | 默认 300 |
| 防重放秒数 | 默认 300 |
| Client Secret / OIDC 回调地址 | **新版 OIDC 登录**：在 BotFather → Bot Settings → Web Login 获取 Client Secret；回调地址填 `https://你的商城域名/auth/telegram/callback`，并在 BotFather 的 Allowed URLs 里加入该地址与域名 origin。填了这两项自动走新版 OIDC，否则走旧版 Login Widget |

用户端效果：
- 登录页出现「Telegram 登录」入口，一键登录/注册；
- 用户可在个人中心绑定/解绑 Telegram；管理员可在用户管理中解绑用户的 Telegram；
- 绑定过 Telegram 的用户是「消息群发」的收件人。

### 第 7 步（可选）：配置事件通知

后台 → **设置 → 通知**：
1. 找到 Telegram 渠道，开启；
2. 在 recipients 里填写要接收通知的 Telegram chat_id（每个一行）；
3. 保存。此后订单/支付等事件会通过 Bot 发送消息到这些 chat。

> 通知使用的 Token 来自「设置 → Telegram」里的 Bot Token（不是 Bot 客户端的 Token）。

### 第 8 步（可选）：消息群发

后台 → **Telegram Bot → 消息群发**：
1. 新建广播：填写标题、选择收件人（全部绑定用户 / 指定用户 / 按条件筛选）、消息内容、附件；
2. 提交后任务通过 Redis 队列异步投递（Redis 不可用时退回进程内执行）；
3. 可在广播详情查看投递进度。

---

## 四、常见问题

| 问题 | 排查 |
|---|---|
| Bot 不回消息 | ① 确认后台「基础设置」已启用并保存过；② 确认 `config.yml` 的 `webhook_url` 是 HTTPS 且公网可达（可用 `curl -I https://域名/api/v1/telegram/webhook` 测试）；③ 看后台「连接状态」页的 Webhook Status，`set_commands_failed` / `get_me_failed` 说明 Token 无效；④ 检查服务日志中 `telegram_webhook_apply_failed` |
| 后台显示未连接 | 「连接状态」页的 Connected 为 false 时，点后台保存一次配置会触发重新应用 webhook 与状态同步 |
| 换 Bot 后不生效 | 在「Bot 客户端」创建/启用新的 `telegram_bot` 客户端，再保存一次基础设置 |
| 只想要 Bot 消息，不需要登录 | 不填「设置 → Telegram」任何内容即可，两者互不影响 |
| webhook 收到 401 | `config.yml` 的 `secret_token` 与 Telegram 端记录的不一致（Telegram 由 setWebhook 时传入），修改后保存一次后台配置或重启服务 |

---

## 五、相关代码位置（便于后续维护）

| 内容 | 路径 |
|---|---|
| webhook 应用服务（命令/菜单/回调处理） | `internal/modules/telegram/webhook/application/service.go` |
| 内置命令/菜单/内联键盘构建 | `internal/modules/telegram/webhook/application/helpers.go` |
| webhook HTTP 端点（`POST /api/v1/telegram/webhook`） | `internal/modules/telegram/webhook/transport/http/` |
| Bot API 客户端（sendMessage/setWebhook/getMe 等） | `internal/modules/telegram/notify/infrastructure/botapi/client.go` |
| Bot 配置 schema（菜单/帮助/欢迎） | `internal/modules/settings/schema/messaging/telegram_bot.go` |
| 后台 Bot 设置 API | `internal/modules/settings/transport/http/telegram_bot_handler.go` |
| Bot Token 加密存储（channel_clients 表） | `internal/modules/channelclient/application/service.go` |
| 启动时应用 webhook | `internal/app/bootstrap.go` |
| Telegram 登录（Widget / OIDC / Mini App） | `internal/modules/identity/userauth/application/telegram_*.go` |
| 后台页面 | `frontend/admin/src/views/admin/TelegramBot*.vue`、`Settings.vue`（Telegram tab）、`Notifications.vue` |
