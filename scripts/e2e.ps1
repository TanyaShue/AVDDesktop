#!/usr/bin/env pwsh
# ============================================================================
# AVDDesktop 端到端测试运行脚本
#
# 用法：
#   .\scripts\e2e.ps1
#
# 说明：
#   - 走真实服务层 + 真网络 + 真磁盘：首次运行会下载软件自带 JDK（约 200 MB）与官方命令行工具（约 150 MB），
#     并安装 platform-tools / emulator 到软件自己的目录
#   - 软件根目录由 AVDDESKTOP_E2E_HOME 指定（默认 %LOCALAPPDATA%\AVDDesktop\e2e），
#     与真实使用中的软件目录隔离；重复运行会复用已下载内容
#   - 不需要系统 JDK（软件会自动下载 Temurin 21）；需要网络
# ============================================================================
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
$previousE2EHome = $env:AVDDESKTOP_E2E_HOME
try {
    if (-not $env:AVDDESKTOP_E2E_HOME) {
        # 跨平台取用户数据目录：pwsh 在 macOS/Linux 上同样可用。
        $localAppData = [System.Environment]::GetFolderPath("LocalApplicationData")
        if (-not $localAppData) { $localAppData = [System.IO.Path]::GetTempPath() }
        $env:AVDDESKTOP_E2E_HOME = [System.IO.Path]::Combine($localAppData, "AVDDesktop", "e2e")
    }

    Write-Host "=== AVDDesktop E2E ===" -ForegroundColor Cyan
    Write-Host ("  软件根目录: {0}" -f $env:AVDDESKTOP_E2E_HOME)
    Write-Host ""

    # 全局超时必须大于各步骤预算之和（45m×3 + 60m×2 = 255m），
    # 否则单步预算还没到期，测试二进制就先被全局超时杀死。
    & go test -tags e2e -count=1 -timeout 300m ./internal/e2e/ -v
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
    # 还原调用方会话的环境变量：脚本内设置不应污染当前终端。
    $env:AVDDESKTOP_E2E_HOME = $previousE2EHome
}
