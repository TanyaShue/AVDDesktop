#!/usr/bin/env pwsh
# ============================================================================
# AVDDesktop 端到端回归测试运行脚本
#
# 用法：
#   .\scripts\e2e.ps1                 # 轻量回归：自检 + 仓库索引 + 测速 + AVD 生命周期
#   .\scripts\e2e.ps1 -Heavy          # 追加：真实下载安装 cmdline-tools / platform-tools
#   .\scripts\e2e.ps1 -Boot           # 追加：真实启动模拟器并验证 adb
#   .\scripts\e2e.ps1 -All            # 全部（耗时最长，需要已安装系统镜像）
#   .\scripts\e2e.ps1 -Run TestE2E_RepoIndexAndPlan
#
# 说明：
#   - 所有用例使用临时目录，不会改动你真实的 SDK / AVD 目录（仅只读引用）
#   - 需要网络、JDK、系统镜像的用例在缺失时会自动跳过并说明原因
# ============================================================================
[CmdletBinding()]
param(
    [switch]$Heavy,
    [switch]$Boot,
    [switch]$All,
    [string]$Run = "",
    [string]$Source = "",
    [int]$TimeoutMinutes = 60,
    # 透传给 go test 的 -v（注意：-Verbose 是 PowerShell 内置参数，不能重用）
    [switch]$Detailed
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    if ($All) { $Heavy = $true; $Boot = $true }

    if ($Heavy) { $env:AVDDESKTOP_E2E_HEAVY = "1" } else { Remove-Item Env:AVDDESKTOP_E2E_HEAVY -ErrorAction SilentlyContinue }
    if ($Boot)  { $env:AVDDESKTOP_E2E_BOOT  = "1" } else { Remove-Item Env:AVDDESKTOP_E2E_BOOT  -ErrorAction SilentlyContinue }
    if ($Source) { $env:AVDDESKTOP_E2E_SOURCE = $Source } else { Remove-Item Env:AVDDESKTOP_E2E_SOURCE -ErrorAction SilentlyContinue }

    Write-Host "=== AVDDesktop E2E ===" -ForegroundColor Cyan
    Write-Host ("  下载安装用例 : {0}" -f $(if ($Heavy) { "启用" } else { "跳过" }))
    Write-Host ("  模拟器启动用例: {0}" -f $(if ($Boot)  { "启用" } else { "跳过" }))
    if ($Source) { Write-Host ("  镜像源       : {0}" -f $Source) }
    Write-Host ""

    $args = @("test", "-tags", "e2e", "./internal/e2e/", "-timeout", "$($TimeoutMinutes)m")
    if ($Detailed) { $args += "-v" }
    if ($Run) { $args += @("-run", $Run) }

    Write-Host ("go " + ($args -join " ")) -ForegroundColor DarkGray
    & go @args
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
