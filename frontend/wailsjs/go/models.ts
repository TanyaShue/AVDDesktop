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
	export class AppSettings {
	    version: number;
	    theme: string;
	    confirmBeforeDelete: boolean;
	    showTaskDrawer: boolean;
	    logLevel: string;
	    keepLogDays: number;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.theme = source["theme"];
	        this.confirmBeforeDelete = source["confirmBeforeDelete"];
	        this.showTaskDrawer = source["showTaskDrawer"];
	        this.logLevel = source["logLevel"];
	        this.keepLogDays = source["keepLogDays"];
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
	export class EnvIssue {
	    id: string;
	    severity: string;
	    title: string;
	    detail: string;
	    fixLabel?: string;
	    fixCommand?: string;
	    fixKind?: string;
	    fixPayload?: string;
	
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
	    }
	}
	export class HostInfo {
	    os: string;
	    arch: string;
	    cpuCores?: number;
	    memoryGB?: number;
	
	    static createFrom(source: any = {}) {
	        return new HostInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.cpuCores = source["cpuCores"];
	        this.memoryGB = source["memoryGB"];
	    }
	}
	export class ToolFix {
	    kind: string;
	    label: string;
	    payload?: string;
	    command?: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolFix(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.payload = source["payload"];
	        this.command = source["command"];
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
	    appRoot: string;
	    sdkRoot: string;
	    avdHome: string;
	    javaPath?: string;
	    ready: boolean;
	    needInit: boolean;
	    components: ToolStatus[];
	    accel?: AccelInfo;
	    disk: DiskInfo;
	    host: HostInfo;
	    images: number;
	    avds: number;
	    issues: EnvIssue[];
	    checkedAt: number;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new EnvReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appRoot = source["appRoot"];
	        this.sdkRoot = source["sdkRoot"];
	        this.avdHome = source["avdHome"];
	        this.javaPath = source["javaPath"];
	        this.ready = source["ready"];
	        this.needInit = source["needInit"];
	        this.components = this.convertValues(source["components"], ToolStatus);
	        this.accel = this.convertValues(source["accel"], AccelInfo);
	        this.disk = this.convertValues(source["disk"], DiskInfo);
	        this.host = this.convertValues(source["host"], HostInfo);
	        this.images = source["images"];
	        this.avds = source["avds"];
	        this.issues = this.convertValues(source["issues"], EnvIssue);
	        this.checkedAt = source["checkedAt"];
	        this.elapsedMs = source["elapsedMs"];
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
	    api: string;
	    tag: string;
	    abi: string;
	    version?: string;
	    description?: string;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SystemImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.api = source["api"];
	        this.tag = source["tag"];
	        this.abi = source["abi"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.installed = source["installed"];
	    }
	}
	

}

export namespace service {
	
	export class AppInfo {
	    name: string;
	    version: string;
	    appRoot: string;
	    sdkRoot: string;
	    avdHome: string;
	    logLevel: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.appRoot = source["appRoot"];
	        this.sdkRoot = source["sdkRoot"];
	        this.avdHome = source["avdHome"];
	        this.logLevel = source["logLevel"];
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
	export class ResolvedPaths {
	    appRoot: string;
	    sdkRoot: string;
	    avdHome: string;
	    javaPath?: string;
	    logDir: string;
	    cacheDir: string;
	
	    static createFrom(source: any = {}) {
	        return new ResolvedPaths(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appRoot = source["appRoot"];
	        this.sdkRoot = source["sdkRoot"];
	        this.avdHome = source["avdHome"];
	        this.javaPath = source["javaPath"];
	        this.logDir = source["logDir"];
	        this.cacheDir = source["cacheDir"];
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

