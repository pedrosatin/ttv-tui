[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$Prefix = (Join-Path $env:LOCALAPPDATA 'Programs\ttv-tui'),
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$repo = 'pedrosatin/ttv-tui'
if ([string]::IsNullOrWhiteSpace($Prefix)) { throw 'Provide a valid installation prefix.' }
if ($Version -eq 'latest' -and -not $DryRun) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'ttv-tui-installer' }
    $Version = $release.tag_name
}
$versionNumber = $Version -replace '^v', ''
if ($versionNumber -ne 'latest' -and $versionNumber -notmatch '^[0-9][0-9A-Za-z._-]*$') { throw "Invalid version: $Version" }
$binDir = [IO.Path]::GetFullPath((Join-Path $Prefix 'bin'))
$archive = "ttv-tui_${versionNumber}_windows_amd64.zip"
$base = "https://github.com/$repo/releases/download/v$versionNumber"
if ($DryRun) {
    Write-Host "Plan: version $Version; destination $binDir\ttv-tui.exe"
    if ($versionNumber -eq 'latest') { Write-Host 'Resolve the latest version on GitHub before downloading.' }
    else { Write-Host "Download $base/$archive e $base/checksums.txt" }
    Write-Host 'Verify SHA-256, extract the binary, and add the directory to the user PATH if needed.'
    return
}
if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { throw 'This installer requires Windows x64.' }
function Invoke-DownloadWithRetry {
    param([string]$Uri, [string]$OutFile)
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        Write-Host "Downloading $Uri (attempt $attempt of 3)..."
        try {
            Invoke-WebRequest -Uri $Uri -OutFile $OutFile -UseBasicParsing
            return
        } catch {
            if ($attempt -eq 3) { throw "Download failed after 3 attempts: $Uri" }
            $delay = 2 * $attempt
            Write-Host "Download failed; retrying in $delay seconds."
            Start-Sleep -Seconds $delay
        }
    }
}
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("ttv-tui-" + [guid]::NewGuid().ToString())
try {
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    $zipPath = Join-Path $tempDir $archive
    $checksumsPath = Join-Path $tempDir 'checksums.txt'
    Invoke-DownloadWithRetry -Uri "$base/$archive" -OutFile $zipPath
    Invoke-DownloadWithRetry -Uri "$base/checksums.txt" -OutFile $checksumsPath
    $pattern = '^([a-fA-F0-9]{64})\s+\*?' + [regex]::Escape($archive) + '$'
    $matchesFound = @(Get-Content -LiteralPath $checksumsPath | Where-Object { $_ -match $pattern })
    if ($matchesFound.Count -ne 1) { throw "Expected exactly one entry for $archive in checksums.txt." }
    $expected = ([regex]::Match($matchesFound[0], $pattern)).Groups[1].Value
    $actual = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash
    if ($actual -ne $expected) { throw "Invalid SHA-256 for $archive." }
    Write-Host 'SHA-256 checksum verified. Extracting...'
    $extractDir = Join-Path $tempDir 'extracted'
    Expand-Archive -LiteralPath $zipPath -DestinationPath $extractDir
    $binary = Join-Path $extractDir 'ttv-tui.exe'
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw 'ttv-tui.exe not found in the archive.' }
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $binDir 'ttv-tui.exe') -Force
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $normalizedBin = $binDir.TrimEnd('\', '/')
    $present = @($userPath -split ';' | Where-Object { [Environment]::ExpandEnvironmentVariables($_.Trim().Trim('"')).TrimEnd('\', '/') -ieq $normalizedBin }).Count -gt 0
    if (-not $present) {
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $binDir } else { $userPath.TrimEnd(';') + ';' + $binDir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Added to user PATH: $binDir"
    } else { Write-Host "Already in user PATH: $binDir" }
    Write-Host "ttv-tui v$versionNumber installed in $binDir."
    foreach ($dependency in @(@('mpv', 'shinchiro.mpv'), @('streamlink', 'streamlink.streamlink'))) {
        if (-not (Get-Command $dependency[0] -ErrorAction SilentlyContinue)) {
            Write-Host "$($dependency[0]) not found. Install with: winget install --id $($dependency[1]) -e"
            Write-Host "Or, with the Scoop extras bucket: scoop install extras/$($dependency[0])"
        }
    }
    Write-Host 'Open a new terminal and run: ttv-tui --version'
} finally {
    if (Test-Path -LiteralPath $tempDir) { Remove-Item -LiteralPath $tempDir -Recurse -Force }
}
