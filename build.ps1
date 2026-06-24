# 构建 + UPX 压缩
$ErrorActionPreference = "Stop"

$upx = "$env:USERPROFILE\upx\upx-4.2.4-win64\upx.exe"
$bin = "build\bin\wireguard.exe"

Write-Host "=== wails build ===" -ForegroundColor Cyan
wails build
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

if (Test-Path $upx) {
    $before = (Get-Item $bin).Length
    Write-Host "=== UPX 压缩 ===" -ForegroundColor Cyan
    & $upx --best $bin
    if ($LASTEXITCODE -eq 0) {
        $after = (Get-Item $bin).Length
        Write-Host "压缩完成: $([math]::Round($before/1MB,2)) MB → $([math]::Round($after/1MB,2)) MB ($([math]::Round(($before-$after)/$before*100,1))%)" -ForegroundColor Green
    }
} else {
    Write-Host "UPX 未安装，跳过压缩" -ForegroundColor Yellow
}
