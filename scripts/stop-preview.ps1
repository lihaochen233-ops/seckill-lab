# 只停止本项目由助手启动并记录的后台预览，不匹配或未记录的进程不会被停止。
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$recordPath = Join-Path $projectRoot '.local/preview.json'
if (-not (Test-Path -LiteralPath $recordPath)) { Write-Host 'No managed preview. Stop your terminal/debug session with Ctrl+C.'; exit 0 }
$record = Get-Content -LiteralPath $recordPath -Raw | ConvertFrom-Json
$process = Get-Process -Id $record.processId -ErrorAction SilentlyContinue
if (-not $process) { Write-Host 'Preview has already stopped.'; exit 0 }
$expected = [IO.Path]::GetFullPath((Join-Path $projectRoot '.local/mall-latest.exe'))
if ($process.Path -ne $expected -or $process.StartTime.ToUniversalTime().ToString('o') -ne $record.startedAt) { throw 'Process identity changed; refusing to stop an unrelated process.' }
Stop-Process -Id $process.Id
Write-Host 'PULSE background preview stopped. Port 8088 is available for your own run.'
