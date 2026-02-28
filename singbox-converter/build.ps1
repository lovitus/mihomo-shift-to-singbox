# Cross-platform build script for singbox-converter
$ErrorActionPreference = "Stop"
$binary = "singbox-converter"

New-Item -ItemType Directory -Force -Path dist | Out-Null

$targets = @(
    @{ GOOS="windows"; GOARCH="amd64"; ext=".exe" },
    @{ GOOS="linux";   GOARCH="amd64"; ext="" },
    @{ GOOS="linux";   GOARCH="arm64"; ext="" },
    @{ GOOS="darwin";  GOARCH="amd64"; ext="" },
    @{ GOOS="darwin";  GOARCH="arm64"; ext="" }
)

foreach ($t in $targets) {
    $outName = "dist/${binary}-$($t.GOOS)-$($t.GOARCH)$($t.ext)"
    Write-Host "Building $outName ..."
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    go build -ldflags "-s -w" -o $outName .
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Build failed for $($t.GOOS)/$($t.GOARCH)"
        exit 1
    }
}

# Reset env
Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue

Write-Host "`nAll builds complete:"
Get-ChildItem dist/ | Format-Table Name, @{N="Size(KB)";E={[math]::Round($_.Length/1024)}}
