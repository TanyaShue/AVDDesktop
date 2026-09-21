export namespace domain {
	
	export class AccelInfo {
	    available: boolean;
	    kind: string;
	    raw: string;
	    hints?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AccelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.kind = source["kind"];
	        this.raw = source["raw"];
	        this.hints = source["hints"];
	    }
	}
	export class Action {
	    kind: string;
	    label: string;
	    payload?: string;
	
	    static createFrom(source: any = {}) {
	        return new Action(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.payload = source["payload"];
	    }
	}
	export class AdbDevice {
	    serial: string;
	    state: string;
	    product?: string;
	    model?: string;
	    device?: string;
	    transportId?: string;
	    isEmulator: boolean;
	    instanceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new AdbDevice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.state = source["state"];
	        this.product = source["product"];
	        this.model = source["model"];
	        this.device = source["device"];
	        this.transportId = source["transportId"];
	        this.isEmulator = source["isEmulator"];
	        this.instanceId = source["instanceId"];
	    }
	}
	export class AppError {
	    code: string;
	    message: string;
	    hint?: string;
	    detail?: string;
	    actions?: Action[];
	
	    static createFrom(source: any = {}) {
	        return new AppError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.hint = source["hint"];
	        this.detail = source["detail"];
	        this.actions = this.convertValues(source["actions"], Action);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SpeedResult {
	    sourceId: string;
	    at: number;
	    ok: boolean;
	    dnsMs: number;
	    resolveIPs?: string[];
	    connectMs: number;
	    ttfbMs: number;
	    httpStatus: number;
	    rangeSupported: boolean;
	    xmlOK: boolean;
	    hasCmdlineTools: boolean;
	    hasEmulator: boolean;
	    hasSystemImages: boolean;
	    throughputMBps: number;
	    jitterMs: number;
	    score: number;
	    grade: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeedResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceId = source["sourceId"];
	        this.at = source["at"];
	        this.ok = source["ok"];
	        this.dnsMs = source["dnsMs"];
	        this.resolveIPs = source["resolveIPs"];
	        this.connectMs = source["connectMs"];
	        this.ttfbMs = source["ttfbMs"];
	        this.httpStatus = source["httpStatus"];
	        this.rangeSupported = source["rangeSupported"];
	        this.xmlOK = source["xmlOK"];
	        this.hasCmdlineTools = source["hasCmdlineTools"];
	        this.hasEmulator = source["hasEmulator"];
	        this.hasSystemImages = source["hasSystemImages"];
	        this.throughputMBps = source["throughputMBps"];
	        this.jitterMs = source["jitterMs"];
	        this.score = source["score"];
	        this.grade = source["grade"];
	        this.error = source["error"];
	    }
	}
	export class MirrorSource {
	    id: string;
	    name: string;
	    baseURL: string;
	    kind: string;
	    grade: string;
	    enabled: boolean;
	    note?: string;
	    region?: string;
	    lastResult?: SpeedResult;
	
	    static createFrom(source: any = {}) {
	        return new MirrorSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.kind = source["kind"];
	        this.grade = source["grade"];
	        this.enabled = source["enabled"];
	        this.note = source["note"];
	        this.region = source["region"];
	        this.lastResult = this.convertValues(source["lastResult"], SpeedResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AppSettings {
	    version: number;
	    sdkRoot: string;
	    jdkPath: string;
	    avdHome: string;
	    injectEnvForChildren: boolean;
	    activeSourceId: string;
	    customSources: MirrorSource[];
	    autoFallbackToOfficial: boolean;
	    maxConnectionsPerFile: number;
	    maxParallelPackages: number;
	    timeoutSeconds: number;
	    speedLimitKBps: number;
	    proxyMode: string;
	    proxyURL: string;
	    downloadDir: string;
	    acceptedLicenseIds: string[];
	    autoAcceptLicenses: boolean;
	    defaultDeviceProfile: string;
	    defaultRamMB: number;
	    defaultCores: number;
	    defaultDataPartitionGB: string;
	    defaultGpuMode: string;
	    theme: string;
	    language: string;
	    deviceViewMode: string;
	    confirmBeforeDelete: boolean;
	    showTaskDrawer: boolean;
	    logLevel: string;
	    keepLogDays: number;
	    askBeforeDriverInstall: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.sdkRoot = source["sdkRoot"];
	        this.jdkPath = source["jdkPath"];
	        this.avdHome = source["avdHome"];
	        this.injectEnvForChildren = source["injectEnvForChildren"];
	        this.activeSourceId = source["activeSourceId"];
	        this.customSources = this.convertValues(source["customSources"], MirrorSource);
	        this.autoFallbackToOfficial = source["autoFallbackToOfficial"];
	        this.maxConnectionsPerFile = source["maxConnectionsPerFile"];
	        this.maxParallelPackages = source["maxParallelPackages"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.speedLimitKBps = source["speedLimitKBps"];
	        this.proxyMode = source["proxyMode"];
	        this.proxyURL = source["proxyURL"];
	        this.downloadDir = source["downloadDir"];
	        this.acceptedLicenseIds = source["acceptedLicenseIds"];
	        this.autoAcceptLicenses = source["autoAcceptLicenses"];
	        this.defaultDeviceProfile = source["defaultDeviceProfile"];
	        this.defaultRamMB = source["defaultRamMB"];
	        this.defaultCores = source["defaultCores"];
	        this.defaultDataPartitionGB = source["defaultDataPartitionGB"];
	        this.defaultGpuMode = source["defaultGpuMode"];
	        this.theme = source["theme"];
	        this.language = source["language"];
	        this.deviceViewMode = source["deviceViewMode"];
	        this.confirmBeforeDelete = source["confirmBeforeDelete"];
	        this.showTaskDrawer = source["showTaskDrawer"];
	        this.logLevel = source["logLevel"];
	        this.keepLogDays = source["keepLogDays"];
	        this.askBeforeDriverInstall = source["askBeforeDriverInstall"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AvdSummary {
	    name: string;
	    displayName: string;
	    path: string;
	    target: string;
	    apiLevel: string;
	    tagId: string;
	    tagDisplay: string;
	    abi: string;
	    deviceProfileId: string;
	    deviceProfileName: string;
	    oem: string;
	    ramMB: number;
	    cores: number;
	    dataPartition: string;
	    sdCard: string;
	    width: number;
	    height: number;
	    density: number;
	    gpuEnabled: boolean;
	    gpuMode: string;
	    playstore: boolean;
	    state: string;
	    instanceId?: string;
	    serial?: string;
	    port?: number;
	    sizeBytes: number;
	    createdAt?: number;
	    lastUsedAt?: number;
	    tags?: string[];
	    note?: string;
	    broken?: string;
	
	    static createFrom(source: any = {}) {
	        return new AvdSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.path = source["path"];
	        this.target = source["target"];
	        this.apiLevel = source["apiLevel"];
	        this.tagId = source["tagId"];
	        this.tagDisplay = source["tagDisplay"];
	        this.abi = source["abi"];
	        this.deviceProfileId = source["deviceProfileId"];
	        this.deviceProfileName = source["deviceProfileName"];
	        this.oem = source["oem"];
	        this.ramMB = source["ramMB"];
	        this.cores = source["cores"];
	        this.dataPartition = source["dataPartition"];
	        this.sdCard = source["sdCard"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.density = source["density"];
	        this.gpuEnabled = source["gpuEnabled"];
	        this.gpuMode = source["gpuMode"];
	        this.playstore = source["playstore"];
	        this.state = source["state"];
	        this.instanceId = source["instanceId"];
	        this.serial = source["serial"];
	        this.port = source["port"];
	        this.sizeBytes = source["sizeBytes"];
	        this.createdAt = source["createdAt"];
	        this.lastUsedAt = source["lastUsedAt"];
	        this.tags = source["tags"];
	        this.note = source["note"];
	        this.broken = source["broken"];
	    }
	}
	export class AvdDetail {
	    summary: AvdSummary;
	    config: Record<string, string>;
	    rawConfig: string;
	    systemImageDir: string;
	    missingImage: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AvdDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = this.convertValues(source["summary"], AvdSummary);
	        this.config = source["config"];
	        this.rawConfig = source["rawConfig"];
	        this.systemImageDir = source["systemImageDir"];
	        this.missingImage = source["missingImage"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AvdHomeInfo {
	    path: string;
	    source: string;
	    exists: boolean;
	    writable: boolean;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new AvdHomeInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.source = source["source"];
	        this.exists = source["exists"];
	        this.writable = source["writable"];
	        this.count = source["count"];
	    }
	}
	export class LaunchOptions {
	    coldBoot: boolean;
	    wipeData: boolean;
	    noWindow: boolean;
	    noAudio: boolean;
	    noBootAnim: boolean;
	    gpuMode?: string;
	    snapshotName?: string;
	    writableSystem: boolean;
	    netSpeed?: string;
	    netDelay?: string;
	    dnsServers?: string[];
	    httpProxy?: string;
	    timezone?: string;
	    locale?: string;
	    memoryMB?: number;
	    cores?: number;
	    port?: number;
	    scale?: string;
	    extraArgs?: string[];
	
	    static createFrom(source: any = {}) {
	        return new LaunchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.coldBoot = source["coldBoot"];
	        this.wipeData = source["wipeData"];
	        this.noWindow = source["noWindow"];
	        this.noAudio = source["noAudio"];
	        this.noBootAnim = source["noBootAnim"];
	        this.gpuMode = source["gpuMode"];
	        this.snapshotName = source["snapshotName"];
	        this.writableSystem = source["writableSystem"];
	        this.netSpeed = source["netSpeed"];
	        this.netDelay = source["netDelay"];
	        this.dnsServers = source["dnsServers"];
	        this.httpProxy = source["httpProxy"];
	        this.timezone = source["timezone"];
	        this.locale = source["locale"];
	        this.memoryMB = source["memoryMB"];
	        this.cores = source["cores"];
	        this.port = source["port"];
	        this.scale = source["scale"];
	        this.extraArgs = source["extraArgs"];
	    }
	}
	export class AvdPatch {
	    displayName?: string;
	    hw?: Record<string, string>;
	    removeHW?: string[];
	    tags?: string[];
	    note?: string;
	    launch?: LaunchOptions;
	
	    static createFrom(source: any = {}) {
	        return new AvdPatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.displayName = source["displayName"];
	        this.hw = source["hw"];
	        this.removeHW = source["removeHW"];
	        this.tags = source["tags"];
	        this.note = source["note"];
	        this.launch = this.convertValues(source["launch"], LaunchOptions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AvdSpec {
	    name: string;
	    displayName: string;
	    profileId: string;
	    systemImagePath: string;
	    path?: string;
	    sdcardSize?: string;
	    hw: Record<string, string>;
	    createWithAvdManager: boolean;
	    tags?: string[];
	    note?: string;
	    launchDefaults?: LaunchOptions;
	
	    static createFrom(source: any = {}) {
	        return new AvdSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.profileId = source["profileId"];
	        this.systemImagePath = source["systemImagePath"];
	        this.path = source["path"];
	        this.sdcardSize = source["sdcardSize"];
	        this.hw = source["hw"];
	        this.createWithAvdManager = source["createWithAvdManager"];
	        this.tags = source["tags"];
	        this.note = source["note"];
	        this.launchDefaults = this.convertValues(source["launchDefaults"], LaunchOptions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CheckResult {
	    name: string;
	    ok: boolean;
	    detail: string;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ok = source["ok"];
	        this.detail = source["detail"];
	        this.elapsedMs = source["elapsedMs"];
	    }
	}
	export class ConfigDiff {
	    added: Record<string, string>;
	    changed: Record<string, string>;
	    removed: string[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.changed = source["changed"];
	        this.removed = source["removed"];
	        this.warnings = source["warnings"];
	    }
	}
	export class DeviceProfile {
	    id: string;
	    index: number;
	    name: string;
	    oem: string;
	    tag: string;
	    category: string;
	    width?: number;
	    height?: number;
	    density?: number;
	    ramMB?: number;
	
	    static createFrom(source: any = {}) {
	        return new DeviceProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.index = source["index"];
	        this.name = source["name"];
	        this.oem = source["oem"];
	        this.tag = source["tag"];
	        this.category = source["category"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.density = source["density"];
	        this.ramMB = source["ramMB"];
	    }
	}
	export class EnvIssue {
	    id: string;
	    severity: string;
	    title: string;
	    detail: string;
	    fixLabel?: string;
	    fixCommand?: string;
	    fixKind?: string;
	    fixPayload?: string;
	    docsUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.severity = source["severity"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.fixLabel = source["fixLabel"];
	        this.fixCommand = source["fixCommand"];
	        this.fixKind = source["fixKind"];
	        this.fixPayload = source["fixPayload"];
	        this.docsUrl = source["docsUrl"];
	    }
	}
	export class WindowsInfo {
	    available: boolean;
	    hypervisorPresent: boolean;
	    virtualizationFirmwareEnabled: boolean;
	    slat: boolean;
	    vmmMonitor: boolean;
	    longPathsEnabled: boolean;
	    hyperVHostService: boolean;
	    vmComputeService: boolean;
	    cpu?: string;
	    productName?: string;
	    caption?: string;
	    version?: string;
	    build?: string;
	    source?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WindowsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.hypervisorPresent = source["hypervisorPresent"];
	        this.virtualizationFirmwareEnabled = source["virtualizationFirmwareEnabled"];
	        this.slat = source["slat"];
	        this.vmmMonitor = source["vmmMonitor"];
	        this.longPathsEnabled = source["longPathsEnabled"];
	        this.hyperVHostService = source["hyperVHostService"];
	        this.vmComputeService = source["vmComputeService"];
	        this.cpu = source["cpu"];
	        this.productName = source["productName"];
	        this.caption = source["caption"];
	        this.version = source["version"];
	        this.build = source["build"];
	        this.source = source["source"];
	        this.error = source["error"];
	    }
	}
	export class HostInfo {
	    os: string;
	    arch: string;
	    windows?: string;
	    cpuModel?: string;
	    cpuCores?: number;
	    memoryGB?: number;
	
	    static createFrom(source: any = {}) {
	        return new HostInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.windows = source["windows"];
	        this.cpuModel = source["cpuModel"];
	        this.cpuCores = source["cpuCores"];
	        this.memoryGB = source["memoryGB"];
	    }
	}
	export class DiskInfo {
	    path: string;
	    totalGB: number;
	    freeGB: number;
	    sufficient: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiskInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.totalGB = source["totalGB"];
	        this.freeGB = source["freeGB"];
	        this.sufficient = source["sufficient"];
	    }
	}
	export class ToolFix {
	    kind: string;
	    label: string;
	    payload?: string;
	    command?: string;
	    docsUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolFix(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.payload = source["payload"];
	        this.command = source["command"];
	        this.docsUrl = source["docsUrl"];
	    }
	}
	export class ToolStatus {
	    id: string;
	    name: string;
	    state: string;
	    version?: string;
	    path?: string;
	    detail?: string;
	    fix?: ToolFix;
	    meta?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ToolStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.state = source["state"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.detail = source["detail"];
	        this.fix = this.convertValues(source["fix"], ToolFix);
	        this.meta = source["meta"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvReport {
	    sdkRoot: string;
	    sdkRootSource: string;
	    jdkPath: string;
	    avdHome: AvdHomeInfo;
	    components: ToolStatus[];
	    accel: AccelInfo;
	    disks: DiskInfo[];
	    host: HostInfo;
	    ready: boolean;
	    blockers: ToolStatus[];
	    scannedAt: number;
	    windows?: WindowsInfo;
	    issues?: EnvIssue[];
	    runningInstances: number;
	    acceptedLicenses?: string[];
	    scanMs: number;
	
	    static createFrom(source: any = {}) {
	        return new EnvReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sdkRoot = source["sdkRoot"];
	        this.sdkRootSource = source["sdkRootSource"];
	        this.jdkPath = source["jdkPath"];
	        this.avdHome = this.convertValues(source["avdHome"], AvdHomeInfo);
	        this.components = this.convertValues(source["components"], ToolStatus);
	        this.accel = this.convertValues(source["accel"], AccelInfo);
	        this.disks = this.convertValues(source["disks"], DiskInfo);
	        this.host = this.convertValues(source["host"], HostInfo);
	        this.ready = source["ready"];
	        this.blockers = this.convertValues(source["blockers"], ToolStatus);
	        this.scannedAt = source["scannedAt"];
	        this.windows = this.convertValues(source["windows"], WindowsInfo);
	        this.issues = this.convertValues(source["issues"], EnvIssue);
	        this.runningInstances = source["runningInstances"];
	        this.acceptedLicenses = source["acceptedLicenses"];
	        this.scanMs = source["scanMs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagnosticReport {
	    generatedAt: number;
	    appVersion: string;
	    env: EnvReport;
	    settings: AppSettings;
	    checks: CheckResult[];
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.generatedAt = source["generatedAt"];
	        this.appVersion = source["appVersion"];
	        this.env = this.convertValues(source["env"], EnvReport);
	        this.settings = this.convertValues(source["settings"], AppSettings);
	        this.checks = this.convertValues(source["checks"], CheckResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class EmulatorInstance {
	    id: string;
	    avdName: string;
	    serial: string;
	    port: number;
	    adbPort: number;
	    pid: number;
	    state: string;
	    startedAt: number;
	    bootCompletedAt?: number;
	    exitCode?: number;
	    lastError?: string;
	    args: string[];
	    logsPath: string;
	
	    static createFrom(source: any = {}) {
	        return new EmulatorInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.avdName = source["avdName"];
	        this.serial = source["serial"];
	        this.port = source["port"];
	        this.adbPort = source["adbPort"];
	        this.pid = source["pid"];
	        this.state = source["state"];
	        this.startedAt = source["startedAt"];
	        this.bootCompletedAt = source["bootCompletedAt"];
	        this.exitCode = source["exitCode"];
	        this.lastError = source["lastError"];
	        this.args = source["args"];
	        this.logsPath = source["logsPath"];
	    }
	}
	
	
	
	export class HwConfigItem {
	    key: string;
	    label: string;
	    group: string;
	    type: string;
	    default: string;
	    enumValues?: string[];
	    min?: number;
	    max?: number;
	    unit?: string;
	    description: string;
	    advanced: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HwConfigItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.group = source["group"];
	        this.type = source["type"];
	        this.default = source["default"];
	        this.enumValues = source["enumValues"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.unit = source["unit"];
	        this.description = source["description"];
	        this.advanced = source["advanced"];
	    }
	}
	export class License {
	    id: string;
	    text: string;
	    accepted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new License(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.text = source["text"];
	        this.accepted = source["accepted"];
	    }
	}
	export class PlanStep {
	    path: string;
	    action: string;
	    reason: string;
	    sizeBytes: number;
	    sourceURL: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.sizeBytes = source["sizeBytes"];
	        this.sourceURL = source["sourceURL"];
	    }
	}
	export class InstallPlan {
	    steps: PlanStep[];
	    totalBytes: number;
	    licenses: License[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new InstallPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.steps = this.convertValues(source["steps"], PlanStep);
	        this.totalBytes = source["totalBytes"];
	        this.licenses = this.convertValues(source["licenses"], License);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobInfo {
	    id: string;
	    kind: string;
	    title: string;
	    subtitle?: string;
	    group?: string;
	    status: string;
	    phase?: string;
	    percent: number;
	    bytesDone: number;
	    bytesTotal: number;
	    speedBps: number;
	    etaSeconds: number;
	    itemsDone: number;
	    itemsTotal: number;
	    startedAt: number;
	    endedAt?: number;
	    error?: AppError;
	
	    static createFrom(source: any = {}) {
	        return new JobInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.subtitle = source["subtitle"];
	        this.group = source["group"];
	        this.status = source["status"];
	        this.phase = source["phase"];
	        this.percent = source["percent"];
	        this.bytesDone = source["bytesDone"];
	        this.bytesTotal = source["bytesTotal"];
	        this.speedBps = source["speedBps"];
	        this.etaSeconds = source["etaSeconds"];
	        this.itemsDone = source["itemsDone"];
	        this.itemsTotal = source["itemsTotal"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.error = this.convertValues(source["error"], AppError);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class LogLine {
	    at: number;
	    level: string;
	    source?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = source["at"];
	        this.level = source["level"];
	        this.source = source["source"];
	        this.message = source["message"];
	    }
	}
	
	export class NameValidation {
	    name: string;
	    valid: boolean;
	    reason?: string;
	    suggest?: string;
	
	    static createFrom(source: any = {}) {
	        return new NameValidation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.valid = source["valid"];
	        this.reason = source["reason"];
	        this.suggest = source["suggest"];
	    }
	}
	
	export class SdkPackage {
	    path: string;
	    displayName: string;
	    kind: string;
	    revision: string;
	    channel: string;
	    sizeBytes: number;
	    checksumSHA1: string;
	    url: string;
	    installed: boolean;
	    installedRevision?: string;
	    hasUpdate: boolean;
	    licenseId?: string;
	    dependencies?: string[];
	    obsolete: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SdkPackage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.displayName = source["displayName"];
	        this.kind = source["kind"];
	        this.revision = source["revision"];
	        this.channel = source["channel"];
	        this.sizeBytes = source["sizeBytes"];
	        this.checksumSHA1 = source["checksumSHA1"];
	        this.url = source["url"];
	        this.installed = source["installed"];
	        this.installedRevision = source["installedRevision"];
	        this.hasUpdate = source["hasUpdate"];
	        this.licenseId = source["licenseId"];
	        this.dependencies = source["dependencies"];
	        this.obsolete = source["obsolete"];
	    }
	}
	export class SdkRootCandidate {
	    path: string;
	    source: string;
	    score: number;
	    exists: boolean;
	    hasCmdlineTools: boolean;
	    hasPlatformTools: boolean;
	    hasEmulator: boolean;
	    version?: string;
	
	    static createFrom(source: any = {}) {
	        return new SdkRootCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.source = source["source"];
	        this.score = source["score"];
	        this.exists = source["exists"];
	        this.hasCmdlineTools = source["hasCmdlineTools"];
	        this.hasPlatformTools = source["hasPlatformTools"];
	        this.hasEmulator = source["hasEmulator"];
	        this.version = source["version"];
	    }
	}
	export class SdkRootValidation {
	    path: string;
	    ok: boolean;
	    writable: boolean;
	    found: string[];
	    missing: string[];
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new SdkRootValidation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.ok = source["ok"];
	        this.writable = source["writable"];
	        this.found = source["found"];
	        this.missing = source["missing"];
	        this.message = source["message"];
	    }
	}
	export class Snapshot {
	    name: string;
	    sizeBytes: number;
	    createdAt?: number;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sizeBytes = source["sizeBytes"];
	        this.createdAt = source["createdAt"];
	        this.description = source["description"];
	    }
	}
	
	export class SystemImage {
	    path: string;
	    apiLevel: string;
	    tagId: string;
	    tagDisplay: string;
	    abi: string;
	    vendor: string;
	    isPlaystore: boolean;
	    revision: string;
	    sizeBytes: number;
	    installed: boolean;
	    hasUpdate: boolean;
	    requiresEmulator?: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.apiLevel = source["apiLevel"];
	        this.tagId = source["tagId"];
	        this.tagDisplay = source["tagDisplay"];
	        this.abi = source["abi"];
	        this.vendor = source["vendor"];
	        this.isPlaystore = source["isPlaystore"];
	        this.revision = source["revision"];
	        this.sizeBytes = source["sizeBytes"];
	        this.installed = source["installed"];
	        this.hasUpdate = source["hasUpdate"];
	        this.requiresEmulator = source["requiresEmulator"];
	    }
	}
	
	
	export class VerifyResult {
	    path: string;
	    ok: boolean;
	    details: string[];
	
	    static createFrom(source: any = {}) {
	        return new VerifyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.ok = source["ok"];
	        this.details = source["details"];
	    }
	}

}

export namespace service {
	
	export class AddSourceRequest {
	    name: string;
	    baseURL: string;
	    probe: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AddSourceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.probe = source["probe"];
	    }
	}
	export class BootstrapRequest {
	    sourceId: string;
	    withPlatformTools: boolean;
	    withEmulator: boolean;
	    acceptLicenses: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceId = source["sourceId"];
	        this.withPlatformTools = source["withPlatformTools"];
	        this.withEmulator = source["withEmulator"];
	        this.acceptLicenses = source["acceptLicenses"];
	    }
	}
	export class CloneRequest {
	    sourceName: string;
	    newName: string;
	
	    static createFrom(source: any = {}) {
	        return new CloneRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceName = source["sourceName"];
	        this.newName = source["newName"];
	    }
	}
	export class DeleteRequest {
	    name: string;
	    deleteFiles: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeleteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.deleteFiles = source["deleteFiles"];
	    }
	}
	export class DetectRequest {
	    force: boolean;
	    sdkRootOverride?: string;
	
	    static createFrom(source: any = {}) {
	        return new DetectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.force = source["force"];
	        this.sdkRootOverride = source["sdkRootOverride"];
	    }
	}
	export class ExportRequest {
	    name: string;
	    targetZip: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.targetZip = source["targetZip"];
	    }
	}
	export class ImportRequest {
	    zipPath: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.zipPath = source["zipPath"];
	        this.name = source["name"];
	    }
	}
	export class InstallApkRequest {
	    serial: string;
	    apkPath: string;
	    grantAll: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InstallApkRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.apkPath = source["apkPath"];
	        this.grantAll = source["grantAll"];
	    }
	}
	export class InstallRequest {
	    packages: string[];
	    sourceId: string;
	    allowFallbackToOfficial: boolean;
	    autoAcceptLicenses: boolean;
	
	    static createFrom(source: any = {}) {
	        return new InstallRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.packages = source["packages"];
	        this.sourceId = source["sourceId"];
	        this.allowFallbackToOfficial = source["allowFallbackToOfficial"];
	        this.autoAcceptLicenses = source["autoAcceptLicenses"];
	    }
	}
	export class ListRemoteRequest {
	    kinds: string[];
	    channel: string;
	    sourceId: string;
	    refresh: boolean;
	    sysImgTag: string;
	
	    static createFrom(source: any = {}) {
	        return new ListRemoteRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kinds = source["kinds"];
	        this.channel = source["channel"];
	        this.sourceId = source["sourceId"];
	        this.refresh = source["refresh"];
	        this.sysImgTag = source["sysImgTag"];
	    }
	}
	export class ListSystemImagesRequest {
	    tags: string[];
	    sourceId: string;
	    refresh: boolean;
	    onlyInstalled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListSystemImagesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tags = source["tags"];
	        this.sourceId = source["sourceId"];
	        this.refresh = source["refresh"];
	        this.onlyInstalled = source["onlyInstalled"];
	    }
	}
	export class LogcatRequest {
	    serial: string;
	    filter: string;
	
	    static createFrom(source: any = {}) {
	        return new LogcatRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.filter = source["filter"];
	    }
	}
	export class PickRequest {
	    title: string;
	    defaultDirectory?: string;
	    defaultFilename?: string;
	    filterDisplay?: string;
	    filterPattern?: string;
	
	    static createFrom(source: any = {}) {
	        return new PickRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.defaultDirectory = source["defaultDirectory"];
	        this.defaultFilename = source["defaultFilename"];
	        this.filterDisplay = source["filterDisplay"];
	        this.filterPattern = source["filterPattern"];
	    }
	}
	export class PullRequest {
	    serial: string;
	    remote: string;
	    local: string;
	
	    static createFrom(source: any = {}) {
	        return new PullRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.remote = source["remote"];
	        this.local = source["local"];
	    }
	}
	export class PushRequest {
	    serial: string;
	    local: string;
	    remote: string;
	
	    static createFrom(source: any = {}) {
	        return new PushRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.local = source["local"];
	        this.remote = source["remote"];
	    }
	}
	export class ResolvedPaths {
	    sdkRoot: string;
	    sdkRootSource: string;
	    avdHome: string;
	    avdHomeSource: string;
	    jdkPath: string;
	    cacheDir: string;
	    downloadDir: string;
	    logDir: string;
	
	    static createFrom(source: any = {}) {
	        return new ResolvedPaths(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sdkRoot = source["sdkRoot"];
	        this.sdkRootSource = source["sdkRootSource"];
	        this.avdHome = source["avdHome"];
	        this.avdHomeSource = source["avdHomeSource"];
	        this.jdkPath = source["jdkPath"];
	        this.cacheDir = source["cacheDir"];
	        this.downloadDir = source["downloadDir"];
	        this.logDir = source["logDir"];
	    }
	}
	export class StartRequest {
	    avdName: string;
	    options: domain.LaunchOptions;
	
	    static createFrom(source: any = {}) {
	        return new StartRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.avdName = source["avdName"];
	        this.options = this.convertValues(source["options"], domain.LaunchOptions);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SysImgTagAvailability {
	    id: string;
	    display: string;
	    note: string;
	    available: boolean;
	    count: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SysImgTagAvailability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.display = source["display"];
	        this.note = source["note"];
	        this.available = source["available"];
	        this.count = source["count"];
	        this.error = source["error"];
	    }
	}
	export class TerminalRequest {
	    directory: string;
	    command?: string;
	
	    static createFrom(source: any = {}) {
	        return new TerminalRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.directory = source["directory"];
	        this.command = source["command"];
	    }
	}
	export class TestRequest {
	    sourceIds: string[];
	    maxThroughputMB: number;
	    quick: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceIds = source["sourceIds"];
	        this.maxThroughputMB = source["maxThroughputMB"];
	        this.quick = source["quick"];
	    }
	}
	export class UninstallRequest {
	    packages: string[];
	    dryRun: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UninstallRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.packages = source["packages"];
	        this.dryRun = source["dryRun"];
	    }
	}
	export class UpdateSourceRequest {
	    id: string;
	    name: string;
	    baseURL: string;
	    enabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateSourceRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.enabled = source["enabled"];
	    }
	}
	export class WriteConfigRawRequest {
	    name: string;
	    content: string;
	    dryRun: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WriteConfigRawRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.content = source["content"];
	        this.dryRun = source["dryRun"];
	    }
	}

}

