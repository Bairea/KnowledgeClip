@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion

echo ============================================
echo   KnowledgeClip - browser-act 安装脚本
echo ============================================
echo.
echo 基于官方文档: https://github.com/browser-act/skills/blob/main/docs/installation.md
echo.

:: 1. 检测 Python 3.12+
echo [1/4] 检测 Python 环境...
where python >nul 2>&1
if %errorlevel% neq 0 (
    echo   [错误] 未检测到 Python
    echo   请安装 Python 3.12+: https://www.python.org/downloads/
    echo   安装时勾选 "Add Python to PATH"
    pause
    exit /b 1
)

for /f "tokens=*" %%i in ('python -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')'"') do set PY_VERSION=%%i
echo   Python 版本: %PY_VERSION%

:: 简单检查版本是否 >= 3.12
for /f "tokens=1,2 delims=." %%a in ("%PY_VERSION%") do (
    set PY_MAJOR=%%a
    set PY_MINOR=%%b
)
if %PY_MAJOR% LSS 3 (
    echo   [错误] 需要 Python 3.12+，当前版本 %PY_VERSION%
    pause
    exit /b 1
)
if %PY_MAJOR% EQU 3 if %PY_MINOR% LSS 12 (
    echo   [错误] 需要 Python 3.12+，当前版本 %PY_VERSION%
    pause
    exit /b 1
)

:: 2. 检测/安装 uv
echo.
echo [2/4] 检测 uv 包管理器...
where uv >nul 2>&1
if %errorlevel% neq 0 (
    echo   正在安装 uv...
    powershell -c "irm https://astral.sh/uv/install.ps1 | iex"
    if !errorlevel! neq 0 (
        echo   [警告] uv 安装失败
        echo   请手动安装 uv: https://astral.sh/uv
        echo   然后运行: uv tool install browser-act-cli --python 3.12
        pause
        exit /b 1
    )
    :: 刷新 PATH
    for /f "tokens=*" %%i in ('powershell -c "[Environment]::GetEnvironmentVariable('Path', 'User') + ';' + [Environment]::GetEnvironmentVariable('Path', 'Machine')"') do set PATH=%%i
)
echo   uv 已就绪

:: 3. 安装 browser-act-cli
echo.
echo [3/4] 安装 browser-act-cli...
where browser-act >nul 2>&1
if %errorlevel% equ 0 (
    echo   browser-act 已安装
    for /f "tokens=*" %%i in ('browser-act --version') do echo   %%i
    echo   如需升级: uv tool upgrade browser-act-cli
    goto :verify
)

echo   正在安装 browser-act-cli...
echo   ^(这可能需要几分钟，请耐心等待^)
echo.
uv tool install browser-act-cli --python 3.12
if %errorlevel% neq 0 (
    echo.
    echo   [错误] 安装失败。请检查网络连接后重试。
    echo.
    echo   手动安装命令:
    echo     uv tool install browser-act-cli --python 3.12
    echo.
    pause
    exit /b 1
)

:: 4. 验证
:verify
echo.
echo [4/4] 验证安装...
where browser-act >nul 2>&1
if %errorlevel% equ 0 (
    echo   [✓] browser-act 安装成功！
    for /f "tokens=*" %%i in ('browser-act --version') do echo   %%i
    echo.
    echo   现在可以启动 KnowledgeClip 了。
    echo   首次使用时，请在各站点登录一次。
) else (
    echo   [警告] 安装完成但未检测到 browser-act 命令。
    echo   请关闭并重新打开命令提示符，然后重试。
    echo.
    echo   手动安装命令:
    echo     uv tool install browser-act-cli --python 3.12
)

echo.
pause
