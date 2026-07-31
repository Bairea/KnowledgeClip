@echo off
echo Building KnowledgeClip...
echo.

:: 清理旧产物（防止混淆）
if exist bin\server.exe del bin\server.exe

:: Build frontend
cd web
call npm run build
cd ..

:: Build Go binary as Windows GUI app (no terminal window)
:: -s: strip symbol table
:: -w: strip DWARF debug info
:: -H=windowsgui: build as Windows GUI app (no console window)
go build -ldflags "-s -w -H=windowsgui" -o bin\KnowledgeClip.exe ./cmd/server/

echo.
echo Build complete: bin\KnowledgeClip.exe
echo.
echo Usage:
echo   Just double-click bin\KnowledgeClip.exe
echo   Then open http://localhost:8080 in your browser
pause
