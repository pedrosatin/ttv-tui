[CmdletBinding()]
param(
    [string]$Version = 'latest',
    [string]$Prefix = (Join-Path $env:LOCALAPPDATA 'Programs\ttv-tui'),
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$repo = 'pedrosatin/ttv-tui'
if ([string]::IsNullOrWhiteSpace($Prefix)) { throw 'Informe um prefixo de instalação válido.' }
if ($Version -eq 'latest' -and -not $DryRun) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" -Headers @{ 'User-Agent' = 'ttv-tui-installer' }
    $Version = $release.tag_name
}
$versionNumber = $Version -replace '^v', ''
if ($versionNumber -ne 'latest' -and $versionNumber -notmatch '^[0-9][0-9A-Za-z._-]*$') { throw "Versão inválida: $Version" }
$binDir = [IO.Path]::GetFullPath((Join-Path $Prefix 'bin'))
$archive = "ttv-tui_${versionNumber}_windows_amd64.zip"
$base = "https://github.com/$repo/releases/download/v$versionNumber"
if ($DryRun) {
    Write-Host "Plano: versão $Version; destino $binDir\ttv-tui.exe"
    if ($versionNumber -eq 'latest') { Write-Host 'Resolver a última versão no GitHub antes do download.' }
    else { Write-Host "Baixar $base/$archive e $base/checksums.txt" }
    Write-Host 'Verificar SHA-256, extrair o binário e adicionar o diretório ao PATH do usuário se necessário.'
    return
}
if (-not [Environment]::Is64BitOperatingSystem -or $env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { throw 'Este instalador requer Windows x64.' }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("ttv-tui-" + [guid]::NewGuid().ToString())
try {
    New-Item -ItemType Directory -Path $tempDir | Out-Null
    $zipPath = Join-Path $tempDir $archive
    $checksumsPath = Join-Path $tempDir 'checksums.txt'
    Write-Host "Baixando $archive..."
    Invoke-WebRequest -Uri "$base/$archive" -OutFile $zipPath -UseBasicParsing
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile $checksumsPath -UseBasicParsing
    $pattern = '^([a-fA-F0-9]{64})\s+\*?' + [regex]::Escape($archive) + '$'
    $matchesFound = @(Get-Content -LiteralPath $checksumsPath | Where-Object { $_ -match $pattern })
    if ($matchesFound.Count -ne 1) { throw "Entrada única para $archive não encontrada em checksums.txt." }
    $expected = ([regex]::Match($matchesFound[0], $pattern)).Groups[1].Value
    $actual = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash
    if ($actual -ne $expected) { throw "SHA-256 inválido para $archive." }
    Write-Host 'Checksum SHA-256 verificado. Extraindo...'
    $extractDir = Join-Path $tempDir 'extracted'
    Expand-Archive -LiteralPath $zipPath -DestinationPath $extractDir
    $binary = Join-Path $extractDir 'ttv-tui.exe'
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw 'ttv-tui.exe não encontrado no arquivo.' }
    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    Copy-Item -LiteralPath $binary -Destination (Join-Path $binDir 'ttv-tui.exe') -Force
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $normalizedBin = $binDir.TrimEnd('\', '/')
    $present = @($userPath -split ';' | Where-Object { [Environment]::ExpandEnvironmentVariables($_.Trim().Trim('"')).TrimEnd('\', '/') -ieq $normalizedBin }).Count -gt 0
    if (-not $present) {
        $newPath = if ([string]::IsNullOrEmpty($userPath)) { $binDir } else { $userPath.TrimEnd(';') + ';' + $binDir }
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host "Adicionado ao PATH do usuário: $binDir"
    } else { Write-Host "Já está no PATH do usuário: $binDir" }
    Write-Host "ttv-tui v$versionNumber instalado em $binDir."
    foreach ($dependency in @(@('mpv', 'shinchiro.mpv'), @('streamlink', 'streamlink.streamlink'))) {
        if (-not (Get-Command $dependency[0] -ErrorAction SilentlyContinue)) {
            Write-Host "$($dependency[0]) não encontrado. Instale com: winget install --id $($dependency[1]) -e"
            Write-Host "Ou, com o bucket extras do Scoop: scoop install extras/$($dependency[0])"
        }
    }
    Write-Host 'Abra um novo terminal e execute: ttv-tui --version'
} finally {
    if (Test-Path -LiteralPath $tempDir) { Remove-Item -LiteralPath $tempDir -Recurse -Force }
}
