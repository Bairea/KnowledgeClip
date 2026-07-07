$path = 'internal/engine/rod_engine.go'
$c = Get-Content -Path $path -Raw
Set-Content -Path $path -Value $c -Encoding UTF8 -NoNewline
