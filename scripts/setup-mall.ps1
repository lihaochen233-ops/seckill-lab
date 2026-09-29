$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$envPath = Join-Path $projectRoot '.env.mall'
if (Test-Path -LiteralPath $envPath) { Write-Host '.env.mall already exists; kept unchanged.'; exit 0 }
function New-Secret {
    $bytes = New-Object byte[] 24
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
}
$values = @(
    "MYSQL_ROOT_PASSWORD=$(New-Secret)", "MYSQL_PASSWORD=$(New-Secret)",
    "REDIS_PASSWORD=$(New-Secret)", "RABBIT_PASSWORD=$(New-Secret)",
    'ADMIN_EMAIL=admin@pulse.local', "ADMIN_PASSWORD=$(New-Secret)",
    'APP_ORIGINS=http://127.0.0.1:8088,http://localhost:8088',
    'COOKIE_SECURE=false', 'ORDER_TTL=15m'
)
[IO.File]::WriteAllLines($envPath, $values, (New-Object Text.UTF8Encoding($false)))
Write-Host 'Created .env.mall with random credentials. Read ADMIN_PASSWORD locally to sign in.'
Write-Host 'Start: docker compose --env-file .env.mall -f compose.mall.yaml up --build -d'
