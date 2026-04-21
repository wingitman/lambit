#Requires -Version 5.1
<#
.SYNOPSIS
    Uninstalls lambit on Windows.

.DESCRIPTION
    Removes the lambit binary and install directory from
    $env:LOCALAPPDATA\Programs\lambit, removes that directory from your
    user PATH in the registry, and notes where to clean up the profile
    entry added by install.ps1.

    No admin rights required.

.EXAMPLE
    .\uninstall.ps1
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\lambit'

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
# 1. Remove install directory
# ---------------------------------------------------------------------------
Write-Step 'Removing lambit binary...'
if (Test-Path $InstallDir) {
    Remove-Item -Path $InstallDir -Recurse -Force
    Write-Ok "Removed: $InstallDir"
} else {
    Write-Note "Install directory not found (already removed?): $InstallDir"
}

# ---------------------------------------------------------------------------
# 2. Remove install dir from user PATH in registry
# ---------------------------------------------------------------------------
Write-Step 'Updating user PATH (registry)...'
$registryPath = 'HKCU:\Environment'
$currentPath  = (Get-ItemProperty -Path $registryPath -Name Path -ErrorAction SilentlyContinue).Path

if (-not $currentPath) {
    Write-Note 'User PATH entry not found in registry — nothing to remove.'
} else {
    $parts   = $currentPath -split ';' | Where-Object { $_ -ne $InstallDir -and $_ -ne '' }
    $newPath = $parts -join ';'

    if ($newPath -eq $currentPath) {
        Write-Note "$InstallDir was not in registry PATH."
    } else {
        Set-ItemProperty -Path $registryPath -Name Path -Value $newPath
        Write-Ok "Removed $InstallDir from registry PATH."

        # Broadcast WM_SETTINGCHANGE so new terminals pick up the change.
        $signature = @'
[DllImport("user32.dll", SetLastError=true, CharSet=CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(
    IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam,
    uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
        $type   = Add-Type -MemberDefinition $signature -Name WinEnvU -Namespace Win32 -PassThru
        $result = [UIntPtr]::Zero
        $type::SendMessageTimeout(
            [IntPtr]0xffff, 0x001A, [UIntPtr]::Zero, 'Environment',
            0x0002, 5000, [ref]$result
        ) | Out-Null
    }
}

# ---------------------------------------------------------------------------
# 3. Done
# ---------------------------------------------------------------------------
Write-Host ''
Write-Ok 'lambit has been uninstalled.'
Write-Host ''
Write-Note 'Your PowerShell profile still contains the lambit PATH line.'
Write-Note "It is now harmless, but you can remove it from:"
Write-Note "  $PROFILE"
Write-Note "Look for the block starting with '# lambit PATH'."
Write-Host ''
