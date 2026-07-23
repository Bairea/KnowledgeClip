#!/usr/bin/env bash
# KnowledgeClip - browser-act 安装脚本 (macOS/Linux)
# 基于官方文档: https://github.com/browser-act/skills/blob/main/docs/installation.md

set -euo pipefail

echo "============================================"
echo "  KnowledgeClip - browser-act 安装脚本"
echo "============================================"
echo ""

# 1. 检测 Python 3.12+
echo "[1/4] 检测 Python 环境..."
if command -v python3 &>/dev/null; then
    PY_VERSION=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
    echo "  Python 版本: $PY_VERSION"
    MAJOR=$(echo "$PY_VERSION" | cut -d. -f1)
    MINOR=$(echo "$PY_VERSION" | cut -d. -f2)
    if [ "$MAJOR" -lt 3 ] || ([ "$MAJOR" -eq 3 ] && [ "$MINOR" -lt 12 ]); then
        echo "  [错误] 需要 Python 3.12+，当前版本 $PY_VERSION"
        echo "  请安装 Python 3.12+: https://www.python.org/downloads/"
        exit 1
    fi
else
    echo "  [错误] 未检测到 Python3"
    echo "  请安装 Python 3.12+: https://www.python.org/downloads/"
    exit 1
fi

# 2. 检测/安装 uv
echo ""
echo "[2/4] 检测 uv 包管理器..."
if ! command -v uv &>/dev/null; then
    echo "  正在安装 uv..."
    curl -LsSf https://astral.sh/uv/install.sh | sh
    # 刷新 PATH
    export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
    if ! command -v uv &>/dev/null; then
        echo "  [警告] uv 安装完成但不在 PATH 中"
        echo "  请运行: source ~/.bashrc 或重启终端"
        echo "  然后手动运行: uv tool install browser-act-cli --python 3.12"
        exit 0
    fi
fi
echo "  uv 已就绪: $(uv --version)"

# 3. 安装 browser-act-cli
echo ""
echo "[3/4] 安装 browser-act-cli..."
if command -v browser-act &>/dev/null; then
    echo "  browser-act 已安装: $(browser-act --version)"
    echo "  如需升级: uv tool upgrade browser-act-cli"
else
    uv tool install browser-act-cli --python 3.12
    echo "  安装完成"
fi

# 4. 验证
echo ""
echo "[4/4] 验证安装..."
if command -v browser-act &>/dev/null; then
    echo "  [✓] browser-act 安装成功！"
    browser-act --version
    echo ""
    echo "  现在可以启动 KnowledgeClip 了。"
    echo "  首次使用时，请在各站点登录一次。"
else
    # 尝试从 uv tool dir 运行
    UV_TOOL_DIR=$(uv tool dir 2>/dev/null || echo "$HOME/.local/share/uv/tools")
    if [ -f "$UV_TOOL_DIR/browser-act-cli/bin/browser-act" ]; then
        echo "  [✓] browser-act 已安装但不在 PATH"
        echo "  请添加以下路径到 PATH:"
        echo "    export PATH=\"$UV_TOOL_DIR/browser-act-cli/bin:\$PATH\""
    else
        echo "  [警告] 安装可能未完成，请手动运行:"
        echo "    uv tool install browser-act-cli --python 3.12"
    fi
fi
