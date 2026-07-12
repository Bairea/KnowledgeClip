#!/bin/bash
# 检查开发环境与打包 exe 的资源差异

echo "=== 检查 embed 资源 ==="

# 1. 检查 embed 文件是否都存在
echo "1. Embed 文件检查:"
echo "  - internal/api/static/ (前端静态文件):"
ls -la internal/api/static/ 2>&1 | head -3
ls -la internal/api/static/assets/ 2>&1 | head -5

echo "  - internal/systrayapp/icon.ico (系统托盘图标):"
ls -la internal/systrayapp/icon.ico 2>&1

echo "  - cmd/server/default_sites.yaml (默认站点配置):"
ls -la cmd/server/default_sites.yaml 2>&1

# 2. 检查 embed 声明
echo ""
echo "2. Embed 声明检查:"
grep -rn "//go:embed" --include="*.go" 2>&1 | grep -v ".git"

# 3. 检查相对路径引用
echo ""
echo "3. 相对路径引用检查（可能遗漏 embed 的文件）:"
grep -rn 'os\.ReadFile|os\.Open|ioutil\.ReadFile|filepath\.Join.*"configs"|filepath\.Join.*"data"' --include="*.go" 2>&1 | grep -v ".git" | grep -v "_test.go"

# 4. 检查 dist 目录残留
echo ""
echo "4. Dist 目录检查:"
if [ -d "dist/configs" ]; then
    echo "  WARNING: dist/configs 存在残留配置"
    cat dist/configs/sites.yaml 2>&1 | head -5
fi
if [ -d "dist/data" ]; then
    echo "  WARNING: dist/data 存在残留数据"
    ls -la dist/data/ 2>&1
fi

# 5. 检查静态资源 hash 是否匹配
echo ""
echo "5. 静态资源 hash 检查:"
html_asset=$(grep -oE 'index-[A-Za-z0-9_-]+\.js' internal/api/static/index.html 2>&1)
actual_asset=$(ls internal/api/static/assets/index-*.js 2>&1 | head -1 | xargs basename)
if [ "$html_asset" = "$actual_asset" ]; then
    echo "  OK: HTML 引用的 JS ($html_asset) 与实际文件 ($actual_asset) 匹配"
else
    echo "  ERROR: HTML 引用 $html_asset，实际文件 $actual_asset"
fi

echo ""
echo "=== 检查完成 ==="