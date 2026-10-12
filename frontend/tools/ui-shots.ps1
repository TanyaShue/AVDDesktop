# 预览截图：用 Edge headless 抓取 preview.html 的关键界面，用于开发期视觉走查。
#
# 前置：另开一个终端跑 `cd frontend; npx vite --port 5199 --strictPort`
# 用法：pwsh -File frontend/tools/ui-shots.ps1 -Prefix after
param(
    [string]$Base = "http://localhost:5199/preview.html",
    [string]$Out = "",
    [string]$Prefix = "shot",
    [int]$Width = 1440,
    [int]$Height = 900,
    [int]$Budget = 9000
)

$ErrorActionPreference = "Stop"

$edge = @(
    "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe",
    "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe",
    "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
    "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe"
) | Where-Object { Test-Path $_ } | Select-Object -First 1

if (-not $edge) { throw "找不到 Edge/Chrome 可执行文件" }

if (-not $Out) { $Out = Join-Path $PSScriptRoot "..\..\build\ui-shots" }
$Out = [System.IO.Path]::GetFullPath($Out)
New-Item -ItemType Directory -Force -Path $Out | Out-Null

$states = @(
    @{ name = "devices";              query = "" },
    @{ name = "devices-empty-env";    query = "?env=missing" },
    @{ name = "devices-drawer";       query = "?drawer=1" },
    @{ name = "devices-dark";         query = "?theme=dark" },
    @{ name = "devices-compact";      query = "?view=compact" },
    @{ name = "devices-sort";         query = "?sort=1" },
    @{ name = "devices-menu";         query = "?menu=1" },
    @{ name = "devices-toast";        query = "?toast=1" },
    @{ name = "wizard";               query = "?dialog=wizard" },
    @{ name = "settings";             query = "?page=settings" },
    @{ name = "settings-dark";        query = "?page=settings&theme=dark" },
    @{ name = "env-setup";            query = "?page=settings&dialog=env" },
    @{ name = "mirror";               query = "?page=settings&dialog=mirror" },
    @{ name = "images";               query = "?page=settings&dialog=images" }
)

$profile = Join-Path $env:TEMP "avddesktop-ui-shot"

foreach ($state in $states) {
    $file = Join-Path $Out "$Prefix-$($state.name).png"
    if (Test-Path $file) { Remove-Item $file -Force }
    $url = "$Base$($state.query)"
    & $edge --headless=new --disable-gpu --hide-scrollbars --no-first-run --no-default-browser-check `
        --user-data-dir="$profile" --window-size="$Width,$Height" --virtual-time-budget=$Budget `
        --screenshot="$file" $url 2>$null | Out-Null
    if (Test-Path $file) {
        Write-Host ("OK   {0}  <- {1}" -f (Split-Path $file -Leaf), $url)
    } else {
        Write-Host ("FAIL {0}  <- {1}" -f (Split-Path $file -Leaf), $url)
    }
}

Write-Host "输出目录：$Out"
