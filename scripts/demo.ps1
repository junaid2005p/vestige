$ErrorActionPreference = 'Stop'
$root = Join-Path $PSScriptRoot '..\demo-work'
$source = Join-Path $root 'source'
$repo = Join-Path $root 'repo'
$restore = Join-Path $root 'restore'
New-Item -ItemType Directory -Force -Path $source | Out-Null
Set-Content -NoNewline -Path (Join-Path $source 'hello.txt') -Value 'Vestige demo content'
go run ./cmd/vestige init $repo
go run ./cmd/vestige backup $source $repo
Set-Content -NoNewline -Path (Join-Path $source 'hello.txt') -Value 'Vestige demo content, updated'
go run ./cmd/vestige backup $source $repo
go run ./cmd/vestige snapshots $repo
go run ./cmd/vestige verify $repo
Write-Host "Demo repository: $repo"
