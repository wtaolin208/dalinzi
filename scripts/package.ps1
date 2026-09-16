param(
    [string]$ConfigPath='configs/default.json',
    [string]$ChallengerRequest='',
    [string]$DefenderRequest='',
    [switch]$LocalOnly
)
$ErrorActionPreference='Stop'
$projectRoot=Split-Path $PSScriptRoot -Parent
$releaseId=(Get-Date -Format 'yyyyMMdd-HHmmss')+'-'+[guid]::NewGuid().ToString('N').Substring(0,8)
$releaseRoot=Join-Path $projectRoot "iteration/releases/$releaseId"
$sourceRoot=Join-Path $releaseRoot 'source'
$binaryRoot=Join-Path $releaseRoot 'linux-amd64'
$savedEnv=@{}
foreach ($name in @('GO111MODULE','GOOS','GOARCH','CGO_ENABLED')) { $savedEnv[$name]=[Environment]::GetEnvironmentVariable($name,'Process') }
Push-Location $projectRoot
try {
    $selectedConfig=(Resolve-Path -LiteralPath $ConfigPath).Path
    if (-not $LocalOnly -and (-not $ChallengerRequest -or -not $DefenderRequest)) { throw 'Official initial snapshots for both sides are required; use -LocalOnly for an unverified local artifact.' }
    $env:GO111MODULE='on'
    $env:GOOS='windows'
    $env:GOARCH='amd64'
    $env:CGO_ENABLED='0'
    if (-not $LocalOnly) {
        foreach ($pair in @(@('challenger',$ChallengerRequest),@('defender',$DefenderRequest))) {
            $snapshot=Get-Content -Raw -LiteralPath $pair[1] | ConvertFrom-Json
            if ($snapshot.teamOur.type -ne $pair[0] -or $snapshot.roundNo -ne 1) { throw "Expected round 1 $($pair[0]) snapshot" }
            & go run ./cmd/preflight -config $selectedConfig -request $pair[1]
            if ($LASTEXITCODE -ne 0) { throw 'Map/config preflight failed' }
        }
    }
    New-Item -ItemType Directory -Path $sourceRoot,$binaryRoot | Out-Null
    # Explicit source whitelist; excludes logs, credentials, IDE files and old artifacts.
    foreach ($tree in @('cmd','internal')) {
        Get-ChildItem -LiteralPath (Join-Path $projectRoot $tree) -Filter '*.go' -Recurse -File | ForEach-Object {
            $relative=$_.FullName.Substring($projectRoot.Length+1)
            $target=Join-Path $sourceRoot $relative
            New-Item -ItemType Directory -Force -Path (Split-Path $target -Parent) | Out-Null
            Copy-Item -LiteralPath $_.FullName -Destination $target
        }
    }
    foreach ($name in @('go.mod','run.sh','README.md')) { Copy-Item -LiteralPath $name -Destination $sourceRoot }
    New-Item -ItemType Directory -Path (Join-Path $sourceRoot 'docs'),(Join-Path $sourceRoot 'configs'),(Join-Path $binaryRoot 'configs') | Out-Null
    Copy-Item -LiteralPath 'docs/request.txt' -Destination (Join-Path $sourceRoot 'docs/request.txt')
    Copy-Item -LiteralPath $selectedConfig -Destination (Join-Path $sourceRoot 'configs/default.json')
    Copy-Item -LiteralPath $selectedConfig -Destination (Join-Path $binaryRoot 'configs/default.json')
    Copy-Item -LiteralPath 'run.sh' -Destination $binaryRoot
    Set-Location $sourceRoot
    & go test ./... -count=1 2>&1 | Tee-Object -FilePath (Join-Path $releaseRoot 'test.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Frozen source tests failed' }
    & go vet ./... 2>&1 | Tee-Object -FilePath (Join-Path $releaseRoot 'vet.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Frozen source vet failed' }
    $env:GOOS='linux'
    & go build -trimpath -o (Join-Path $binaryRoot 'agent') ./cmd/agent
    if ($LASTEXITCODE -ne 0) { throw 'Linux agent build failed' }
    # ZIP Unix mode bits preserve Linux executable permissions.
    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    foreach ($item in @(@('source',$sourceRoot),@('linux-amd64',$binaryRoot))) {
        $zipPath=Join-Path $releaseRoot ($item[0]+'.zip')
        $archive=[IO.Compression.ZipFile]::Open($zipPath,[IO.Compression.ZipArchiveMode]::Create)
        try {
            Get-ChildItem -LiteralPath $item[1] -Recurse -File | Sort-Object FullName | ForEach-Object {
                $entryName=$_.FullName.Substring($item[1].Length+1).Replace('\','/')
                $entry=[IO.Compression.ZipFileExtensions]::CreateEntryFromFile($archive,$_.FullName,$entryName)
                $mode=if ($entryName -eq 'agent' -or $entryName -eq 'run.sh') {33261} else {33188}
                $entry.ExternalAttributes=($mode -shl 16)
            }
        } finally { $archive.Dispose() }
    }
    $hashes=@{}
    Get-ChildItem -LiteralPath $releaseRoot -Recurse -File | ForEach-Object { $hashes[$_.FullName.Substring($releaseRoot.Length+1)]=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
    $manifest=@{releaseId=$releaseId; localOnly=[bool]$LocalOnly; localTestsPassed=$true; officialBattleVerified=$false; toolchain=(& go version); files=$hashes}
    $manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $releaseRoot 'manifest.json') -Encoding UTF8
    Write-Output "Release: $releaseRoot"
} finally {
    Pop-Location
    foreach ($name in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($name,$savedEnv[$name],'Process') }
}
