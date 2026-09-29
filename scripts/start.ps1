param([switch]$SkipFrontend)
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path -Parent $PSScriptRoot)
try {
    $env:GOCACHE = Join-Path (Get-Location) '.local/go-cache'
    $env:GOMODCACHE = Join-Path (Get-Location) '.local/go-mod'
    if (-not $SkipFrontend) {
        Push-Location frontend
        try {
            if (-not (Test-Path node_modules)) { npm ci; if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' } }
            npm run build
            if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
        } finally { Pop-Location }
    }
    Write-Host 'PULSE demo: http://127.0.0.1:8088 (Ctrl+C stops the server)'
    go run ./cmd/mall -mode demo
    if ($LASTEXITCODE -ne 0) { throw 'Mall stopped with an error' }
} finally { Pop-Location }
