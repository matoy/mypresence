param(
    [switch]$SkipGoTests,
    [int]$AppPort = 18080,
    [int]$MockHttpPort = 19080,
    [int]$MockSmtpPort = 19025,
    [string]$PostgresDSN = ""
)

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $RepoRoot

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("mypresence-recette-" + [System.Guid]::NewGuid().ToString("N").Substring(0, 8))
$dataDir = Join-Path $tmpDir "data"
New-Item -ItemType Directory -Force -Path $tmpDir, $dataDir | Out-Null

$appProc = $null
$mockProc = $null

try {
    if (-not $SkipGoTests) {
        Write-Host "==> [1/4] Running Go vet & black-box functional acceptance suite..." -ForegroundColor Cyan
        go vet ./...
        if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

        go test -v -run TestRecette_FullReleaseAcceptance .
        if ($LASTEXITCODE -ne 0) { throw "TestRecette_FullReleaseAcceptance failed" }
    }

    Write-Host "==> [2/4] Compiling mypresence and cmd/testmocks binaries..." -ForegroundColor Cyan
    $appBin = Join-Path $tmpDir "mypresence.exe"
    $mockBin = Join-Path $tmpDir "testmocks.exe"
    go build -o $appBin .
    if ($LASTEXITCODE -ne 0) { throw "go build mypresence failed" }
    go build -o $mockBin ./cmd/testmocks
    if ($LASTEXITCODE -ne 0) { throw "go build cmd/testmocks failed" }

    $appBaseUrl = "http://127.0.0.1:$AppPort"
    $mockBaseUrl = "http://127.0.0.1:$MockHttpPort"

    Write-Host "==> [3/4] Starting mock integration suite ($mockBaseUrl) & mypresence ($appBaseUrl)..." -ForegroundColor Cyan
    $mockProc = Start-Process -FilePath $mockBin `
        -ArgumentList "--http-addr=127.0.0.1:$MockHttpPort", "--smtp-addr=127.0.0.1:$MockSmtpPort", "--public-url=$mockBaseUrl", "--saml-acs-url=$appBaseUrl/saml/acs" `
        -PassThru -NoNewWindow -RedirectStandardOutput (Join-Path $tmpDir "testmocks.out.log") -RedirectStandardError (Join-Path $tmpDir "testmocks.err.log")

    for ($i = 0; $i -lt 40; $i++) {
        try {
            $r = Invoke-WebRequest -Uri "$mockBaseUrl/healthz" -UseBasicParsing -TimeoutSec 2
            if ($r.StatusCode -eq 200) { break }
        } catch {}
        Start-Sleep -Milliseconds 250
    }

    $envMap = @{
        "PORT"                          = "$AppPort"
        "DATA_DIR"                      = $dataDir
        "SECRET_KEY"                    = "recette-local-secret-key-at-least-32-characters"
        "APP_URL"                       = $appBaseUrl
        "APP_NAME"                      = "Presence Recette E2E"
        "ADMIN_USER"                    = "admin"
        "ADMIN_PASSWORD"                = "AdminRecette#2026!"
        "DEFAULT_LANG"                  = "fr"
        "SAML_IDP_METADATA_URL"         = "$mockBaseUrl/saml/idp/metadata"
        "SAML_ENTITY_ID"                = "$appBaseUrl/saml/metadata"
        "SAML_ROOT_URL"                 = $appBaseUrl
        "SAML_AUTO_LOGIN"               = "false"
        "SAML_GROUPS_CLAIM"             = "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"
        "SAML_GROUP_GLOBAL"             = "group-global-id"
        "SAML_GROUP_FLOORPLAN_MANAGER"  = "group-fp-id"
        "O365_SYNC_ENABLED"             = "true"
        "O365_TENANT_ID"                = "common"
        "O365_CLIENT_ID"                = "e2e-o365-client"
        "O365_CLIENT_SECRET"            = "e2e-o365-secret"
        "O365_REDIRECT_URL"             = "$appBaseUrl/auth/o365/callback"
        "O365_LOGIN_BASE_URL"           = $mockBaseUrl
        "O365_GRAPH_BASE_URL"           = "$mockBaseUrl/v1.0"
        "JIRA_CLOUD_ID"                 = "cloud-e2e"
        "JIRA_BASE_URL"                 = $mockBaseUrl
        "JIRA_TOKEN"                    = "e2e-jira-token"
        "SMTP_URL"                      = "smtp://127.0.0.1:$MockSmtpPort"
        "SMTP_FROM"                     = "noreply@presence.local"
        "METRICS_TOKEN"                 = "e2e-metrics-token"
        "ENABLE_PASSKEYS"               = "true"
        "PASSKEY_RP_ID"                 = "localhost"
        "PASSKEY_RP_ORIGIN"             = "http://localhost:$AppPort"
    }
    if ($PostgresDSN -ne "") {
        $envMap["DB_DRIVER"] = "postgres"
    }

    $savedEnv = @{}
    foreach ($k in $envMap.Keys) {
        $savedEnv[$k] = [Environment]::GetEnvironmentVariable($k, "Process")
        [Environment]::SetEnvironmentVariable($k, $envMap[$k], "Process")
    }

    try {
        $appProc = Start-Process -FilePath $appBin -PassThru -NoNewWindow `
            -RedirectStandardOutput (Join-Path $tmpDir "app.out.log") `
            -RedirectStandardError (Join-Path $tmpDir "app.err.log")
    } finally {
        foreach ($k in $envMap.Keys) {
            [Environment]::SetEnvironmentVariable($k, $savedEnv[$k], "Process")
        }
    }

    $healthy = $false
    for ($i = 0; $i -lt 60; $i++) {
        if ($appProc.HasExited) { break }
        try {
            $r = Invoke-WebRequest -Uri "$appBaseUrl/health" -UseBasicParsing -TimeoutSec 2
            if ($r.StatusCode -eq 200) { $healthy = $true; break }
        } catch {}
        Start-Sleep -Milliseconds 300
    }
    if (-not $healthy) {
        Write-Host "--- mypresence stderr ---" -ForegroundColor Red
        Get-Content (Join-Path $tmpDir "app.err.log") -ErrorAction SilentlyContinue
        throw "mypresence failed to become healthy on $appBaseUrl"
    }

    Write-Host "==> [4/4] Running Playwright E2E release acceptance suite..." -ForegroundColor Cyan
    $env:BASE_URL = $appBaseUrl
    $env:MOCK_URL = $mockBaseUrl
    $env:ADMIN_USER = "admin"
    $env:ADMIN_PASSWORD = "AdminRecette#2026!"
    $env:EXPECT_PASSKEYS = "1"

    Push-Location (Join-Path $RepoRoot "e2e")
    try {
        npx playwright test
        $pwExit = $LASTEXITCODE
    } finally {
        Pop-Location
    }

    if ($pwExit -ne 0) {
        Write-Host "--- mypresence logs (tail 60) ---" -ForegroundColor Yellow
        Get-Content (Join-Path $tmpDir "app.out.log") -Tail 60 -ErrorAction SilentlyContinue
        Get-Content (Join-Path $tmpDir "app.err.log") -Tail 60 -ErrorAction SilentlyContinue
        throw "Playwright suite failed with exit code $pwExit"
    }

    Write-Host "ALL RECETTE CHECKS PASSED CLEANLY!" -ForegroundColor Green
}
finally {
    if ($appProc -and -not $appProc.HasExited) { Stop-Process -Id $appProc.Id -Force -ErrorAction SilentlyContinue }
    if ($mockProc -and -not $mockProc.HasExited) { Stop-Process -Id $mockProc.Id -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 300
    Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    Pop-Location
}
