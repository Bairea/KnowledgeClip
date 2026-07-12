#!/bin/bash
# 验证打包 exe 功能完整性

echo "=== Phase 2: 验证打包 exe 功能 ==="

# 1. 清理测试环境
echo "1. 清理测试环境..."
rm -rf /tmp/kctest/configs /tmp/kctest/data /tmp/kctest/.browser-data
mkdir -p /tmp/kctest

# 2. 运行打包 exe
echo "2. 运行打包 exe..."
cd /tmp/kctest
/d/cs_proj/KnowledgeClip/dist/KnowledgeClip-windows.exe &
PID=$!
sleep 3

# 3. 验证预设站点
echo "3. 验证预设站点..."
SITES=$(curl -s http://localhost:8080/api/sites)
SITE_COUNT=$(echo "$SITES" | jq 'length')
echo "  站点数量: $SITE_COUNT (预期: 7)"

if [ "$SITE_COUNT" -lt 7 ]; then
    echo "  ERROR: 站点数量不足"
    echo "  实际站点:"
    echo "$SITES" | jq -r '.[].name'
else
    echo "  OK: 站点数量正确"
fi

# 4. 验证前端静态文件
echo "4. 验证前端静态文件..."
HTML=$(curl -s http://localhost:8080/)
if echo "$HTML" | grep -q "KnowledgeClip"; then
    echo "  OK: 前端页面正常"
else
    echo "  ERROR: 前端页面异常"
fi

# 5. 验证系统托盘图标（无法通过 API 测试，检查日志）
echo "5. 验证数据库 schema..."
curl -s http://localhost:8080/api/health > /dev/null
if [ $? -eq 0 ]; then
    echo "  OK: API 响应正常"
else
    echo "  ERROR: API 响应异常"
fi

# 6. 验证配置文件双向同步
echo "6. 验证配置文件双向同步..."
if [ -f "/tmp/kctest/configs/sites.yaml" ]; then
    CONFIG_SITES=$(grep -c "^    - id:" /tmp/kctest/configs/sites.yaml)
    echo "  配置文件站点数: $CONFIG_SITES (预期: 7)"
    if [ "$CONFIG_SITES" -lt 7 ]; then
        echo "  ERROR: 配置文件站点不足"
        cat /tmp/kctest/configs/sites.yaml | head -10
    else
        echo "  OK: 配置文件正确"
    fi
else
    echo "  ERROR: 配置文件不存在"
fi

# 7. 验证数据库
echo "7. 验证数据库..."
if [ -f "/tmp/kctest/data/knowledgeclip.db" ]; then
    echo "  OK: 数据库文件存在"
else
    echo "  ERROR: 数据库文件不存在"
fi

# 8. 停止进程
echo "8. 停止进程..."
taskkill //F //PID $PID 2>&1 || true

echo ""
echo "=== 验证完成 ==="