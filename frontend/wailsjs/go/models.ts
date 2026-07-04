export namespace backend {
	
	export class CpolarSnapshot {
	    label: string;
	    localPort: number;
	    localTarget: string;
	    publicUrl: string;
	    publicHost: string;
	    publicPort: number;
	    stoppedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CpolarSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.localPort = source["localPort"];
	        this.localTarget = source["localTarget"];
	        this.publicUrl = source["publicUrl"];
	        this.publicHost = source["publicHost"];
	        this.publicPort = source["publicPort"];
	        this.stoppedAt = source["stoppedAt"];
	    }
	}
	export class CpolarTunnelInfo {
	    id: string;
	    label: string;
	    proto: string;
	    localPort: number;
	    localTarget: string;
	    tunnelName: string;
	    publicUrl: string;
	    publicHost: string;
	    publicPort: number;
	    inspectAddr?: string;
	    targetChecked: boolean;
	    targetError?: string;
	    running: boolean;
	    startedAt: string;
	    stoppedAt?: string;
	    accountEmail: string;
	    recentLog?: string[];
	    lastError?: string;
	    processExited: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CpolarTunnelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.proto = source["proto"];
	        this.localPort = source["localPort"];
	        this.localTarget = source["localTarget"];
	        this.tunnelName = source["tunnelName"];
	        this.publicUrl = source["publicUrl"];
	        this.publicHost = source["publicHost"];
	        this.publicPort = source["publicPort"];
	        this.inspectAddr = source["inspectAddr"];
	        this.targetChecked = source["targetChecked"];
	        this.targetError = source["targetError"];
	        this.running = source["running"];
	        this.startedAt = source["startedAt"];
	        this.stoppedAt = source["stoppedAt"];
	        this.accountEmail = source["accountEmail"];
	        this.recentLog = source["recentLog"];
	        this.lastError = source["lastError"];
	        this.processExited = source["processExited"];
	    }
	}
	export class CpolarStatus {
	    ok: boolean;
	    configPath: string;
	    cpolarPath: string;
	    cpolarFound: boolean;
	    baseUrl: string;
	    accountCount: number;
	    tokenCount: number;
	    nextIndex: number;
	    tunnels: CpolarTunnelInfo[];
	    lastTunnels?: CpolarSnapshot[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new CpolarStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.configPath = source["configPath"];
	        this.cpolarPath = source["cpolarPath"];
	        this.cpolarFound = source["cpolarFound"];
	        this.baseUrl = source["baseUrl"];
	        this.accountCount = source["accountCount"];
	        this.tokenCount = source["tokenCount"];
	        this.nextIndex = source["nextIndex"];
	        this.tunnels = this.convertValues(source["tunnels"], CpolarTunnelInfo);
	        this.lastTunnels = this.convertValues(source["lastTunnels"], CpolarSnapshot);
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
	
	export class PEExport {
	    name: string;
	    ordinal: number;
	    rva: number;
	
	    static createFrom(source: any = {}) {
	        return new PEExport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ordinal = source["ordinal"];
	        this.rva = source["rva"];
	    }
	}
	export class GW2DLLInfo {
	    found: boolean;
	    path?: string;
	    capability: string;
	    directCallable: boolean;
	    requiresGodotHost: boolean;
	    entrySymbol?: string;
	    exports?: PEExport[];
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new GW2DLLInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.found = source["found"];
	        this.path = source["path"];
	        this.capability = source["capability"];
	        this.directCallable = source["directCallable"];
	        this.requiresGodotHost = source["requiresGodotHost"];
	        this.entrySymbol = source["entrySymbol"];
	        this.exports = this.convertValues(source["exports"], PEExport);
	        this.reason = source["reason"];
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
	export class Graphwar2PublisherStatus {
	    running: boolean;
	    broker: string;
	    roomName: string;
	    address: string;
	    lastError: string;
	    lastSent: string;
	    lastReceived: string;
	
	    static createFrom(source: any = {}) {
	        return new Graphwar2PublisherStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.broker = source["broker"];
	        this.roomName = source["roomName"];
	        this.address = source["address"];
	        this.lastError = source["lastError"];
	        this.lastSent = source["lastSent"];
	        this.lastReceived = source["lastReceived"];
	    }
	}
	export class Graphwar2RoomProbeResult {
	    ok: boolean;
	    host: string;
	    port: number;
	    events: string[];
	    error?: string;
	    address: string;
	
	    static createFrom(source: any = {}) {
	        return new Graphwar2RoomProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.events = source["events"];
	        this.error = source["error"];
	        this.address = source["address"];
	    }
	}

}

