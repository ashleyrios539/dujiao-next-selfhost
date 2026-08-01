#!/usr/bin/env bash
# ============================================================================
#  Dujiao-Next（含自动测活）远程一键部署/更新入口
#
#  全新服务器安装 / 已有服务器更新，都用这一条命令（脚本自动识别）：
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/install.sh)"
#
#  说明：本脚本拉取并执行 deploy_ubuntu.sh（交互式向导）：
#  • 首次运行 = 全新部署（询问：站点域名 / 后台路径 / 管理员账号 / 管理员密码）
#  • 再次运行 = 更新模式（先确认，数据库/配置/密码/测活设置全部保留）
#
#  非交互（不提问、全部用默认值/环境变量）直接用：
#    sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/main/deploy_ubuntu.sh)" deploy_ubuntu.sh -y
# ============================================================================
set -euo pipefail

REPO_BRANCH="${REPO_BRANCH:-main}"
DEPLOY_URL="https://raw.githubusercontent.com/ashleyrios539/dujiaodxcheck_xhenmo01/${REPO_BRANCH}/deploy_ubuntu.sh"

command -v curl >/dev/null 2>&1 || { echo "缺少 curl，请先安装：apt-get install -y curl" >&2; exit 1; }

echo "==> 拉取部署脚本：$DEPLOY_URL"
exec sudo bash -c "$(curl -fsSL "$DEPLOY_URL")" deploy_ubuntu.sh
