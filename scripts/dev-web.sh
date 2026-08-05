#!/usr/bin/env bash

set -Eeuo pipefail

repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
backend_pid=""
frontend_pid=""

cleanup() {
    local exit_code=$?
    trap - EXIT INT TERM

    if [[ -n "${frontend_pid}" ]]; then
        kill "${frontend_pid}" 2>/dev/null || true
    fi
    if [[ -n "${backend_pid}" ]]; then
        kill "${backend_pid}" 2>/dev/null || true
    fi

    if [[ -n "${frontend_pid}" ]]; then
        wait "${frontend_pid}" 2>/dev/null || true
    fi
    if [[ -n "${backend_pid}" ]]; then
        wait "${backend_pid}" 2>/dev/null || true
    fi

    exit "${exit_code}"
}

handle_signal() {
    exit 130
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "缺少命令：$1" >&2
        exit 1
    fi
}

wait_for_url() {
    local service_name="$1"
    local url="$2"
    local pid="$3"
    local attempt

    for ((attempt = 1; attempt <= 120; attempt++)); do
        if curl --silent --fail "${url}" >/dev/null; then
            return 0
        fi
        if ! kill -0 "${pid}" 2>/dev/null; then
            echo "${service_name} 启动失败，请查看上方日志。" >&2
            return 1
        fi
        sleep 0.25
    done

    echo "等待 ${service_name} 启动超时：${url}" >&2
    return 1
}

trap cleanup EXIT
trap handle_signal INT TERM

require_command go
require_command node
require_command npm
require_command curl

cd "${repo_dir}"

if [[ ! -d frontend/node_modules ]]; then
    echo "首次启动：正在安装前端依赖..."
    npm --prefix frontend ci
fi

if [[ ! -f frontend/dist/index.html ]]; then
    echo "首次启动：正在生成 Go Web 编译所需的 frontend/dist..."
    NODE_OPTIONS=--max-old-space-size=4096 npm --prefix frontend run build
fi

echo "正在启动 Go Web 后端：http://127.0.0.1:18888"
GO_STOCK_WEB_ADDR=127.0.0.1:18888 go run -tags web . &
backend_pid=$!
wait_for_url "Go Web 后端" "http://127.0.0.1:18888/api/health" "${backend_pid}"

echo "正在启动 Vite 前端..."
npm --prefix frontend run dev &
frontend_pid=$!
wait_for_url "Vite 前端" "http://127.0.0.1:5173" "${frontend_pid}"

echo
echo "Web 开发环境已启动：http://127.0.0.1:5173"
echo "修改 frontend/src 下的代码会自动热更新；按 Ctrl+C 停止前后端。"

while kill -0 "${backend_pid}" 2>/dev/null && kill -0 "${frontend_pid}" 2>/dev/null; do
    sleep 1
done

if ! kill -0 "${backend_pid}" 2>/dev/null; then
    echo "Go Web 后端已退出。" >&2
else
    echo "Vite 前端已退出。" >&2
fi

exit 1
