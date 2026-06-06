#Requires -Version 5.1
<#
.SYNOPSIS
    Installs lambit on Windows.

.DESCRIPTION
    Builds the lambit binary with 'go build', installs it to
    $env:LOCALAPPDATA\Programs\lambit, adds that directory to your
    user PATH (persisted via the registry and your PowerShell profile),
    and confirms the install.

    No admin rights required.

.EXAMPLE
    .\install.ps1
#>

param(
    [switch]$Update,
    [switch]$BuildAll
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$BinaryName  = 'lambit.exe'
$InstallDir  = Join-Path $env:LOCALAPPDATA 'Programs\lambit'
$BuildDir    = Join-Path $PSScriptRoot 'bin'
$BinaryBuild = Join-Path $BuildDir $BinaryName
$BinaryDest  = Join-Path $InstallDir $BinaryName
$ReleaseBin  = Join-Path $PSScriptRoot 'releases\windows\lambit.exe'

function Write-Step([string]$msg) {
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Write-Ok([string]$msg) {
    Write-Host "    $msg" -ForegroundColor Green
}

function Write-Note([string]$msg) {
    Write-Host "    $msg" -ForegroundColor Yellow
}

# ---------------------------------------------------------------------------
# 1. Build release binaries, or obtain the install binary
# ---------------------------------------------------------------------------
if ($BuildAll) {
    Write-Step 'Checking for Go (required for -BuildAll)...'
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Host 'ERROR: Go is not installed or not on PATH.' -ForegroundColor Red
        Write-Host 'Download Go from https://go.dev/dl/ and re-run with -BuildAll.' -ForegroundColor Red
        exit 1
    }
    Write-Ok (go version)
    $Commit = (git rev-parse HEAD 2>$null)
    if (-not $Commit) { $Commit = 'dev' }
    $Targets = @(
        @{ GOOS = 'linux';   GOARCH = 'amd64'; Out = 'releases\linux\amd64\lambit' },
        @{ GOOS = 'linux';   GOARCH = 'arm64'; Out = 'releases\linux\arm64\lambit' },
        @{ GOOS = 'darwin';  GOARCH = 'amd64'; Out = 'releases\darwin\amd64\lambit' },
        @{ GOOS = 'darwin';  GOARCH = 'arm64'; Out = 'releases\darwin\arm64\lambit' },
        @{ GOOS = 'windows'; GOARCH = 'amd64'; Out = 'releases\windows\lambit.exe' }
    )
    foreach ($t in $Targets) {
        Write-Step "Building $($t.GOOS)/$($t.GOARCH)..."
        $out = Join-Path $PSScriptRoot $t.Out
        New-Item -ItemType Directory -Path (Split-Path $out) -Force | Out-Null
        $env:GOOS = $t.GOOS
        $env:GOARCH = $t.GOARCH
        & go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$Commit" -o $out .
        if ($LASTEXITCODE -ne 0) { Write-Host "ERROR: build failed for $($t.GOOS)/$($t.GOARCH)." -ForegroundColor Red; exit 1 }
        Write-Ok $t.Out
    }
    $env:GOOS = $null
    $env:GOARCH = $null
    Write-Host ''
    Write-Host '  Pre-built binaries written to releases\' -ForegroundColor Green
    exit 0
}

if (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Step 'Go found - building lambit from source...'
    Write-Ok (go version)
    if (-not (Test-Path $BuildDir)) { New-Item -ItemType Directory -Path $BuildDir | Out-Null }
    $Commit = (git rev-parse HEAD 2>$null)
    if (-not $Commit) { $Commit = 'dev' }
    & go build -ldflags="-s -w -X github.com/wingitman/lambit/internal/version.Commit=$Commit" -o $BinaryBuild .
    if ($LASTEXITCODE -ne 0) { Write-Host 'ERROR: go build failed.' -ForegroundColor Red; exit 1 }
    Write-Ok "Built: $BinaryBuild"
    $SourceBin = $BinaryBuild
} else {
    Write-Step 'Go not found - using pre-built binary from releases\windows\...'
    if (-not (Test-Path $ReleaseBin)) {
        Write-Host 'ERROR: Pre-built binary not found at releases\windows\lambit.exe' -ForegroundColor Red
        Write-Host '       Please install Go (https://go.dev/dl/) and re-run, or ask a developer to run ".\install.ps1 -BuildAll" and commit the releases\ folder.' -ForegroundColor Red
        exit 1
    }
    Write-Ok "Using: $ReleaseBin"
    $SourceBin = $ReleaseBin
}

# ---------------------------------------------------------------------------
# 3. Install binary
# ---------------------------------------------------------------------------
Write-Step 'Installing binary...'
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}
Copy-Item -Path $SourceBin -Destination $BinaryDest -Force
Write-Ok "Installed: $BinaryDest"

# ---------------------------------------------------------------------------
# 4. Add install dir to user PATH via registry (persists for new terminals)
# ---------------------------------------------------------------------------
Write-Step 'Updating user PATH (registry)...'
$registryPath = 'HKCU:\Environment'
$currentPath  = (Get-ItemProperty -Path $registryPath -Name Path -ErrorAction SilentlyContinue).Path

if ($currentPath -and ($currentPath -split ';') -contains $InstallDir) {
    Write-Note "$InstallDir is already in your registry PATH."
} else {
    $newPath = if ($currentPath) { "$currentPath;$InstallDir" } else { $InstallDir }
    Set-ItemProperty -Path $registryPath -Name Path -Value $newPath
    Write-Ok "Added $InstallDir to registry PATH."

    # Broadcast WM_SETTINGCHANGE so Explorer and new terminals pick up the
    # change without requiring a logoff.
    $signature = @'
[DllImport("user32.dll", SetLastError=true, CharSet=CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(
    IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam,
    uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
    $type   = Add-Type -MemberDefinition $signature -Name WinEnv -Namespace Win32 -PassThru
    $result = [UIntPtr]::Zero
    $type::SendMessageTimeout(
        [IntPtr]0xffff, 0x001A, [UIntPtr]::Zero, 'Environment',
        0x0002, 5000, [ref]$result
    ) | Out-Null
}

# Also update the current session immediately.
if (($env:PATH -split ';') -notcontains $InstallDir) {
    $env:PATH = "$env:PATH;$InstallDir"
}

# ---------------------------------------------------------------------------
# 5. Patch PowerShell profile (belt-and-suspenders PATH)
#    Adds $env:PATH line so lambit is available even if the registry broadcast
#    doesn't propagate before the next terminal is opened.
# ---------------------------------------------------------------------------
Write-Step 'Patching PowerShell profile...'

$profilePath = $PROFILE
$profileDir  = Split-Path $profilePath -Parent

if (-not (Test-Path $profileDir)) {
    New-Item -ItemType Directory -Path $profileDir | Out-Null
}
if (-not (Test-Path $profilePath)) {
    New-Item -ItemType File -Path $profilePath | Out-Null
    Write-Note "Created profile: $profilePath"
}

$profileContent = Get-Content $profilePath -Raw -ErrorAction SilentlyContinue
$marker         = 'lambit PATH'

if ($profileContent -and $profileContent -match [regex]::Escape($marker)) {
    Write-Note 'Profile already has lambit PATH entry.'
} else {
    $pathLine = "`$env:PATH = `"$InstallDir;`$env:PATH`""
    $addition = "`n# $marker`n$pathLine`n"
    Add-Content -Path $profilePath -Value $addition -Encoding UTF8
    Write-Ok "Added lambit PATH to profile: $profilePath"
}

# ---------------------------------------------------------------------------
# 6. Done
# ---------------------------------------------------------------------------
$ConfigFile = Join-Path ([System.Environment]::GetFolderPath('ApplicationData')) 'delbysoft\lambit.toml'

Write-Host ''
Write-Host '  lambit installed successfully!' -ForegroundColor Green
Write-Host ''
Write-Host '  Open a new PowerShell terminal and run lambit from your lambda project:' -ForegroundColor White
Write-Host '    cd C:\path\to\your\lambda-project' -ForegroundColor Cyan
Write-Host '    lambit' -ForegroundColor Cyan
Write-Host ''
Write-Host '  Or reload your profile in this session:' -ForegroundColor White
Write-Host '    . $PROFILE' -ForegroundColor Cyan
Write-Host ''
Write-Host '  Config file (created on first launch):' -ForegroundColor White
Write-Host "    $ConfigFile" -ForegroundColor Cyan
Write-Host ''
Write-Note "  Tip: if you see an 'execution policy' error, run once as your user:"
Write-Note '    Set-ExecutionPolicy -Scope CurrentUser RemoteSigned'
Write-Host ''
