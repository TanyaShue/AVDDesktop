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
	    mirrorSourceId: string;
	    jdkMirrorSourceId: string;
	
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
	        this.mirrorSourceId = source["mirrorSourceId"];
	        this.jdkMirrorSourceId = source["jdkMirrorSourceId"];
	    }
	}
	export class AvdHardware {
	    ramMb?: number;
	    cpuCores?: number;
	    lcdWidth?: number;
	    lcdHeight?: number;
	
	    static createFrom(source: any = {}) {
	        return new AvdHardware(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ramMb = source["ramMb"];
	        this.cpuCores = source["cpuCores"];
	        this.lcdWidth = source["lcdWidth"];
	        this.lcdHeight = source["lcdHeight"];
	    }
	}
	export class AvdSpec {
	    name: string;
	    systemImagePath: string;
	    profileId?: string;
	    hardware?: AvdHardware;
	
	    static createFrom(source: any = {}) {
	        return new AvdSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.systemImagePath = source["systemImagePath"];
	        this.profileId = source["profileId"];
	        this.hardware = this.convertValues(source["hardware"], AvdHardware);
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
	    path: string;
	    api: string;
	    tag: string;
	    abi: string;
	    deviceProfileId: string;
	    state: string;
	    instanceId?: string;
	    serial?: string;
	    port?: number;
	    broken?: string;
	    ramMb?: number;
	    cpuCores?: number;
	    lcdWidth?: number;
	    lcdHeight?: number;
	
	    static createFrom(source: any = {}) {
	        return new AvdSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.api = source["api"];
	        this.tag = source["tag"];
	        this.abi = source["abi"];
	        this.deviceProfileId = source["deviceProfileId"];
	        this.state = source["state"];
	        this.instanceId = source["instanceId"];
	        this.serial = source["serial"];
	        this.port = source["port"];
	        this.broken = source["broken"];
	        this.ramMb = source["ramMb"];
	        this.cpuCores = source["cpuCores"];
	        this.lcdWidth = source["lcdWidth"];
	        this.lcdHeight = source["lcdHeight"];
	    }
	}
	export class DeviceProfile {
	    id: string;
	    index: number;
	    name: string;
	    oem: string;
	    tag: string;
	
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
	    pid: number;
	    state: string;
	    startedAt: number;
	    endedAt?: number;
	    exitCode?: number;
	    lastError?: string;
	    args: string[];
	
	    static createFrom(source: any = {}) {
	        return new EmulatorInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.avdName = source["avdName"];
	        this.serial = source["serial"];
	        this.port = source["port"];
	        this.pid = source["pid"];
	        this.state = source["state"];
	        this.startedAt = source["startedAt"];
	        this.endedAt = source["endedAt"];
	        this.exitCode = source["exitCode"];
	        this.lastError = source["lastError"];
	        this.args = source["args"];
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
	    jdkRoot: string;
	    sdkRoot: string;
	    avdHome: string;
	    javaPath?: string;
	    mirrorSourceId?: string;
	    mirrorSourceName?: string;
	    jdkMirrorSourceId?: string;
	    jdkMirrorSourceName?: string;
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
	        this.jdkRoot = source["jdkRoot"];
	        this.sdkRoot = source["sdkRoot"];
	        this.avdHome = source["avdHome"];
	        this.javaPath = source["javaPath"];
	        this.mirrorSourceId = source["mirrorSourceId"];
	        this.mirrorSourceName = source["mirrorSourceName"];
	        this.jdkMirrorSourceId = source["jdkMirrorSourceId"];
	        this.jdkMirrorSourceName = source["jdkMirrorSourceName"];
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
	
	export class JobInfo {
	    id: string;
	    kind: string;
	    title: string;
	    subtitle?: string;
	    status: string;
	    phase?: string;
	    percent: number;
	    bytesDone: number;
	    bytesTotal: number;
	    speedBps: number;
	    etaSeconds: number;
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
	        this.status = source["status"];
	        this.phase = source["phase"];
	        this.percent = source["percent"];
	        this.bytesDone = source["bytesDone"];
	        this.bytesTotal = source["bytesTotal"];
	        this.speedBps = source["speedBps"];
	        this.etaSeconds = source["etaSeconds"];
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
	    seq?: number;
	    at: number;
	    level: string;
	    source?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.at = source["at"];
	        this.level = source["level"];
	        this.source = source["source"];
	        this.message = source["message"];
	    }
	}
	export class MirrorResource {
	    id: string;
	    name: string;
	    path?: string;
	    url?: string;
	    required: boolean;
	    available: boolean;
	    statusCode?: number;
	    sizeBytes?: number;
	    latencyMs?: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorResource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.url = source["url"];
	        this.required = source["required"];
	        this.available = source["available"];
	        this.statusCode = source["statusCode"];
	        this.sizeBytes = source["sizeBytes"];
	        this.latencyMs = source["latencyMs"];
	        this.error = source["error"];
	    }
	}
	export class MirrorCheck {
	    sourceId: string;
	    sourceName: string;
	    baseURL: string;
	    region?: string;
	    reachable: boolean;
	    compatible: boolean;
	    recommended: boolean;
	    latencyMs: number;
	    throughputBps: number;
	    resources: MirrorResource[];
	    checkedAt: number;
	    elapsedMs: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MirrorCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceId = source["sourceId"];
	        this.sourceName = source["sourceName"];
	        this.baseURL = source["baseURL"];
	        this.region = source["region"];
	        this.reachable = source["reachable"];
	        this.compatible = source["compatible"];
	        this.recommended = source["recommended"];
	        this.latencyMs = source["latencyMs"];
	        this.throughputBps = source["throughputBps"];
	        this.resources = this.convertValues(source["resources"], MirrorResource);
	        this.checkedAt = source["checkedAt"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
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
	
	export class MirrorSource {
	    id: string;
	    name: string;
	    baseURL: string;
	    region?: string;
	    note?: string;
	    official?: boolean;
	    active?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MirrorSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseURL = source["baseURL"];
	        this.region = source["region"];
	        this.note = source["note"];
	        this.official = source["official"];
	        this.active = source["active"];
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
	export class SystemImage {
	    path: string;
	    api: string;
	    tag: string;
	    abi: string;
	    version?: string;
	    description?: string;
	    androidVersion: string;
	    rootSupported: boolean;
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
	        this.androidVersion = source["androidVersion"];
	        this.rootSupported = source["rootSupported"];
	        this.installed = source["installed"];
	    }
	}
	

}

export namespace service {
	
	export class ResolvedPaths {
	    appRoot: string;
	    jdkRoot: string;
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
	        this.jdkRoot = source["jdkRoot"];
	        this.sdkRoot = source["sdkRoot"];
	        this.avdHome = source["avdHome"];
	        this.javaPath = source["javaPath"];
	        this.logDir = source["logDir"];
	        this.cacheDir = source["cacheDir"];
	    }
	}
	export class StartRequest {
	    avdName: string;
	    coldBoot: boolean;
	    noWindow: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StartRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.avdName = source["avdName"];
	        this.coldBoot = source["coldBoot"];
	        this.noWindow = source["noWindow"];
	    }
	}

}

