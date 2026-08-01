#!/usr/bin/env bash
# ============================================================================
#  Dujiao-Next（含自动测活）Ubuntu 一键部署脚本（自包含，全新服务器直接可用）
#  适用系统：Ubuntu 22.04 / 24.04（x86_64）
#
#  ═══════════ 全新服务器一键运行（任选其一）═══════════
#
#  方式 A：下载脚本后运行（交互式向导，推荐）
#    wget https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh
#    sudo bash deploy_ubuntu.sh
#
#  方式 B：一条命令直接装（保留交互式向导，需服务器能访问 GitHub）
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh)"
#
#  方式 C：非交互，全部用环境变量指定
#    sudo DOMAIN=shop.example.com DJ_ADMIN_PASS='你的密码' bash deploy_ubuntu.sh
#
#  ═══════════════════════════════════════════════════════════
#
#  功能：
#    1. 安装依赖（Git / Go / Node / pnpm / Redis / Nginx）
#    2. 克隆源码并构建 fullstack 单二进制（含自动测活功能）
#    3. 生成强随机密钥与 config.yml
#    4. 创建独立运行用户 + systemd 服务
#    5. 可选：配置 Nginx 反向代理与 HTTPS
#    6. 输出测活配置命令与登录信息
#
#  可配置项（通过环境变量、直接修改下方变量，或按脚本交互提示填写）：
#    REPO_URL      源码仓库地址
#    REPO_BRANCH   分支（默认 main）
#    SRC_DIR       源码目录（/opt/src/dujiao-next）
#    INSTALL_DIR   安装目录（/opt/dujiao）
#    SERVICE_USER  运行用户（dujiao）
#    SERVER_PORT   HTTP 端口（8080）
#    DOMAIN        站点域名；留空则跳过 Nginx/HTTPS
#    ADMIN_PATH    后台路径；留空则随机生成
#    DJ_ADMIN_USER 初始管理员用户名（默认 admin）
#    DJ_ADMIN_PASS 初始管理员密码；留空则随机生成并打印
#    RESELLER_ENABLED          分销/代理商功能开关（默认 true）
#    RESELLER_SUB_SITES_ENABLED 是否允许分销商开设子站（默认 false=仅主站渠道价批发采购）
#
#  参数：
#    -y, --yes     非交互模式：跳过交互提示，全部使用默认/环境变量值
#    -h, --help    显示帮助
# ============================================================================
set -euo pipefail

# ------------------------------ 可配置项 ----------------------------------
REPO_URL="${REPO_URL:-https://github.com/ashleyrios539/dujiaodxcheck_xhenmo01.git}"
REPO_BRANCH="${REPO_BRANCH:-main}"
SRC_DIR="${SRC_DIR:-/opt/src/dujiao-next}"
INSTALL_DIR="${INSTALL_DIR:-/opt/dujiao}"
SERVICE_USER="${SERVICE_USER:-dujiao}"
SERVER_PORT="${SERVER_PORT:-8080}"
DOMAIN="${DOMAIN:-}"
ADMIN_PATH="${ADMIN_PATH:-}"
DJ_ADMIN_USER="${DJ_ADMIN_USER:-}"
DJ_ADMIN_PASS="${DJ_ADMIN_PASS:-}"
RESELLER_ENABLED="${RESELLER_ENABLED:-true}"
RESELLER_SUB_SITES_ENABLED="${RESELLER_SUB_SITES_ENABLED:-false}"

# ------------------------------ 日志工具 ----------------------------------
RESET="\033[0m"; BOLD="\033[1m"; GREEN="\033[32m"; YELLOW="\033[33m"; RED="\033[31m"; CYAN="\033[36m"
log()  { echo -e "${GREEN}[$(date '+%H:%M:%S')]${RESET} $*"; }
info() { echo -e "${CYAN}[INFO]${RESET} $*"; }
warn() { echo -e "${YELLOW}[WARN]${RESET} $*"; }
err()  { echo -e "${RED}[ERROR]${RESET} $*" >&2; }

die() { err "$*"; exit 1; }

# ------------------------------ 参数解析 ----------------------------------
NON_INTERACTIVE=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    -y|--yes) NON_INTERACTIVE=1 ;;
    -h|--help)
      echo "用法：sudo bash $0 [-y|--yes] [-h|--help]"
      echo ""
      echo "  -y, --yes  非交互模式：跳过交互提示，使用环境变量或默认值"
      echo "  -h, --help 显示本帮助"
      echo ""
      echo "交互模式会依次询问：站点域名 / 后台路径 / 管理员用户名 / 管理员密码。"
      echo "也可通过环境变量预设（如 DOMAIN=shop.example.com sudo bash $0）。"
      exit 0
      ;;
    *) die "未知参数：$1（可用 -y 跳过交互，-h 查看帮助）" ;;
  esac
  shift
done

# ------------------------------ 交互提示 ----------------------------------
# ask 询问一个可配置项：环境变量已设置则跳过，否则提示并读取。
#   用法：ask "提示文案" 变量名 默认值
ask() {
  local prompt="$1" var="$2" default="$3"
  if [ -n "${!var:-}" ]; then
    return 0
  fi
  local input
  printf "${CYAN}[设置]${RESET} %s" "$prompt"
  read -r input || true
  input="$(printf '%s' "$input" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
  if [ -n "$input" ]; then
    printf -v "$var" '%s' "$input"
  elif [ -n "$default" ]; then
    printf -v "$var" '%s' "$default"
  fi
}

# ask_password 询问管理员密码（不回显）；留空则保持为空（稍后自动生成）。
ask_password() {
  local var="$1"
  if [ -n "${!var:-}" ]; then
    return 0
  fi
  local input
  printf "${CYAN}[设置]${RESET} %s" "初始管理员密码（回车=自动生成强密码）: "
  read -rs input || true
  echo ""
  if [ -n "$input" ]; then
    printf -v "$var" '%s' "$input"
  fi
}

# 检测安装状态：install 目录存在 config.yml 视为「更新」，否则为「全新部署」
detect_install_state() {
  INSTALL_STATE="fresh"
  FRESH_INSTALL=1
  if [ -f "$INSTALL_DIR/config.yml" ]; then
    INSTALL_STATE="update"
    FRESH_INSTALL=0
  fi
}

interactive_prompt() {
  [ "$NON_INTERACTIVE" -eq 1 ] && return 0
  # 非 TTY（如管道/定时任务）时跳过交互，避免卡在 read 等待
  [ -t 0 ] || return 0

  echo ""
  if [ "$INSTALL_STATE" = "update" ]; then
    info "==================== 更新模式 ===================="
    info "检测到已安装（$INSTALL_DIR/config.yml 存在），本次将作为【更新】执行。"
    info "  • 拉取最新源码 → 重新构建前后端 → 重启服务"
    info "  • 数据库 / config.yml / 上传文件 / 管理员密码 / 测活设置 全部保留，无需重新配置"
    echo ""
    ask "站点域名（回车=保持现状，不修改 Nginx）: " DOMAIN ""
    return 0
  fi

  info "==================== 部署配置向导（全新安装） ===================="
  info "（直接回车使用默认值；留空的项稍后自动生成）"
  echo ""

  ask "站点域名（如 shop.example.com，回车=跳过 Nginx/HTTPS）: " DOMAIN ""
  ask "后台路径（回车=随机生成，如 /dj-xxxxx）: " ADMIN_PATH ""
  ask "初始管理员用户名（默认 admin）: " DJ_ADMIN_USER "admin"
  ask_password DJ_ADMIN_PASS
  echo ""
}

# 部署前确认
confirm_deploy() {
  [ "$NON_INTERACTIVE" -eq 1 ] && return 0
  [ -t 0 ] || return 0

  echo ""
  if [ "$INSTALL_STATE" = "update" ]; then
    info "==================== 更新确认 ===================="
    echo "  源码目录:   $SRC_DIR"
    echo "  安装目录:   $INSTALL_DIR"
    echo "  本次操作:   拉取最新代码 → 重新构建 → 重启服务"
    echo "  数据保留:   数据库 / config.yml / 上传文件 / 管理员密码 / 测活设置"
    echo ""
    local confirm
    read -r -p "  确认更新到最新版本？[Y/n] " confirm || true
    case "${confirm:-Y}" in
      Y|y|yes|YES) ;;
      *) die "已取消更新，未做任何修改" ;;
    esac
    echo ""
    return 0
  fi

  info "==================== 部署配置预览 ===================="
  echo "  源码目录:   $SRC_DIR"
  echo "  安装目录:   $INSTALL_DIR"
  echo "  服务端口:   $SERVER_PORT"
  echo "  站点域名:   ${DOMAIN:-(不配置 Nginx/HTTPS)}"
  echo "  后台路径:   ${ADMIN_PATH:-(随机生成)}"
  echo "  管理员:     ${DJ_ADMIN_USER:-admin}"
  echo "  管理员密码: ${DJ_ADMIN_PASS:-(自动生成)}"
  echo ""
  local confirm
  read -r -p "  确认开始部署？[Y/n] " confirm || true
  case "${confirm:-Y}" in
    Y|y|yes|YES) ;;
    *) die "已取消部署" ;;
  esac
  echo ""
}

# ------------------------------ 基础检查 ----------------------------------
[ "$(id -u)" -eq 0 ] || die "请使用 root 运行：sudo bash $0"

OS_ID="$(. /etc/os-release && echo "$ID")"
[ "$OS_ID" = "ubuntu" ] || warn "脚本面向 Ubuntu，当前系统为 $OS_ID，继续执行可能存在问题"

log "========== Dujiao-Next 自动测活版 一键部署 =========="

# ------------------------------ 1. 安装基础依赖 ---------------------------
install_base() {
  log "安装系统依赖..."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y
  apt-get install -y curl wget git unzip build-essential openssl \
    nginx redis-server ca-certificates apt-transport-https
}

# ------------------------------ 2. 安装 Go ---------------------------------
install_go() {
  if command -v go >/dev/null 2>&1; then
    GO_VERSION="$(go version | sed -n 's/.*go\([0-9]\+\.[0-9]\+\).*/\1/p')"
    if awk -v v="$GO_VERSION" 'BEGIN{exit !(v >= 1.26)}'; then
      log "Go 已安装：$(go version)"
      return
    fi
    warn "Go 版本过低（$GO_VERSION），将升级到 1.26.5"
  fi
  log "安装 Go 1.26.5..."
  wget -q https://go.dev/dl/go1.26.5.linux-amd64.tar.gz -O /tmp/go.tar.gz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tar.gz
  rm -f /tmp/go.tar.gz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
  log "Go 安装完成：$(go version)"
}

# ------------------------------ 3. 安装 Node 与 pnpm -----------------------
install_node() {
  if command -v node >/dev/null 2>&1 && node -v | grep -qE '^v(2[2-9]|3[0-9])'; then
    log "Node 已安装：$(node -v)"
  else
    log "安装 Node.js 24..."
    curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
    apt-get install -y nodejs
  fi
  if command -v pnpm >/dev/null 2>&1; then
    log "pnpm 已安装：$(pnpm -v)"
  else
    log "安装 pnpm 10..."
    export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
    corepack enable || true
    corepack prepare pnpm@10.34.3 --activate 2>/dev/null || npm install -g pnpm@10.34.3
  fi
  log "Node：$(node -v)，pnpm：$(pnpm -v)"
}

# ------------------------------ 4. 启动 Redis ------------------------------
start_redis() {
  if systemctl is-active --quiet redis-server 2>/dev/null; then
    log "Redis 运行中"
  else
    log "启动 Redis..."
    systemctl enable --now redis-server 2>/dev/null || service redis-server start || true
  fi
  redis-cli ping >/dev/null 2>&1 && log "Redis 正常（PONG）" || warn "Redis 未响应，请检查"
}

# ------------------------------ 5. 获取源码 --------------------------------
fetch_source() {
  if [ -d "$SRC_DIR/.git" ]; then
    log "更新源码（$SRC_DIR）..."
    cd "$SRC_DIR"
    git fetch origin "$REPO_BRANCH"
    git reset --hard FETCH_HEAD
  else
    log "克隆源码..."
    mkdir -p "$(dirname "$SRC_DIR")"
    git clone --branch "$REPO_BRANCH" --single-branch "$REPO_URL" "$SRC_DIR"
    cd "$SRC_DIR"
  fi
}

# ------------------------------ 6. 构建 ------------------------------------
build_binary() {
  log "安装前端依赖并构建（admin + user），可能需要数分钟..."
  cd "$SRC_DIR"

  ( cd frontend/admin && pnpm install --frozen-lockfile && pnpm run build:fullstack )
  ( cd frontend/user  && pnpm install --frozen-lockfile && pnpm run build )

  rm -rf internal/web/dist
  mkdir -p internal/web/dist
  cp -r frontend/admin/dist internal/web/dist/admin
  cp -r frontend/user/dist  internal/web/dist/user

  log "编译 fullstack 二进制..."
  CGO_ENABLED=0 go build -trimpath -tags release,fullstack \
    -ldflags="-s -w -X github.com/dujiao-next/internal/version.Version=v1.0.0 -X github.com/dujiao-next/internal/version.BuildType=release" \
    -o dujiao-next ./cmd/server
  log "构建完成：$SRC_DIR/dujiao-next"
}

# ------------------------------ 7. 安装目录与配置 --------------------------
gen_secret() { openssl rand -hex 32; }

gen_password() {
  local p
  p="$(openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 16)"
  echo "${p}A1b"   # 强制含大写/小写/数字，且长度足够
}

gen_admin_path() { openssl rand -hex 5 | sed 's/^/\/dj-/'; }

setup_install_dir() {
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    log "运行用户 $SERVICE_USER 已存在"
  else
    log "创建运行用户 $SERVICE_USER..."
    useradd -r -s /usr/sbin/nologin "$SERVICE_USER"
  fi

  mkdir -p "$INSTALL_DIR"
  cp -f "$SRC_DIR/dujiao-next" "$INSTALL_DIR/dujiao-next"
  chmod 755 "$INSTALL_DIR/dujiao-next"

  if [ "$INSTALL_STATE" = "update" ]; then
    warn "检测到已有安装，跳过生成 config.yml（数据与配置均保留，仅更新程序）"
  else
    log "生成 config.yml 与强随机密钥..."
    APP_SECRET="$(gen_secret)"
    JWT_SECRET="$(gen_secret)"
    USER_JWT_SECRET="$(gen_secret)"
    [ -n "$ADMIN_PATH" ] || ADMIN_PATH="$(gen_admin_path)"

    cat > "$INSTALL_DIR/config.yml" <<EOF
app:
  secret_key: $APP_SECRET
  totp_issuer: Dujiao-Next

server:
  host: 0.0.0.0
  port: $SERVER_PORT
  mode: release

log:
  dir: "$INSTALL_DIR/logs"

database:
  driver: sqlite
  dsn: "$INSTALL_DIR/db/dujiao.db"

jwt:
  secret: $JWT_SECRET

user_jwt:
  secret: $USER_JWT_SECRET

bootstrap:
  default_admin_username: ""
  default_admin_password: ""

web:
  admin_path: "$ADMIN_PATH"

redis:
  enabled: true
  host: 127.0.0.1
  port: 6379

queue:
  enabled: true
  host: 127.0.0.1
  port: 6379

reseller:
  enabled: $RESELLER_ENABLED
  main_hosts:
    - localhost
    - 127.0.0.1
    - "::1"
  trusted_forwarded_host: false
  subdomain_base: ""
  sub_sites_enabled: $RESELLER_SUB_SITES_ENABLED
  self_apply_enabled: true
  settlement_confirm_days: 7
EOF
    chmod 600 "$INSTALL_DIR/config.yml"
  fi

  if [ -z "$ADMIN_PATH" ]; then
    ADMIN_PATH="$(sed -n 's/^[[:space:]]*admin_path:[[:space:]]*"\([^"]*\)"/\1/p' "$INSTALL_DIR/config.yml")"
  fi

  # 管理员密码：仅在全新安装时自动生成；二次部署保留原有密码与数据
  if [ "$FRESH_INSTALL" = "1" ] && [ -z "$DJ_ADMIN_PASS" ]; then
    DJ_ADMIN_PASS="$(gen_password)"
    GENERATED_PASS=1
  else
    GENERATED_PASS=0
  fi

  chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
}

# ------------------------------ 8. systemd 服务 ----------------------------
setup_service() {
  cat > /etc/systemd/system/dujiao.service <<EOF
[Unit]
Description=Dujiao-Next
After=network.target redis-server.service
Wants=redis-server.service

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/dujiao-next
Restart=on-failure
RestartSec=3
$([ "$FRESH_INSTALL" = "1" ] && printf 'Environment=DJ_DEFAULT_ADMIN_USERNAME=%s\nEnvironment=DJ_DEFAULT_ADMIN_PASSWORD=%s' "${DJ_ADMIN_USER:-admin}" "$DJ_ADMIN_PASS")

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable dujiao
  systemctl restart dujiao
  log "systemd 服务已启动"
}

# ------------------------------ 9. 健康检查 --------------------------------
wait_health() {
  local tries=30
  log "等待服务就绪..."
  for ((i=1; i<=tries; i++)); do
    if curl -fsS "http://127.0.0.1:$SERVER_PORT/health" >/dev/null 2>&1; then
      log "服务健康检查通过"
      return 0
    fi
    sleep 2
  done
  warn "服务未在预期时间内就绪，请查看日志：journalctl -u dujiao -n 100"
  return 1
}

# ------------------------------ 10. Nginx + HTTPS --------------------------
setup_nginx() {
  [ -n "$DOMAIN" ] || { info "未设置 DOMAIN，跳过 Nginx/HTTPS 配置"; return 0; }

  log "配置 Nginx 反向代理（$DOMAIN -> 127.0.0.1:$SERVER_PORT）..."
  cat > /etc/nginx/sites-available/dujiao <<EOF
server {
    listen 80;
    server_name $DOMAIN;

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:$SERVER_PORT;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 300s;
    }
}
EOF
  ln -sf /etc/nginx/sites-available/dujiao /etc/nginx/sites-enabled/dujiao
  nginx -t && systemctl enable nginx && systemctl reload nginx

  if command -v certbot >/dev/null 2>&1; then
    log "申请 HTTPS 证书（Let's Encrypt）..."
    certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos --redirect -m "admin@$DOMAIN" || \
      warn "certbot 申请失败，可稍后手动执行：certbot --nginx -d $DOMAIN"
  else
    warn "未安装 certbot，HTTPS 请稍后手动配置：certbot --nginx -d $DOMAIN"
  fi
}

# ------------------------------ 11. 结果汇总 -------------------------------
print_summary() {
  local base_url="http://$DOMAIN"
  [ -n "$DOMAIN" ] || base_url="http://$(hostname -I | awk '{print $1}'):$SERVER_PORT"

  echo ""
  echo "================================================================"
  echo " 部署完成！"
  echo "---------------------------------------------------------------"
  echo "  前台地址： $base_url/"
  echo "  后台地址： $base_url$ADMIN_PATH"
  echo "  管理员：   ${DJ_ADMIN_USER:-admin}"
  if [ "$GENERATED_PASS" = "1" ]; then
    echo "  初始密码： $DJ_ADMIN_PASS   （请立即登录后修改！）"
    echo "  密码已同时写入 $INSTALL_DIR/admin_credentials.txt"
    cat > "$INSTALL_DIR/admin_credentials.txt" <<EOF
管理员用户名：${DJ_ADMIN_USER:-admin}
管理员密码：$DJ_ADMIN_PASS
后台路径：$ADMIN_PATH
EOF
    chmod 600 "$INSTALL_DIR/admin_credentials.txt"
    chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR/admin_credentials.txt"
  else
    echo "  初始密码： （沿用原密码，未改动）"
    echo "  数据已保留：商品 / 订单 / 卡密 / 站点设置（含测活配置）均未丢失"
  fi
  echo "---------------------------------------------------------------"
  echo " 服务管理："
  echo "   systemctl status dujiao        # 查看状态"
  echo "   journalctl -u dujiao -f        # 实时日志"
  echo "   tail -f $INSTALL_DIR/logs/app.log"
  echo "---------------------------------------------------------------"
  echo " 配置自动测活：后台「设置 → 测活设置」页面填入卡密/接口即可；"
  echo " 或执行以下命令（将 <TOKEN> 替换为登录返回的 token）："
  echo ""
  echo "  TOKEN=\$(curl -s -X POST $base_url/api/v1/admin/login \\"
  echo "    -H 'Content-Type: application/json' \\"
  echo "    -d '{\"username\":\"${DJ_ADMIN_USER:-admin}\",\"password\":\"$DJ_ADMIN_PASS\"}' \\"
  echo "    | sed -n 's/.*\"token\":\"\\([^\"]*\\)\".*/\\1/p')"
  echo ""
  echo "  curl -s -X PUT $base_url/api/v1/admin/settings \\"
  echo "    -H \"Authorization: Bearer \$TOKEN\" -H 'Content-Type: application/json' \\"
  echo "    -d '{\"key\":\"card_check_config\",\"value\":{\"enabled\":true,\"kami\":\"你的CheckDx卡密\",\"interface\":\"post5（1.0pt）【✅ Open|开放中】_global\",\"country\":\"美国\",\"buffer\":5,\"timeout_seconds\":60,\"poll_interval_millis\":2000}}'"
  echo ""
  echo "  然后对自动发货商品开启测活："
  echo "  curl -s -X PATCH $base_url/api/v1/admin/products/商品ID \\"
  echo "    -H \"Authorization: Bearer \$TOKEN\" -H 'Content-Type: application/json' \\"
  echo "    -d '{\"card_check_enabled\":true}'"
  echo ""
  echo " 也可在后台商品列表直接点击『测活』徽章切换；编辑商品可设置『测活价格』。"
  echo " 用户端：商品页勾选『开启测活』后按测活价格购买，未勾选按原价直接发货。"
  echo "---------------------------------------------------------------"
  echo " 再次部署 / 更新："
  echo "  sudo bash $SRC_DIR/deploy_ubuntu.sh"
  echo "  （或：sudo bash -c \"\$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/$REPO_BRANCH/deploy_ubuntu.sh)\"）"
  echo " 详细说明见仓库内 DEPLOY_UBUNTU.md 第九章"
  echo "================================================================"
}

# ================================ 主流程 ==================================
detect_install_state
interactive_prompt
confirm_deploy
install_base
install_go
install_node
start_redis
fetch_source
build_binary
setup_install_dir
setup_service
wait_health || true
setup_nginx
print_summary
