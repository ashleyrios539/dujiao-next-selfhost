#!/usr/bin/env bash
# ============================================================================
#  Dujiao-Next 简易自部署 —— 初始化脚本
#
#  首次部署时运行一次：生成 .env（随机密钥 / 随机后台路径 / 强管理员密码）。
#  已存在 .env 时直接跳过，不会覆盖现有配置。
#
#  用法：
#    ./setup.sh
#    docker compose up -d
#
#  可选：生成前用环境变量覆盖：
#    DJ_ADMIN_USER=owner DJ_ADMIN_PASS='MyStr0ngPw!' ADMIN_PATH=/dj-my-secret ./setup.sh
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

if [ -f .env ]; then
  echo "已存在 .env（保留现有配置）。如需重新生成：rm .env && ./setup.sh"
  exit 0
fi

[ -f .env.example ] || { echo "错误：缺少 .env.example" >&2; exit 1; }

gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

gen_password() {
  local p
  if command -v openssl >/dev/null 2>&1; then
    p="$(openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 16)"
  else
    p="$(od -An -N32 -tx1 /dev/urandom | tr -dc '0-9' | head -c 16)"
  fi
  # 追加 A1b，保证同时含大写/小写/数字，并满足密码策略的最小长度要求
  echo "${p}A1b"
}

gen_admin_path() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 5 | sed 's/^/\/dj-/'
  else
    echo "/dj-$(od -An -N5 -tx1 /dev/urandom | tr -d ' \n')"
  fi
}

APP_SECRET_KEY="${APP_SECRET_KEY:-$(gen_secret)}"
JWT_SECRET="${JWT_SECRET:-$(gen_secret)}"
USER_JWT_SECRET="${USER_JWT_SECRET:-$(gen_secret)}"
DJ_ADMIN_USER="${DJ_ADMIN_USER:-admin}"
DJ_ADMIN_PASS="${DJ_ADMIN_PASS:-$(gen_password)}"
ADMIN_PATH="${ADMIN_PATH:-$(gen_admin_path)}"

# .env 是单行 KEY=VALUE 格式，值里不允许出现换行（会破坏 env_file 解析）
for _pair in \
  "APP_SECRET_KEY|$APP_SECRET_KEY" \
  "JWT_SECRET|$JWT_SECRET" \
  "USER_JWT_SECRET|$USER_JWT_SECRET" \
  "DJ_DEFAULT_ADMIN_USERNAME|$DJ_ADMIN_USER" \
  "DJ_DEFAULT_ADMIN_PASSWORD|$DJ_ADMIN_PASS" \
  "WEB_ADMIN_PATH|$ADMIN_PATH"; do
  _key="${_pair%%|*}"
  _val="${_pair#*|}"
  if [ "$(printf '%s' "$_val" | wc -l)" -gt 1 ]; then
    echo "错误：$_key 的值不能包含换行符" >&2
    exit 1
  fi
done

# 逐行复制 .env.example 并替换 6 个密钥/路径/账号行。
# 用 printf '%s' 原样写入，避免 sed 对 & / # / \ 等字符的二次解释。
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    APP_SECRET_KEY=*)          printf 'APP_SECRET_KEY=%s\n'          "$APP_SECRET_KEY" ;;
    JWT_SECRET=*)              printf 'JWT_SECRET=%s\n'              "$JWT_SECRET" ;;
    USER_JWT_SECRET=*)         printf 'USER_JWT_SECRET=%s\n'         "$USER_JWT_SECRET" ;;
    DJ_DEFAULT_ADMIN_USERNAME=*) printf 'DJ_DEFAULT_ADMIN_USERNAME=%s\n' "$DJ_ADMIN_USER" ;;
    DJ_DEFAULT_ADMIN_PASSWORD=*) printf 'DJ_DEFAULT_ADMIN_PASSWORD=%s\n' "$DJ_ADMIN_PASS" ;;
    WEB_ADMIN_PATH=*)          printf 'WEB_ADMIN_PATH=%s\n'          "$ADMIN_PATH" ;;
    *)                         printf '%s\n' "$line" ;;
  esac
done < .env.example > .env
chmod 600 .env

echo
echo "✔ 已生成 .env（权限 600）"
echo
echo "  商城地址：  http://<服务器IP>:${APP_PORT:-8080}"
echo "  后台地址：  http://<服务器IP>:${APP_PORT:-8080}${ADMIN_PATH}"
echo "  管理员账号：${DJ_ADMIN_USER}"
echo "  管理员密码：${DJ_ADMIN_PASS}   ← 仅显示这一次，请立即保存"
echo
echo "下一步：docker compose up -d"
echo "首次启动需构建镜像（含前端编译），约 5~15 分钟"
echo "查看运行日志（release 模式写文件）：docker compose exec app tail -f /app/logs/app.log"
