<#
.SYNOPSIS
    Bumps or sets the application version in internal/config/config.go using the YYYYMMDD-X format.

.DESCRIPTION
    This script updates the 'const Version' declaration in internal/config/config.go.
    If no explicit version is passed, it automatically calculates the next version:
      - If the current version shares today's date prefix (YYYYMMDD), it increments the suffix (-X -> -(X+1)).
      - Otherwise, it resets the version to today's date with suffix -1 (YYYYMMDD-1).
    
    Optional switches:
      -Commit: Automatically stages config.go, creates the git commit, and creates the local tag (YYYYMMDD-X).
      -Push:   Performs the commit, tags locally, and pushes both the commit and tag to the remote repository.

.PARAMETER Version
    An optional explicit version string to set directly (e.g. 20260827-1).

.PARAMETER Commit
    Create git commit and local tag automatically.

.PARAMETER Push
    Create git commit, local tag, and push to origin automatically.

.EXAMPLE
    pwsh -File scripts\bump-version.ps1
    # Updates config.go and prints suggested git commands

.EXAMPLE
    pwsh -File scripts\bump-version.ps1 -Commit
    # Updates config.go, creates the commit and creates the local tag YYYYMMDD-X

.EXAMPLE
    pwsh -File scripts\bump-version.ps1 -Push
    # Updates config.go, creates commit, creates tag, and pushes to git remote
#>

[CmdletBinding()]
param (
    [Parameter(Position = 0, Mandatory = $false)]
    [string]$Version,

    [Parameter(Mandatory = $false)]
    [switch]$Commit,

    [Parameter(Mandatory = $false)]
    [switch]$Push
)

$ErrorActionPreference = "Stop"

# Path to the config file containing the Version constant
$configFile = Join-Path $PSScriptRoot "..\internal\config\config.go"
if (-not (Test-Path $configFile)) {
    throw "Configuration file not found at: $configFile"
}

$content = Get-Content $configFile -Raw -Encoding UTF8

# Extract current version from config.go
if ($content -match 'const\s+Version\s*=\s*"([^"]+)"') {
    $currentVersion = $Matches[1]
} else {
    throw "Could not locate 'const Version' declaration in $configFile"
}

$today = (Get-Date).ToString("yyyyMMdd")

if ([string]::IsNullOrWhiteSpace($Version)) {
    # Check if current version starts with today's date prefix (YYYYMMDD-X)
    if ($currentVersion -match "^$today-(\d+)$") {
        $nextIndex = [int]$Matches[1] + 1
        $newVersion = "$today-$nextIndex"
    } else {
        # First release for today
        $newVersion = "$today-1"
    }
} else {
    $newVersion = $Version.Trim()
}

if ($newVersion -eq $currentVersion -and -not $Commit -and -not $Push) {
    Write-Host "Version is already up to date: $currentVersion" -ForegroundColor Yellow
    return
}

# Replace Version declaration in config.go
$newContent = [regex]::Replace(
    $content,
    'const\s+Version\s*=\s*"[^"]+"',
    "const Version = `"$newVersion`""
)

Set-Content -Path $configFile -Value $newContent -Encoding UTF8 -NoNewline

Write-Host "Application version updated:" -ForegroundColor Green
Write-Host "  Previous: $currentVersion"
Write-Host "  New:      $newVersion"

$tag = "$newVersion"

if ($Commit -or $Push) {
    Write-Host "`nCreating Git commit and tag..." -ForegroundColor Cyan
    git add (Resolve-Path $configFile).Path
    git commit -m "chore: bump version to $newVersion"
    git tag $tag
    Write-Host "  Tag '$tag' created locally." -ForegroundColor Green

    if ($Push) {
        Write-Host "`nPushing commit and tags to remote..." -ForegroundColor Cyan
        git push origin HEAD
        git push origin $tag
        Write-Host "  Pushed to remote successfully." -ForegroundColor Green
    } else {
        Write-Host "`nTo push to GitHub, run:" -ForegroundColor Cyan
        Write-Host "  git push origin HEAD"
        Write-Host "  git push origin $tag"
    }
} else {
    Write-Host ""
    Write-Host "Suggested git commands to commit and tag:" -ForegroundColor Cyan
    Write-Host "  git add internal/config/config.go"
    Write-Host "  git commit -m `"chore: bump version to $newVersion`""
    Write-Host "  git tag $tag"
    Write-Host "  git push origin $tag"
}
