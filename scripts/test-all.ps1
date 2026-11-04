# Runs the whole test suite against real database servers in Docker. -Keep leaves them
# running afterwards; other arguments go to `go test`.
param(
    [switch]$Keep,
    [Parameter(ValueFromRemainingArguments)] [string[]]$TestArgs
)

Set-Location (Split-Path -Parent $PSScriptRoot)

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Write-Host 'Docker is not installed. Install Docker Desktop, then run this again.'
    exit 1
}
docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Host 'Docker is not running. Start Docker Desktop, wait until it says it is running, then run this again.'
    exit 1
}

$status = 0
try {
    docker compose up -d --wait
    if ($LASTEXITCODE -ne 0) { throw 'docker compose could not start the database servers' }

    $env:CGO_ENABLED = '1'
    $env:LODE_TEST_POSTGRES_DSN = 'postgres://postgres:postgres@127.0.0.1:55432/postgres?sslmode=disable'
    $env:LODE_TEST_MYSQL_DSN = 'mysql://root:lode-test@127.0.0.1:53306/'
    $env:LODE_TEST_MARIADB_DSN = 'mariadb://root:lode-test@127.0.0.1:53307/'
    $env:LODE_REQUIRE_SERVERS = '1'

    go test -count=1 @TestArgs ./...
    if ($LASTEXITCODE -ne 0) { $status = $LASTEXITCODE }
}
catch {
    Write-Host $_
    $status = 1
}
finally {
    if (-not $Keep) { docker compose down }
}
exit $status
