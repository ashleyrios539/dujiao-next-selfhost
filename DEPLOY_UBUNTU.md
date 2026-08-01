# Dujiao-Next 自动测活版 · Ubuntu 部署教程

本文档面向从源码编译部署的 Ubuntu 服务器，包含**自动测活**功能的完整安装步骤。

适用系统：Ubuntu 22.04 / 24.04（x86_64）。已测试版本：Go 1.26.5、Node.js 24、pnpm 10。

> **快速开始**：仓库自带一键部署脚本 `deploy_ubuntu.sh`，可自动完成依赖安装、源码构建、配置生成、systemd 服务与 Nginx 配置。**全新服务器**（Ubuntu 22.04 / 24.04）装完系统后可直接从 GitHub 拉取运行：
>
> ```bash
> # 方式 A：下载后运行（交互式向导，推荐）
> wget https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh
> sudo bash deploy_ubuntu.sh
>
> # 方式 B：一条命令直装
> sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh)"
>
> # 方式 C：非交互 + 环境变量预设（无人值守）
> sudo DOMAIN=shop.example.com DJ_ADMIN_PASS='你的密码' bash deploy_ubuntu.sh
> ```
>
> 脚本默认开启分销/代理商功能且**关闭子站**（`reseller.enabled: true`、`reseller.sub_sites_enabled: false`，即主站代理中心按渠道价批发采购），可用环境变量 `RESELLER_ENABLED` / `RESELLER_SUB_SITES_ENABLED` 覆盖。也可手动按下列各节逐步部署。

---

## 目录

1. [环境准备](#一环境准备)
2. [获取源码](#二获取源码)
3. [构建（前端 + 后端）](#三构建前端--后端)
4. [配置文件 config.yml](#四配置文件-configyml)
5. [创建运行用户与目录](#五创建运行用户与目录)
6. [首次启动与初始化管理员](#六首次启动与初始化管理员)
7. [注册 systemd 服务](#七注册-systemd-服务)
8. [Nginx 反向代理 + HTTPS](#八nginx-反向代理--https)
9. [配置自动测活功能](#九配置自动测活功能)
10. [运维命令](#十运维命令)
11. [常见问题排查](#十一常见问题排查)

---

## 一、环境准备

以 `root` 或具有 `sudo` 权限的用户执行。

### 1. 更新系统并安装基础工具

```bash
sudo apt update && sudo apt upgrade -y
sudo apt install -y git curl wget unzip build-essential nginx redis-server ca-certificates
```

### 2. 安装 Go（≥ 1.26.5）

```bash
# 若需其他版本，到 https://go.dev/dl/ 查看最新版本号
wget https://go.dev/dl/go1.26.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.26.5.linux-amd64.tar.gz
rm go1.26.5.linux-amd64.tar.gz
```

写入 PATH（追加到 `~/.bashrc` 或 `/etc/profile.d/go.sh`）：

```bash
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' | sudo tee /etc/profile.d/go.sh
source /etc/profile.d/go.sh
go version   # 应输出 go version go1.26.5 linux/amd64
```

### 3. 安装 Node.js 24 与 pnpm 10

```bash
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash -
sudo apt install -y nodejs
node -v      # 应输出 v24.x.x

# 启用 corepack 并固定 pnpm 版本（项目使用 pnpm 10）
sudo corepack enable
corepack prepare pnpm@10.34.3 --activate
pnpm -v      # 应输出 10.34.3
```

### 4. 启动 Redis（可选但强烈建议）

队列与缓存依赖 Redis。若前一步安装了 `redis-server`：

```bash
sudo systemctl enable --now redis-server
redis-cli ping   # 应输出 PONG
```

> 若不使用 Redis，请把 `config.yml` 中 `redis.enabled` 和 `queue.enabled` 设为 `false`，但支付回调、异步交付、测活任务都将变为同步执行，不建议生产环境关闭。

---

## 二、获取源码

```bash
mkdir -p /opt/src && cd /opt/src
git clone https://github.com/ashleyrios539/dujiaodxcheck_xhenmo01.git dujiao-next
cd dujiao-next
```

> 本仓库 `main` 分支已包含自动测活功能的全部改动，直接克隆即可。
> 若仓库为私有，服务器上 clone 前需配置访问凭证（如 GitHub Personal Access Token 或部署密钥）。

---

## 三、构建（前端 + 后端）

### 1. 安装前端依赖

```bash
cd /opt/src/dujiao-next

# admin 后台（fullstack 模式，注入 admin_path 占位符）
cd frontend/admin && pnpm install && pnpm run build:fullstack && cd ../..

# 用户前台
cd frontend/user && pnpm install && pnpm run build && cd ../..
```

### 2. 把前端产物放到 Go embed 目录

```bash
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
```

### 3. 编译 fullstack 二进制

```bash
CGO_ENABLED=0 go build -trimpath -tags release,fullstack \
  -ldflags="-s -w -X github.com/dujiao-next/internal/version.Version=v1.0.0 \
            -X github.com/dujiao-next/internal/version.BuildType=release" \
  -o dujiao-next ./cmd/server

./dujiao-next --help  # 确认可执行
```

> - SQLite 使用纯 Go 驱动（`glebarez/sqlite`），无需 CGO。
> - 产物为一个自带前后端页面的单二进制，之后只需要分发这一个文件 + `config.yml`。

---

## 四、配置文件 config.yml

### 1. 生成三组互不相同的强随机密钥

```bash
openssl rand -hex 32
openssl rand -hex 32
openssl rand -hex 32
```

### 2. 编写配置

```bash
cp config.yml.example config.yml
nano config.yml
```

关键项（务必修改）：

```yaml
app:
  secret_key: <第1组随机值>          # AES-256 加密敏感数据

server:
  host: 0.0.0.0
  port: 8080
  mode: release                      # 生产环境必须为 release

database:
  driver: sqlite                     # sqlite / postgres
  dsn: /opt/dujiao/db/dujiao.db      # 建议放到安装目录下

jwt:
  secret: <第2组随机值>              # 必须 ≥32 字符且与其它密钥不同
user_jwt:
  secret: <第3组随机值>

bootstrap:
  default_admin_username: ""         # 首次初始化管理员，可留空改用环境变量
  default_admin_password: ""

web:
  admin_path: "/dj-mgmt-7x9k2"       # 强烈建议改成不易猜测的后台路径
```

> - 密钥过弱、重复或为默认值时服务会**拒绝启动**（`weak runtime secret` 检查）。
> - 后台路径 `web.admin_path` 只对 fullstack 二进制生效，改个不易猜测的值更安全。
> - 若使用 PostgreSQL，将 `database.driver` 改为 `postgres` 并填写 DSN，例如
>   `postgres://user:pass@127.0.0.1:5432/dujiao?sslmode=disable`。

### 3.（可选）邮件 / 支付配置

邮件用于订单状态通知、注册验证，支付用于收款。按需填写 `email:` 段与各支付渠道配置。未配置前功能可正常使用，但无法发送通知与收款。

### 4. 分销/代理商（渠道价批发，不开子站）

一键脚本 `deploy_ubuntu.sh` 生成 config.yml 时已默认写入如下 `reseller:` 段（可通过环境变量 `RESELLER_ENABLED`、`RESELLER_SUB_SITES_ENABLED` 覆盖）：

```yaml
reseller:
  enabled: true                  # 开启分销/代理商功能
  main_hosts:
    - localhost
    - 127.0.0.1
    - "::1"
  trusted_forwarded_host: false
  subdomain_base: ""             # 不开子站，无需泛解析
  sub_sites_enabled: false       # 关闭子站：仅主站「代理中心」按渠道价批发采购
  self_apply_enabled: true       # 允许用户在个人中心自助申请开通代理
  settlement_confirm_days: 7
```

> **已有服务器**：`deploy_ubuntu.sh` 检测到已存在 `config.yml` 会跳过生成。请手动在现有 `config.yml` 末尾补上上面的 `reseller:` 段，再 `sudo bash deploy_ubuntu.sh` 更新程序即可。

> **使用流程**：代理审核通过后，登录主站 → 个人中心 → 分销中心 → **批发采购**，按渠道价批量下单付款，平台发货到代理名下。渠道价（批发进价，可低于零售价、不得低于成本价）在管理端「分销 → 商品配置」按商品/SKU 设置。

---

## 五、创建运行用户与目录

生产环境建议以独立低权限用户运行：

```bash
sudo useradd -r -s /usr/sbin/nologin dujiao
sudo mkdir -p /opt/dujiao
sudo cp /opt/src/dujiao-next/dujiao-next /opt/dujiao/
sudo cp /opt/src/dujiao-next/config.yml /opt/dujiao/

# 运行时自动创建 db/uploads/logs，但目录所有者需为运行用户
sudo chown -R dujiao:dujiao /opt/dujiao
```

> 程序含自更新/回滚的状态文件机制，**安装目录必须对运行用户可写**，否则服务会拒绝启动。

---

## 六、首次启动与初始化管理员

### 1. 先手动启动一次完成建库与迁移

```bash
cd /opt/dujiao
sudo -u dujiao ./dujiao-next
```

看到 `Embedded SPAs` 与数据库迁移日志无报错后，`Ctrl+C` 停止。

### 2. 初始化管理员（二选一）

**方式 A：环境变量（推荐）**，修改 systemd 服务（见下节）后由 systemd 注入：

```ini
Environment=DJ_DEFAULT_ADMIN_USERNAME=admin
Environment=DJ_DEFAULT_ADMIN_PASSWORD=<强密码，至少8位且含大小写字母数字>
```

**方式 B：写入 config.yml 的 `bootstrap` 段**（初始化后建议改回空值）。

启动后访问 `https://你的域名{admin_path}` 即可用该账号登录后台。

---

## 七、注册 systemd 服务

创建 `/etc/systemd/system/dujiao.service`：

```ini
[Unit]
Description=Dujiao-Next
After=network.target redis-server.service
Wants=redis-server.service

[Service]
Type=simple
User=dujiao
Group=dujiao
WorkingDirectory=/opt/dujiao
ExecStart=/opt/dujiao/dujiao-next
Restart=on-failure
RestartSec=3
# 首次初始化管理员（初始化成功后建议注释掉）
Environment=DJ_DEFAULT_ADMIN_USERNAME=admin
Environment=DJ_DEFAULT_ADMIN_PASSWORD=<强密码>

[Install]
WantedBy=multi-user.target
```

启用并启动：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now dujiao
sudo systemctl status dujiao
```

验证服务监听：

```bash
curl -s http://127.0.0.1:8080/health
# 期望输出 {"status":"ok"} 之类健康信息
```

---

## 八、Nginx 反向代理 + HTTPS

### 1. 站点配置

创建 `/etc/nginx/sites-available/dujiao`：

```nginx
server {
    listen 80;
    server_name shop.example.com;   # 换成你的域名

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
```

```bash
sudo ln -s /etc/nginx/sites-available/dujiao /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

> - `proxy_read_timeout` 调大是因为测活流程在交付任务中会做外部 HTTP 轮询。
> - 测活实际在异步 worker 中执行，不阻塞用户下单请求；以上超时主要影响订单状态页刷新。

### 2. 申请 HTTPS 证书（Let's Encrypt）

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d shop.example.com
sudo systemctl reload nginx
```

之后用 `https://shop.example.com` 访问前台，`https://shop.example.com/你的后台路径` 访问后台。

---

## 九、配置自动测活功能

部署完成后，登录后台完成以下步骤，即可启用"用户自选测活、只卖活卡"。

### 1. 在后台「测活设置」页面配置（推荐）

后台 → **设置** → **测活设置 (Card Check)**，填写并保存：

- **启用测活**：总开关
- **CheckDx 卡密**：在 dxchecklive.com 购买/充值的卡密（点数凭证）
- **检测接口（站点）**：值需在 CheckDx 网页端"设置/接口"查看，维护中的接口不可用
- **卡片发行国家**：默认 `美国`
- **检测缓冲数量 / 检测超时 / 结果轮询间隔**：按需调整

配置完成后，点卡密输入框旁的 **「测试连接」** 按钮，可立即校验卡密是否有效并显示剩余点数。

### 2.（可选）通过 API 配置（等价方式）

```bash
# 登录后台获取 token（用户名/密码为初始化时设置的管理员）
#    响应为 {"status_code":0,"msg":"success","data":{"token":"..."}}
TOKEN=$(curl -s -X POST https://shop.example.com/api/v1/admin/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"你的密码"}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
echo "$TOKEN"

# 查看当前测活配置
curl -s "https://shop.example.com/api/v1/admin/settings?key=card_check_config" \
  -H "Authorization: Bearer $TOKEN"

# 测试卡密是否有效（返回剩余点数）
curl -s -X POST "https://shop.example.com/api/v1/admin/settings/card-check/test" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"kami":"你的CheckDx卡密"}'

# 写入测活配置
curl -s -X PUT https://shop.example.com/api/v1/admin/settings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "key": "card_check_config",
    "value": {
      "enabled": true,
      "kami": "你的CheckDx卡密",
      "interface": "post5（1.0pt）【✅ Open|开放中】_global",
      "country": "美国",
      "buffer": 5,
      "timeout_seconds": 60,
      "poll_interval_millis": 2000
    }
  }'
```

字段说明：

| 字段 | 含义 |
|---|---|
| `enabled` | 是否启用测活 |
| `kami` | 你在 dxchecklive.com 购买/充值的卡密（点数余额） |
| `interface` | 使用的检测接口/站点，值可在 CheckDx 网页端"设置/接口"中查看（含接口名与维护状态，维护中的接口不可用） |
| `country` | 卡片发行国家，默认 `美国` |
| `buffer` | 每单额外多检测的卡数量（活卡不足时补充，默认 5） |
| `timeout_seconds` | 单次检测最长等待秒数（默认 60，最大 300） |
| `poll_interval_millis` | 结果轮询间隔毫秒（默认 2000） |

### 3. 按商品开启"支持测活"并设置测活价格

对需要提供"测活"选项的**自动发货（auto）**商品执行（人工发货商品不走测活流程）：

```bash
# 开启支持测活（也可在管理后台商品列表点击"测活"徽章一键切换）
curl -s -X PATCH https://shop.example.com/api/v1/admin/products/123 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"card_check_enabled": true}'

# 设置测活价格（可选）：用户开启测活后商品价格 = 商品原价 + 该金额
curl -s -X PATCH https://shop.example.com/api/v1/admin/products/123 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"card_check_fee": 2}'
```

（`123` 换成商品 ID。也可在管理后台 → 商品 → 编辑中勾选"支持测活"并填写测活价格。）

### 4. 卡密导入格式

测活需要从卡密文本中解析出 **卡号 / 到期日期 / 三位安全码**。支持以下常见格式（导入时每行一条）：

```
4111111111111111|09|26|123
4111111111111111|09/26|123
card=4111111111111111 exp=09/26 cvv=123
{"card_number":"4111111111111111","expiration":"09/26","cvv":"123"}
```

> 解析失败的卡密会被标记为 `invalid`（失效）并从可售库存移除，不会交付给用户。

### 5. 用户端与行为说明（重要）

**用户端交互**：
- 商品页：支持测活的商品会显示"开启测活"勾选框，并同时展示**不测活价格 / 测活价格**两档价格（无"加价"字样）。
- 结算页：已开启测活的商品显示"已开启测活"标识，金额按测活价格计算。

**后端行为**：
- **只对勾选测活（且已付测活价格）的订单项检测**，交付给用户的卡密仅包含 CheckDx 判定为 `Live` 的卡。
- 未勾选测活的订单项**直接发货不检测**（按原价，可能含死卡）。
- **死卡处理**：`Dead` / `Unknown` / 无法解析的卡自动标记为 `invalid` 状态，不再上架。
- **故障兜底**：测活 API 不可用或接口维护时**不会误清库存**，订单进入重试；活卡不足时订单按"库存不足"处理并重试。
- **退点机制**：每次检测任务结束会自动调用 CheckDx 结束接口，未检测到的卡点数自动退回。
- **日志关键词**：`checkdx_task_started`、`checkdx_task_stopped`、`checkdx_partial_results`、`fulfillment_card_check_done`，用于排查测活情况。

---

## 十、运维命令

```bash
# 服务状态 / 日志
sudo systemctl status dujiao
sudo journalctl -u dujiao -f
tail -f /opt/dujiao/logs/app.log

# 重启 / 停止
sudo systemctl restart dujiao
sudo systemctl stop dujiao

# 备份（先停服再拷贝数据目录更安全）
sudo systemctl stop dujiao
sudo tar czf dujiao-backup-$(date +%F).tar.gz /opt/dujiao/db /opt/dujiao/config.yml
sudo systemctl start dujiao

# 管理员维护子命令（在安装目录执行）
cd /opt/dujiao
sudo -u dujiao ./dujiao-next admin list-admins        # 列出管理员
sudo -u dujiao ./dujiao-next admin reset-password     # 重置管理员密码
sudo -u dujiao ./dujiao-next admin reset-2fa          # 重置管理员 2FA
```

### 更新版本

**方式 A：一键脚本更新（推荐）**——脚本会拉取最新代码、重新构建前后端并重启，已有的 `config.yml` 不会被覆盖：

```bash
sudo bash /opt/src/dujiao-next/deploy_ubuntu.sh
# 或
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh)"
```

**方式 B：手动更新**

```bash
cd /opt/src/dujiao-next
git pull --rebase
# 重新构建前端 + 后端（见第三节），产出新 dujiao-next
sudo cp dujiao-next /opt/dujiao/dujiao-next
sudo chown dujiao:dujiao /opt/dujiao/dujiao-next
sudo systemctl restart dujiao
```

> - 升级到包含「测活设置页」的新版本后，后台 **设置 → 测活设置** 即可用网页配置，不再需要敲 curl。
> - 若升级前用 curl 配置过 `card_check_config`，设置页会自动加载已有值，直接确认/修改即可。
> - 程序内置自更新/回滚机制：升级失败可执行
>   `cd /opt/dujiao && sudo -u dujiao ./dujiao-next rollback --force` 回滚（会先校验数据库迁移状态）。

---

## 十一、常见问题排查

| 现象 | 处理 |
|---|---|
| 启动报 `密钥过弱、重复或仍为默认值` | 修改 `app.secret_key` / `jwt.secret` / `user_jwt.secret` 为 ≥32 字符且互不相同的随机值 |
| 启动报 `unable to open database file` | 检查 `/opt/dujiao/db` 目录存在且 `dujiao` 用户可写 |
| 启动报 `web.admin_path 配置错误` | 检查 `web.admin_path` 以 `/` 开头且不是 `/api`、`/uploads`、`/health` 等保留前缀 |
| 后台打开空白 / 404 | 确认二进制为 fullstack 构建（启动日志含 `Embedded SPAs`），且 `web.admin_path` 与访问路径一致 |
| 下单后一直"发货中"且日志有 `fulfillment_card_check_failed` | 检查 CheckDx 卡密余额、`interface` 是否维护中、服务器能否访问 `https://dxchecklive.com` |
| 日志有 `checkdx_start_failed` | 多为卡密无效、余额不足或接口值错误；到 CheckDx 网页端核对 |
| 日志有 `checkdx_partial_results` | 部分卡超时未返回结果，可调大 `timeout_seconds`；未检测到的卡会退点且不标记失效 |
| 所有卡都被标 `invalid` | 检查卡密导入格式是否符合测活解析规则（见第九节第 4 点） |

---

## 附：端口与依赖一览

| 组件 | 说明 |
|---|---|
| `dujiao-next` :8080 | 主服务（HTTP + 异步 worker 同一进程，`-mode all`） |
| Redis :6379 | 队列（asynq）与缓存 |
| Nginx :80/:443 | 反向代理 + TLS |

如需前后端分离（API 与 worker 拆开跑），可分别用 `-mode api` 与 `-mode worker` 启动两个实例并共用同一数据库，部署方式与本教程相同，此处不再展开。
