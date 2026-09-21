#!/usr/bin/env pwsh
# ============================================================================
# AVDDesktop 端到端测试运行脚本
#
# 用法：
#   .\scripts\e2e.ps1
#
# 说明：
#   - 走真实服务层 + 真网络 + 真磁盘：首次运行会下载官方命令行工具（约 150 MB），
#     并安装 platform-tools / emulator 到软件自己的 SDK 目录
#   - 软件根目录由 AVDDESKTOP_E2E_HOME 指定（默认 %LOCALAPPDATA%\AVDDesktop\e2e），
#     与真实使用中的软件目录隔离；重复运行会复用已下载内容
#   - 需要 JDK（sdkmanager / avdmanager 依赖）与网络
# ============================================================================
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    if (-not $env:AVDDESKTOP_E2E_HOME) {
        $env:AVDDESKTOP_E2E_HOME = Join-Path $env:LOCALAPPDATA "AVDDesktop\e2e"
    }

    Write-Host "=== AVDDesktop E2E ===" -ForegroundColor Cyan
    Write-Host ("  软件根目录: {0}" -f $env:AVDDESKTOP_E2E_HOME)
    Write-Host ""

    & go test -tags e2e -count=1 -timeout 60m ./internal/e2e/ -v
    $code = $LASTEXITCODE

    if ($code -eq 0) {
        Write-Host "`nE2E 通过" -ForegroundColor Green
    } else {
        Write-Host "`nE2E 失败（退出码 $code）" -ForegroundColor Red
    }
    exit $code
}
finally {
    Pop-Location
}
