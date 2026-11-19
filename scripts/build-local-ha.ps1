param([string]$GoExe = 'go')
$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$env:GOEXPERIMENT = 'jsonv2'
$env:CGO_ENABLED = '0'
$env:GOTOOLCHAIN = 'local'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
Push-Location $repositoryRoot
try {
    $ErrorActionPreference = 'Continue'
    & $GoExe test ./internal/directha ./internal/bridge
    if ($LASTEXITCODE -ne 0) { throw 'Handler/routing tests failed.' }
    & $GoExe test ./cmd/quasar-bridge -run 'TestLocalMode|TestReloadJournalKeepsWorkingPolicyOnOpenFailure|TestDirectHAEditor'
    if ($LASTEXITCODE -ne 0) { throw 'Configuration tests failed.' }
    New-Item -ItemType Directory -Path 'dist' -Force | Out-Null
    $env:GOOS = 'linux'
    $env:GOARCH = 'arm64'
    & $GoExe build -trimpath -buildvcs=false -ldflags '-s -w' -o 'dist/quasar-bridge-local-arm64' ./cmd/quasar-bridge
    if ($LASTEXITCODE -ne 0) { throw 'ARM64 build failed.' }
    Get-FileHash -LiteralPath 'dist/quasar-bridge-local-arm64' -Algorithm SHA256
} finally { Pop-Location }
