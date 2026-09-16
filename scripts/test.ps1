param([switch]$Bench,[switch]$Race)
$ErrorActionPreference='Stop'
Push-Location (Join-Path $PSScriptRoot '..')
$oldModule=$env:GO111MODULE
try {
    $env:GO111MODULE='on'
    & go test ./... -count=1 -cover
    if ($LASTEXITCODE -ne 0) {throw 'tests failed'}
    & go vet ./...
    if ($LASTEXITCODE -ne 0) {throw 'vet failed'}
    if ($Bench) {
        & go test ./internal/navigation ./internal/game -run '^$' -bench . -benchmem '-benchtime=200ms' -count=3
        if ($LASTEXITCODE -ne 0) {throw 'benchmarks failed'}
    }
    if ($Race) {
        & go test -race ./... -count=1
        if ($LASTEXITCODE -ne 0) {throw 'race tests failed; use WSL with Go 1.24.1 and gcc if native CGO is unavailable'}
    }
} finally {
    $env:GO111MODULE=$oldModule
    Pop-Location
}
