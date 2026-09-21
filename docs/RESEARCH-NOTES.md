# 调研与实测记录（设计依据）

> 本文记录架构决策所依赖的**实测数据**。测量环境：Windows（本机），PowerShell 7，Go 1.26，Wails v2.16，Node 24。
> 标注 **[实测]** 的为本地真实验证结果，**[文档]** 为官方文档结论。
> 若后续实现时发现与本文不符，请先更新本文再改代码。

---

## 1. 镜像源可用性（[实测]）

方法：`HEAD <base>/repository2-3.xml`（3 次取代表值）+ 对真实包文件 `HEAD`/`Range GET`。

| 源 | base URL | `repository2-3.xml` | 包文件 | 结论 |
| --- | --- | --- | --- | --- |
| **Google 官方** | `https://dl.google.com/android/repository/` | ✅ 200（TTFB ≈ 1227 ms） | ✅ 支持 Range（206） | 可用，作兜底源 |
| **腾讯云** | `https://mirrors.cloud.tencent.com/AndroidSDK/` | ✅ 200（TTFB ≈ 317 ms） | ✅ `emulator-windows_x64-*.zip`、`platform-tools_r37.0.1-win.zip`、`commandlinetools-win-*_latest.zip`、`sys-img/google_apis*/x86_64-36.1_r04.zip` 均 200（≈ 106–360 ms） | **完整目录镜像，首选源** |
| 清华 TUNA | `https://mirrors.tuna.tsinghua.edu.cn/android/repository/` | ❌ 403 | — | 当前不可用于 SDK 目录镜像 |
| 中科大 USTC | `https://mirrors.ustc.edu.cn/android/repository/` | ❌ 404 | — | 路径不存在 |
| 北外 BFSU | `https://mirrors.bfsu.edu.cn/android/repository/` | ❌ 403 | — | 不可用 |
| 东软 | `https://mirrors.neusoft.edu.cn/android/repository/` | ❌ TLS 握手失败 | — | 证书/协议问题 |
| 阿里云 | `https://mirrors.aliyun.com/android.googlesource.com/` | ✅ 200（但内容为 **AOSP 源码**镜像） | — | **不是** SDK 包源，仅可用于 AOSP 源码 |
| 华为云 | `https://repo.huaweicloud.com/android/repository/` | ❌ 404 | — | 不存在 |
| 南大 NJU | `https://mirrors.nju.edu.cn/android/repository/` | ❌ 302 → 目标不可用 | — | 不可用 |

**关键结论**

1. 可用的 SDK 包源目前只有 **Google 官方** 与 **腾讯云**；因此产品必须内建"自定义源 + 兼容性探测"，不能假设存在多个国内镜像。
2. 腾讯云镜像与本机 `repository2-3.xml` **字节数完全相同（415 919 B）**，且 `sys-img/<tag>/sys-img2-3.xml`、`addon2-3.xml`、`repository2-1.xml` 均在 → 说明它是 `dl.google.com/android/repository/` 的**整目录镜像**，可直接做 base URL 替换。
3. 镜像可用性会随时间变化 → 内置源表需要「可更新/可禁用」策略，且测速结果应缓存（TTL 1 小时）并标注测量时间。

### 1.1 Range / 断点续传能力

- Google 官方：返回 **206 Partial Content** + `Accept-Ranges: bytes`，实测下载 1 MiB 用时 1732 ms ≈ **0.58 MB/s**（本机到官方源带宽受限）。
- 腾讯云：`Range` 请求在部分大文件上会**挂起直到超时**（实测 `sys-img` 包 30 s 超时）→ 说明
  1. 测速与下载**必须**支持"不支持 Range"的降级路径：顺序 GET + 读满 N MB 后主动 abort、按已写字节续传；
  2. 单次读取要有硬超时，不能假设服务端会快速返回 206。

> 设计影响：`internal/download` 需要 3 种模式 —— `range-parallel` / `range-sequential` / `plain-with-resume`，并在测速时探测后记录 `rangeSupported`。

---

## 2. 仓库索引（repository XML）（[实测]）

### 2.1 文件与覆盖范围

| 文件 | 条目数（实测） | URL 基准 |
| --- | --- | --- |
| `repository2-3.xml` | 315 个 `<remotePackage>`（`cmdline-tools`×26、`platforms`×56、`build-tools`×82、`emulator`、`emulators`×3、`platform-tools`、extras…） | 仓库根 |
| `sys-img/google_apis/sys-img2-3.xml` | 90 个 `<remotePackage>`（每个 API × ABI 一条） | **该 XML 所在目录** |
| `sys-img/google_apis_playstore/sys-img2-3.xml` | 存在（腾讯云同样镜像） | 同上 |
| `sys-img/android-tv/sys-img2-3.xml` | 存在 | 同上 |
| `addon2-3.xml` | 存在 | 仓库根 |

> 注意：仓库根**不存在** `sys-img2-3.xml`（404）。系统镜像必须按 tag 分别取 `<repo>/sys-img/<tag>/sys-img2-3.xml`。
> 可用 tag 目录实测（腾讯云）：`google_apis`、`google_apis_playstore`、`android-tv`；另有 `aosp_atd`、`google_atd`、`android-desktop`、`android-automotive*`、`android-wear`、`android-xr` 等（按需探测）。

### 2.2 包条目结构（实测片段）

```xml
<!-- repository2-3.xml -->
<remotePackage path="emulator">
  <revision><major>37</major><minor>2</minor><micro>10</micro></revision>
  <display-name>Android Emulator</display-name>
  <uses-license ref="android-sdk-license"/>
  <archives>
    <archive>
      <complete>
        <size>455342191</size>
        <checksum type="sha1">99e809fc3e5e13bd5e552de24c7de79c6f911027</checksum>
        <url>emulator-windows_x64-16349944.zip</url>       <!-- 相对路径 -->
      </complete>
      <host-os>windows</host-os>
      <host-arch>x64</host-arch>
    </archive>
    <!-- linux / darwin_x64 / darwin_aarch64 同理 -->
  </archives>
</remotePackage>
```

实测关键包（抓取时刻）：

| 包 | revision | Windows 归档 | size | sha1 |
| --- | --- | --- | --- | --- |
| `emulator` | 37.2.10 | `emulator-windows_x64-16349944.zip` | 455 342 191 | `99e809fc…` |
| `cmdline-tools;latest` | 23.0 | `commandlinetools-win-16111833_latest.zip` | 154 957 218 | `57d04f2d…` |
| `platform-tools` | 37.0.1 | `platform-tools_r37.0.1-win.zip` | 8 044 989 | `e03e78b1…` |
| `system-images;android-36.1;google_apis;x86_64` | 4 | `x86_64-36.1_r04.zip`（基准 = `sys-img/google_apis/`） | 1 960 532 087 | `15261872…` |

### 2.3 解析要点

- 归档选择：`emulator` / `cmdline-tools` 有 `<host-os>`（`windows|macosx|linux`）与可选 `<host-arch>`（`x64|aarch64`）；**系统镜像归档没有 `<host-os>`**，其 URL 直接可用（按 tag 目录下载即可）。
- 依赖：`<dependency path="emulator#35.4.9"/>` 形式（系统镜像依赖 emulator 最低版本），需递归解析但不必强制满足（仅提示）。
- 频道：`<channel id="stable|beta|canary">`，`sdkmanager --channel` 对应关系；默认 stable。
- 安装后 SDK 目录会写入 `source.properties`（含 `Pkg.Revision`、`Pkg.Path`），这是本地"已安装包"扫描的权威来源（实测样例见 §4.2）。

---

## 3. 许可（licenses）（[实测]）

`licenses/<licenseId>` 内容 = 哈希（首行为空行）。实测本机 `%LOCALAPPDATA%\Android\Sdk\licenses\`：

| 文件（license id） | 内容 |
| --- | --- |
| `android-sdk-license` | `24333f8a63b6825ea9c5514f83c2829b004d1fee` |
| `android-sdk-preview-license` | `84831b9409646a918e30573bab4c9c91346d8abd` |
| `android-sdk-arm-dbt-license` | `859f317696f67ef3d7f30a50a5560e7834b43903` |
| `android-googletv-license` | `601085b94cd77f0b54ff86406957099ebe79c4d6` |
| `android-googlexr-license` | `ceff83576aac4f7f37cb98fe189e9fb3c49d3b81` |
| `google-gdk-license` | `33b6a2b64607f11b759f320ef9dff4ae5c47d97a` |
| `mips-android-sysimage-license` | `e9acab5b5fbb560a72cfaecce8946896ff6aab9d` |

> 尝试直接从 `repository2-3.xml` 的 `<license>` 文本计算 SHA-1 得到 `efa68a6b…`（≠ 上述值），说明**不能**用"文本哈希"复现，必须使用常量表（或从真实的 `licenses/` 目录读取）。→ 设计上用常量表 + UI 展示许可原文。

---

## 4. CLI 输出格式（[实测]，用于解析器设计）

### 4.1 `avdmanager`

```text
$ avdmanager list device -c          # 紧凑：每行一个 id（适合下拉框数据源）
ai_glasses_device
automotive_1024p_landscape
…
desktop_large
desktop_medium
medium_phone
medium_tablet
Nexus 10
…

$ avdmanager list device             # 详细：块状，--------- 分隔
Available devices definitions:
id: 0 or "ai_glasses_device"
    Name: AI Glasses
    OEM : Google
    Tag : ai-glasses
---------
id: 1 or "automotive_1024p_landscape"
    Name: Automotive (1024p landscape)
    OEM : Google
    Tag : android-automotive
---------

$ avdmanager list avd
Available Android Virtual Devices:
    Name: Medium_Phone_API_36.1
  Device: medium_phone (Generic)
    Path: C:\Android\.android\avd\Medium_Phone.avd
  Target: Google Play (Google Inc.)
          Based on: Android 16.0 ("Baklava") Tag/ABI: google_apis_playstore/x86_64
    Skin: 1080x2400
  Sdcard: 512M

$ avdmanager create avd              # 参数（官方 usage 摘录）
  -c --sdcard  : Path to a shared SD card image, or size of a new sdcard
  -g --tag     : The sys-img tag to use for the AVD
  -p --path    : Directory where the new AVD will be created
  -k --package : Package path of the system image (e.g. 'system-images;android-19;google_apis;x86')
  -n --name    : Name of the new AVD [required]
     --skin    : The optional name of a skin to use with this device
  -f --force   : Forces creation (overwrites an existing AVD)
  -b --abi     : The ABI to use for the AVD
  -d --device  : The optional device definition to use (index or id)
```

**解析要点**
- `list device -c` 只给 id，缺元数据；`list device` 给 `Name/OEM/Tag`。二者取其一即可（推荐解析详细格式，附带用内置档案补充分辨率/密度）。
- 当前 cmdline-tools 的设备档案里出现了 `ai_glasses_device`、`desktop_large/medium` 等新档案 → 分类映射不宜写死，需按 `Tag` 前缀 + 名称关键字做启发式分类，未知归"其他"。
- `create avd` 是交互式（询问是否自定义硬件配置）→ 必须处理 stdin（`echo no` 或 Go 侧写 `"no\n"`）。
- **`avdmanager` 已标注 deprecated**（[文档] 官方建议改用 Android CLI 的 `android emulator` 命令）→ 支撑设计中的 `AvdBackend` 抽象。

### 4.2 `source.properties`（本地包版本，实测）

```ini
# system-images\android-36.1\google_apis_playstore\x86_64\source.properties
Pkg.Desc=System Image x86_64 with Google Play.
Pkg.Revision=4
Pkg.Dependencies=emulator#35.4.9
AndroidVersion.ApiLevel=36.1
AndroidVersion.ExtensionLevel=20
AndroidVersion.IsBaseSdk=true
SystemImage.Abi=x86_64
SystemImage.TagId=google_apis_playstore
SystemImage.TagDisplay=Google Play
SystemImage.GpuSupport=true
Addon.VendorId=google
Addon.VendorDisplay=Google Inc.
```

```ini
# emulator\source.properties
Pkg.UserSrc=false
Pkg.Revision=36.3.10
Pkg.Path=emulator
Pkg.Desc=Android Emulator
Pkg.BuildId=14472402
```

> 注意：`Pkg.Path` 在 `cmdline-tools/latest/source.properties` 里是 `cmdline-tools;20.0`（**不是** `latest`），所以"已安装包路径"不能只靠 `Pkg.Path` 反推目录，需按目录结构映射后再用 `Pkg.Path` 修正显示名。
>
> 修正（实测，详见 §10）：`source.properties` 只能说明"目录里有这个包"，**官方工具（sdkmanager /
avdmanager / Android Studio）判定"包已安装"的依据是包目录下的 `package.xml`**。两者必须同时存在：
> 本项目的扫描/展示用 `source.properties`，写入 / 自愈用 `package.xml`（`internal/sdk/localrepo`）。

### 4.3 `emulator`

```text
$ emulator -accel-check
accel:
0
WHPX(10.0.26200) is installed and usable.
accel
```
（退出码 0 = 可用；`0` 行是状态码，第三行为人类可读信息）

```text
$ emulator -version
Android emulator version 36.3.10.0 (build_id 14472402) (CL:N/A)
```

`emulator -help` 实测可用参数（节选，用于启动参数面板）：

- 设备/目录：`-avd`、`-sysdir`、`-datadir`、`-list-avds`、`-avd-arch`
- 资源：`-memory`、`-cores`、`-partition-size`、`-cache-size`、`-no-cache`、`-sdcard`
- 快照：`-snapshot`、`-no-snapshot`、`-no-snapshot-load`、`-no-snapshot-save`、`-force-snapshot-load`、`-snapshot-list`、`-wipe-data`、`-no-snapstorage`、`-snapstorage`
- 图形/窗口：`-gpu`（`auto|host|swiftshader_indirect|angle_indirect|guest`）、`-no-window`、`-qt-hide-window`、`-skin`、`-no-boot-anim`、`-ui-only`
- 加速：`-accel <mode>`、`-no-accel`、`-engine classic|qemu2`
- 网络：`-netspeed`、`-netdelay`、`-netfast`、`-dns-server`、`-http-proxy`、`-net-tap`
- 系统：`-writable-system`、`-delay-adb`、`-timezone`、`-change-locale`、`-change-language`、`-logcat`、`-logcat-output`、`-shell`、`-show-kernel`
- 端口：`-port`、`-ports <console>,<adb>`
- 其它：`-quit-after-boot <s>`（可用于冒烟测试）、`-id`

### 4.4 `sdkmanager`（[文档] + 参考）

- `sdkmanager --list` / `--list_installed` / `--install <paths>` / `--uninstall` / `--licenses` / `--channel=<n>` / `--sdk_root=<dir>` / `--proxy=http --proxy_host= --proxy_port=`
- 输出为人类可读表格（列宽会截断）→ 不适合作为主数据源；本项目以「自解析 XML + 扫描 `source.properties`」为主，`sdkmanager` 仅作兜底。

---

## 5. AVD 目录与配置（[实测]）

### 5.1 AVD 主目录解析

本机同时存在：

```
ANDROID_HOME    = C:\Users\black\AppData\Local\Android\Sdk
ANDROID_SDK_HOME= C:\Android              （HKCU\Environment 持久化）
%USERPROFILE%\.android\avd  → 空
C:\Android\.android\avd     → 实际存放 AVD（avdmanager list avd 也读这里）
```

**结论**：必须实现 §9.4 的完整优先级链，否则会"看不到"设备。

### 5.2 `.ini` 与 `config.ini`（真实样例）

```ini
# C:\Android\.android\avd\Medium_Phone_API_36.1.ini
avd.ini.encoding=UTF-8
path=C:\Android\.android\avd\Medium_Phone.avd
path.rel=avd\Medium_Phone.avd
target=android-36.1
```

```ini
# C:\Android\.android\avd\Medium_Phone.avd\config.ini（由 avdmanager 生成，实测）
AvdId=Medium_Phone_API_36.1
PlayStore.enabled=true
abi.type=x86_64
avd.ini.displayname=Medium Phone API 36.1
avd.ini.encoding=UTF-8
disk.dataPartition.size=6G
fastboot.forceColdBoot=no
fastboot.forceFastBoot=yes
hw.accelerometer=yes
hw.arc=false
hw.audioInput=yes
hw.battery=yes
hw.camera.back=virtualscene
hw.camera.front=emulated
hw.cpu.arch=x86_64
hw.cpu.ncore=8
hw.device.hash2=MD5:2016577e1656e8e7c2adb0fac972beea
hw.device.manufacturer=Generic
hw.device.name=medium_phone
hw.gpu.enabled=yes
hw.gpu.mode=auto
hw.keyboard=yes
hw.lcd.density=420
hw.lcd.height=2400
hw.lcd.width=1080
hw.ramSize=2048
hw.sdCard=yes
hw.sensors.*=yes
image.sysdir.1=system-images\android-36.1\google_apis_playstore\x86_64\
runtime.network.latency=none
runtime.network.speed=full
sdcard.size=512M
showDeviceFrame=yes
skin.dynamic=yes
skin.name=1080x2400
skin.path=1080x2400
tag.display=Google Play
tag.id=google_apis_playstore
target=android-36.1
vm.heapSize=336
```

> `hardware-qemu.ini` 由 emulator 首次启动时生成（运行时状态），**不要手写**。

### 5.3 硬件配置项参考

官方 `hardware-properties.ini`（qemu 源码）是 `config.ini` 键的权威定义来源，含类型/默认值/说明 →
`internal/avd/store/schema.go` 的中文标签、类型、默认值、范围应基于它整理（本项目已按分组归纳在 `ARCHITECTURE.md` §9.5）。

---

## 6. 宿主环境检查（[实测] 本机）

| 项 | 值 | 设计含义 |
| --- | --- | --- |
| Windows 版本（加速相关） | `WHPX(10.0.26200)` | Windows 11 24H2+，WHPX 可用 |
| `emulator -accel-check` | 退出码 0，`WHPX is installed and usable.` | 权威判定手段，UI 直接消费其输出 |
| JDK | Zulu 17.0.6（`C:\Program Files\Zulu\zulu-17`），**PATH 中可用** | 需要 ≥17（cmdline-tools 12+ 要求） |
| `adb` / `emulator` / `sdkmanager` | **不在 PATH** | 软件必须自己解析绝对路径并注入子进程环境，不能依赖 PATH |
| SDK 已装包 | cmdline-tools;20.0、platform-tools 36.0.2、emulator 36.3.10、system-images(android-34 android-desktop / android-36.1 google_apis_playstore) | "已安装/可更新"判定需要在有真实 SDK 的机器上验证 |
| 现有 AVD | 1 个（`Medium_Phone_API_36.1`） | 有现成样本可用于解析器测试 |

**加速方案说明（[文档]）**：官方推荐 Windows 用 **WHPX**（需 Windows 10 1803+，且需开启"Windows 虚拟机监控程序平台"功能）；**AEHD**（原 HAXM 的继任者，`extras;google;Android_Emulator_Hypervisor_Driver`）将于 **2026-12-31 停止支持** → UI 引导文案应优先 WHPX，AEHD 作为备选（且与 Hyper-V 同时启用会冲突）。

---

## 7. 待确认 / 实现时需复测的问题

| # | 问题 | 验证方式 |
| --- | --- | --- |
| 1 | 腾讯云镜像对**新发布**包的同步延迟（如 emulator 新版本当天） | 对比 `dl.google.com` 与镜像的 `repository2-3.xml` ETag/内容哈希 + 包文件 200/404 |
| 2 | 各镜像对 `Range` 的"伪支持"（返回 206 但实际全量传输） | 下载时统计实际流量（`Content-Length` vs 读取字节） |
| 3 | `cmdline-tools` bootstrap 后是否需要额外补文件（`source.properties`、`NOTICE.txt`） | 在空目录解压并与官方安装结果逐文件 diff |
| 4 | `emulator` 目录在实例运行时的**文件占用**范围（哪些 dll 被锁） | 运行实例后尝试替换 → 记录失败文件清单，用于"延迟替换"策略 |
| 5 | Android Studio 自带 `jbr` 的路径规则与版本（用于无 JDK 兜底） | 枚举 `%ProgramFiles%\Android\Android Studio\jbr`、`%LOCALAPPDATA%\Programs\Android Studio\jbr` |
| 6 | Android CLI（`android emulator create`）的现状与迁移时机 | 跟踪官方文档，作为 `AvdBackend` 的第三实现 |
| 7 | 系统镜像下载的**分片**可行性（腾讯云 Range 表现） | 分片下载实测；若不可用则单连接 + 断点续传 |
| 8 | `-quit-after-boot` 能否作为"启动冒烟测试"（CI/自检用） | 用现有 AVD 实测，记录耗时与退出码 |
| 9 | 多实例并发启动下的内存/IO 建议值（UI 默认值合理性） | 8 核 / 2 GB 默认值在多开时的实际表现 |
| 10 | 长路径（>260 字符）下 emulator 与系统镜像是否可用 | 构造深目录实测 |

---

## 8. 实现期补充实测（骨架代码验证后）

以下结论来自骨架实现完成后在真实机器上跑测试得到的**新数据**，其中部分修正了前文：

### 8.1 Range 支持（修正 §1.1）

对**小文件**（`platform-tools_r37.0.1-win.zip`，约 8 MB）做 `Range: bytes=0-1048575` 采样（`go test ./internal/mirror -run TestEngineLive`）：

| 源 | DNS | 连接 | TTFB | HTTP | Range | 吞吐（1 MiB 采样） | 评分 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 腾讯云 | 18–25 ms | 68–70 ms | 214–216 ms | **206** | ✅ 支持 | 5.6–6.4 MB/s | 73（推荐） |
| Google 官方 | 0–11 ms | 84–85 ms | 175–199 ms | **206** | ✅ 支持 | 5.4–5.6 MB/s | 69（可用） |

**修正 §1.1**：腾讯云**支持** Range（返回 206）。此前观察到的"Range 请求挂起"发生在**约 2 GB 的系统镜像**上，属于大文件传输本身慢 + 30 s 超时过短，而不是不支持 Range。因此：

- 下载器仍需保留"不支持 Range 时降级为顺序 + 按已写字节续传"的分支（已实现为三种模式，见 `internal/download`）
- 但不必因为镜像上次超时就降级，探测应以受控大小的样本（≤4 MiB，硬超时 8 s）为准

**另修正认知**：本机到**两个源的实际吞吐接近**（5–6 MB/s），延迟也互有高低。说明选源不能写死"国内镜像一定更快"，必须按测量结果评分 —— 这也是评分模型采用数据驱动的原因。

### 8.2 AVD 的 `.ini` 文件名 ≠ `.avd` 目录名（修正 §5.2）

实测本机样本：

```
C:\Android\.android\avd\Medium_Phone_API_36.1.ini      ← 文件名
   path=C:\Android\.android\avd\Medium_Phone.avd        ← 目录名不同！
```

**影响**：任何"按名称拼目录"的实现都会失败（本次实现的第一版就被测试抓到）。
正确做法：**先读 `.ini` 的 `path` / `path.rel` 字段定位目录**，读不到才退化为 `<name>.avd`。
已在 `internal/avd/store` 的 `Resolve()` 中按此实现，并有测试覆盖。

### 8.3 `PlayStore.enabled` 大小写不一致（补充 §5.2）

实测该键由 avdmanager 写成 `PlayStore.enabled=true`（大写 P、S），而部分文档/代码写作小写。解析必须**大小写不敏感**（已实现 `getCI()`）。

### 8.4 AEHD 的自描述文案不含"aehd"（补充 §6）

`emulator -accel-check` 在 AEHD 环境下的输出是：

```
Android Emulator hypervisor driver is installed and usable.
```

不含字面量 `aehd`，必须匹配全称 `Android Emulator hypervisor driver` 才能识别（已有单测覆盖该用例）。

### 8.5 `adb --version` 的版本行陷阱（补充 §4.3）

```
Android Debug Bridge version 1.0.41     ← 这是协议版本，不是 adb 版本
Version 36.0.2-12345678                 ← 这才是
Installed as C:\Users\...\adb.exe
```

解析必须取**行首为 `Version`** 的那一行（已修正并有单测）。另外 `platform-tools/source.properties` 的 `Pkg.Revision` 更可靠，应优先使用。

### 8.6 环境自检实跑结果（本机，验证产物）

```
SDK 根目录: C:\Users\black\AppData\Local\Android\Sdk（来源：环境变量 ANDROID_HOME）
JDK      : C:\Program Files\Zulu\zulu-17\bin\java.exe            → 17.0.6
AVD 目录 : C:\Android\.android\avd（来源：ANDROID_SDK_HOME/.android/avd，1 个设备，可写）
硬件加速 : available=true kind=whpx
磁盘     : 剩余 29 GB / 共 395 GB
  [jdk           ] present   17.0.6
  [cmdline-tools ] present   20.0
  [platform-tools] present   36.0.2
  [emulator      ] present   36.3.10.0
  [system-images ] present   2 个
  [disk          ] present   29 GB 可用
  [avd-home      ] present   1 个设备
ready=true 阻塞项=0
```

结论：路径解析链、版本读取、加速判定这三处最容易出错的地方均与官方工具的实际行为一致。

### 8.7 Windows 虚拟化能力探测（补充 §6）

通过 CIM（非管理员）读到的真实数据：

```json
{"hypervisorPresent":true,"virtFirmware":false,"slat":false,"vmm":false,
 "cpu":"13th Gen Intel(R) Core(TM) i5-13500H","caption":"Microsoft Windows 11 专业版","build":"26200"}
```

两个重要结论：

1. **`hypervisorPresent=true` 时，`virtFirmware/slat/vmm` 会报 false** —— hypervisor 已接管 CPU，
   根分区看不到这些能力位。不能据此判定“不支持虚拟化”，否则会给出完全错误的 BIOS 引导。
   已在 `buildIssues` 中区分三种归因（已有 hypervisor 但 WHPX 未启用 / 无 hypervisor / BIOS 未开 VT-x）。
2. **PowerShell 5.1 在中文 Windows 默认用 GBK 输出**，`ConvertTo-Json` 的结果不是合法 UTF-8，
   会把 “Windows 11 专业版” 变成乱码。必须在脚本里显式设置
   `[Console]::OutputEncoding=[Text.Encoding]::UTF8`，并在解析后过滤非法字节。

另外实测本机 **未开启长路径支持**（`LongPathsEnabled=0`），因此自检会给出 `reg add` 修复命令。

### 8.8 自检性能优化（实测）

| 阶段 | 耗时 | 原因 |
| --- | --- | --- |
| 初版 | 13.3 s | AVD 健康检查遍历数 GB 的设备数据目录统计大小 |
| + 目录大小缓存与 `WithSize=false` 快路径 | 7.2 s | 自检不再遍历大目录 |
| + Windows 探测移入并行段 | **5.2 s** | CIM 查询（约 3s）与其它检测重叠 |
| 二次调用（TTL / 单飞） | < 5 ms | 命中缓存；并发调用只执行一次扫描 |

---

## 9. 回归测试发现并修复的缺陷

首次完整跑通时，E2E 套件（`internal/e2e`，构建标签 `e2e`）与运行时日志共抓出 5 个真实缺陷；
用户实测又发现 1 个（#6），全部已修复并补了单元测试：

| # | 缺陷 | 现象 | 修复 |
| --- | --- | --- | --- |
| 1 | 系统镜像目录拼接丢前缀 | `android-34` → 拼成 `system-images/34/...`，创建设备时误报“镜像未安装” | 区分展示用 `api=34` 与目录段 `android-34`（`SystemImageDir`） |
| 2 | 官方归档包装目录未剥离 | `platform-tools` 解压成 `<sdk>/platform-tools/platform-tools/adb.exe`，安装后找不到 adb | 按所有条目共同顶层目录剥离（与 sdkmanager 一致），并拒绝把 `../` 当作可剥离前缀 |
| 3 | `ANDROID_AVD_HOME` 指到父目录 | avdmanager 把新建 AVD 放到错误位置，工具随即“看不到”设备 | 指向 avd 目录本身 |
| 4 | 后端选择忽略显式开关 | `createWithAvdManager=false` 仍走 avdmanager（无 JDK 环境会失败） | 显式关闭时强制使用直写后端 |
| 5 | 并发自检重复扫描（由运行时日志发现，非 E2E） | 启动推送与前端请求同时触发两次完整扫描（日志中出现两条“开始环境自检”） | 单飞（single-flight）+ TTL 缓存 |
| 6 | 本地包元数据 `package.xml` 丢失（用户实测发现：创建设备失败） | 安装器整体替换包目录后官方工具认为包未安装，`avdmanager create avd` 报 `Error: "emulator" package must be installed!` | 安装时补写元数据 + 创建前自愈（详见 §10） |

> 结论：路径拼接、归档布局、环境变量语义这三类问题几乎不可能靠代码审阅发现，
> 必须用真实工具与真实归档跑一遍。这也是把 E2E 作为独立阶段交付的原因。

---

## 10. SDK 本地包元数据 `package.xml`（实测）

### 10.1 现象与根因

在本机（真实 SDK + 真实 AVD 目录）创建设备时 avdmanager 失败：

```
[avdmanager] Loading local repository...
Auto-selecting single ABI x86_64
[avdmanager] Error: "emulator" package must be installed!
```

而 `<sdk>/emulator/emulator.exe` 实际存在、`emulator -avd …` 也能正常启动。根因在于**官方
工具判断“包是否已安装”的依据是包目录下的 `package.xml`，而不是 `source.properties`**
（`source.properties` 只是版本载体）：

| 证据 | 结果 |
| --- | --- |
| `emulator/` 有 `source.properties`（Pkg.Revision=37.1.11）但无 `package.xml` | `sdkmanager --list_installed` **不列** emulator；avdmanager 报错 |
| `platform-tools/` 同时有 `package.xml` | `sdkmanager --list_installed` 正常列出 |
| `cmdline-tools/latest/` 只有 `source.properties` | 同样不被列出 |
| 系统镜像目录缺 `package.xml` | `avdmanager create avd` 报 `Package path is not valid. Valid system image paths are: …`（`-k` 的路径要过 `getLocalPackage` 校验） |

`avdmanager` 的实现（`com.android.sdklib.tool.AvdManagerCli`）在创建 AVD 前调用
`EmulatorPackages.getEmulatorPackage()`，它内部是 `AndroidSdkHandler.getLocalPackage("emulator")`；
后者读的是本地仓库元数据（每个包目录的 `package.xml`）。取不到就抛出上面那句错误。

为什么元数据会丢：本项目安装器（`internal/sdk/install`）直接解压官方归档并**整体替换包目录**，
而官方归档里并不含 `package.xml`（该文件由 sdkmanager / Android Studio 在安装后写入）。
日志里的时间线可以复现：15:50 下载 emulator 37.1.11 → 15:52 替换目录完成 → 与
`package.xml` 一起被替换掉 → 此后 avdmanager 再也看不到模拟器包。

### 10.2 修复

1. **安装即补写**：`internal/sdk/localrepo` 复刻官方写入器的格式（固定命名空间 `ns2…ns18`、
   `xsi:type` 归一到所属 schema 族的最新版本、`type-details` 从索引原始 XML 透传），
   安装器在原子替换**之前**把 `package.xml` 写进临时目录，与包内容一起生效。
2. **创建前自愈**：avdmanager 后端检测 `emulator` 与**所选系统镜像**的 `package.xml`，
   缺哪个补哪个（通用包与系统镜像可完全离线从 `source.properties` 补写，
   `platforms` / `sources` 回退到仓库索引）。
3. **可观测**：自检新增问题项「SDK 包元数据缺失（package.xml）」；`VerifyPackage` 也会指出该缺失；
   avdmanager 仍失败时错误提示直接指向缺失文件。

### 10.3 补写元数据时踩到的两个坑（实测）

自愈本身也不是“拼个 XML 就完事”，两个坑都能让官方工具静默丢包：

| # | 坑 | 现象 | 处理 |
| --- | --- | --- | --- |
| 1 | `<uses-license ref="…">` 引用的 id **必须在同一个文件里定义** | `Warning: package.xml parsing problem. 未知 ID "android-sdk-license"` → 该包被丢弃，创建仍失败 | 写入时把索引里的许可文本一并写进根元素的 `<license id=… type="text">…</license>`（与官方文件一致） |
| 2 | 系统镜像的 `type-details` **必须有 `<abis>`** | `avdmanager create avd` 只打印一行 `null` 并返回 1（sdklib 空消息 NPE，`SystemImage` 拿不到 ABI 列表） | 从主 ABI 自动补 `<abis>x86_64</abis>`；镜像站索引用旧版 sys-img2 schema 时正好缺这个字段 |

顺带实测：`<translatedAbis>` 缺失不影响创建（去掉后仍成功）；
系统镜像的 `type-details` 可以完全由本地 `source.properties` 重建
（`AndroidVersion.ApiLevel` / `SystemImage.TagId` / `SystemImage.Abi` / `Addon.VendorId` …），
因此镜像的自愈不需要联网，也不再依赖镜像站索引的完整性。

验收（本机实测）：删掉 `<sdk>/emulator/package.xml` 与所选镜像的 `package.xml` 后创建，
日志显示 `已补写 1 个组件的 SDK 元数据 package.xml`，avdmanager
（`Auto-selecting single ABI x86_64`）返回 0 并生成设备目录；
`sdkmanager --list_installed` 里 `emulator | 37.1.11 | Android Emulator`
与 `cmdline-tools;latest | 20 | Android SDK Command-line Tools` 均正常列出。
