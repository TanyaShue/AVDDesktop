# 预览截图：用 Edge headless 抓取 preview.html 的关键界面，用于开发期视觉走查。
#
# 前置：另开一个终端跑 `cd frontend; npx vite --port 5199 --strictPort`
# 用法：
#   pwsh -File frontend/tools/ui-shots.ps1 -Prefix after
#   pwsh -File frontend/tools/ui-shots.ps1 -Prefix after -Sizes 1440x900,1180x760,960x640
#   pwsh -File frontend/tools/ui-shots.ps1 -Base http://localhost:5199/device-preview.html -Prefix device -Only ""
param(
    [string]$Base = "http://localhost:5199/preview.html",
    [string]$Out = "",
    [string]$Prefix = "shot",
    [int]$Width = 1440,
    [int]$Height = 900,
    [int]$Budget = 9000,
    # 多个窗口尺寸，形如 "1440x900,1180x760,960x640"；给了 -Sizes 就以它为准，
    # 文件名变成 <prefix>-<W>x<H>-<state>.png。主窗口 MinWidth 是 960（main.go），
    # 所以每次改布局至少要覆盖 1440 / 1180（默认宽）/ 960（最小宽）。
    [string]$Sizes = "",
    # 只抓指定状态，逗号分隔（留空表示全部；传 "page" 表示只抓一次无查询参数的首页，
    # 用于设备窗口预览这类没有查询参数驱动的入口）
    [string]$Only = ""
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
    @{ name = "devices";              query = "?noanim=1" },
    @{ name = "devices-empty-env";    query = "?env=missing&noanim=1" },
    @{ name = "devices-drawer";       query = "?drawer=1&noanim=1" },
    @{ name = "devices-dark";         query = "?theme=dark&noanim=1" },
    @{ name = "devices-compact";      query = "?view=compact&noanim=1" },
    @{ name = "devices-sort";         query = "?sort=1&noanim=1" },
    @{ name = "devices-menu";         query = "?menu=1&noanim=1" },
    @{ name = "devices-search";       query = "?q=playstore&noanim=1" },
    @{ name = "devices-no-match";     query = "?q=zzz-none&noanim=1" },
    @{ name = "devices-toast";        query = "?toast=1&noanim=1" },
    @{ name = "wizard";               query = "?dialog=wizard&noanim=1" },
    # 设备设置：dev 是设备在列表中的序号（列表按名称排序：Broken / Nexus / Pixel / Tablet）
    @{ name = "devset";               query = "?dialog=devset&dev=3&noanim=1" },
    @{ name = "devset-running";       query = "?dialog=devset&dev=2&noanim=1" },
    @{ name = "devset-custom";        query = "?dialog=devset&dev=1&custom=1&noanim=1" },
    @{ name = "devset-broken";        query = "?dialog=devset&dev=0&noanim=1" },
    @{ name = "devset-dark";          query = "?dialog=devset&dev=3&theme=dark&noanim=1" },
    @{ name = "settings";             query = "?page=settings&noanim=1" },
    @{ name = "settings-dark";        query = "?page=settings&theme=dark&noanim=1" },
    @{ name = "env-setup";            query = "?page=settings&dialog=env&noanim=1" },
    @{ name = "env-setup-dark";       query = "?page=settings&dialog=env&theme=dark&noanim=1" },
    @{ name = "mirror";               query = "?page=settings&dialog=mirror&noanim=1" },
    @{ name = "mirror-dark";          query = "?page=settings&dialog=mirror&theme=dark&noanim=1" },
    @{ name = "images";               query = "?page=settings&dialog=images&noanim=1" },
    @{ name = "images-dark";          query = "?page=settings&dialog=images&theme=dark&noanim=1" },
    @{ name = "wizard-dark";          query = "?dialog=wizard&theme=dark&noanim=1" }
)

if ($Only) {
    $wanted = @($Only -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ })
    $states = @($states | Where-Object { $wanted -contains $_.name })
    if ($states.Count -eq 0) { $states = @(@{ name = "page"; query = "" }) }
}

# 尺寸列表：默认单档（兼容原有调用）
$viewports = @(@{ w = $Width; h = $Height; tag = ""; multi = $false })
if ($Sizes) {
    $viewports = @()
    foreach ($item in ($Sizes -split ",")) {
        if ($item -match '^\s*(\d+)\s*[xX]\s*(\d+)\s*$') {
            $viewports += @{ w = [int]$Matches[1]; h = [int]$Matches[2]; multi = $true }
        }
    }
    if ($viewports.Count -eq 0) { throw "无法解析 -Sizes：$Sizes（期望形如 1440x900,960x640）" }
}

$profile = Join-Path $env:TEMP "avddesktop-ui-shot"

foreach ($vp in $viewports) {
    foreach ($state in $states) {
        $name = if ($vp.multi) { "$Prefix-$($vp.w)x$($vp.h)-$($state.name)" } else { "$Prefix-$($state.name)" }
        $file = Join-Path $Out "$name.png"
        if (Test-Path $file) { Remove-Item $file -Force }
        $url = "$Base$($state.query)"
        & $edge --headless=new --disable-gpu --hide-scrollbars --no-first-run --no-default-browser-check `
            --user-data-dir="$profile" --window-size="$($vp.w),$($vp.h)" --virtual-time-budget=$Budget `
            --screenshot="$file" $url 2>$null | Out-Null
        if (Test-Path $file) {
            Write-Host ("OK   {0}  <- {1}" -f (Split-Path $file -Leaf), $url)
        } else {
            Write-Host ("FAIL {0}  <- {1}" -f (Split-Path $file -Leaf), $url)
        }
    }
}

Write-Host "输出目录：$Out"
