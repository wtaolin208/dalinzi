param([ValidateSet('windows','linux')][string]$TargetOS='linux',[string]$TargetArch='amd64')
$ErrorActionPreference='Stop'
Push-Location (Join-Path $PSScriptRoot '..')
$oldModule=$env:GO111MODULE
$oldOS=$env:GOOS
$oldArch=$env:GOARCH
$oldCGO=$env:CGO_ENABLED
try {
    $env:GO111MODULE='on'
    $env:GOOS=$TargetOS
    $env:GOARCH=$TargetArch
    $env:CGO_ENABLED='0'
    $outDir=Join-Path (Get-Location) "dist/$TargetOS-$TargetArch"
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $suffix=if ($TargetOS -eq 'windows') {'.exe'} else {''}
    & go build -trimpath -o (Join-Path $outDir "agent$suffix") ./cmd/agent
    if ($LASTEXITCODE -ne 0) {throw 'agent build failed'}
    & go build -trimpath -o (Join-Path $outDir "replay$suffix") ./cmd/replay
    if ($LASTEXITCODE -ne 0) {throw 'replay build failed'}
    foreach ($toolName in @('preflight','review','iterate')) {
        & go build -trimpath -o (Join-Path $outDir "$toolName$suffix") "./cmd/$toolName"
        if ($LASTEXITCODE -ne 0) {throw "$toolName build failed"}
    }
    Copy-Item -LiteralPath run.sh -Destination $outDir
    Copy-Item -LiteralPath configs -Destination $outDir -Recurse -Force
    Get-ChildItem -LiteralPath $outDir -File | Get-FileHash -Algorithm SHA256
} finally {
    $env:GO111MODULE=$oldModule
    $env:GOOS=$oldOS
    $env:GOARCH=$oldArch
    $env:CGO_ENABLED=$oldCGO
    Pop-Location
}
