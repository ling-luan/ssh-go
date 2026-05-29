export namespace config {
	
	export class Tunnel {
	    id: string;
	    name: string;
	    profileId: string;
	    localHost: string;
	    localPort: number;
	    targetHost: string;
	    targetPort: number;
	    autoStart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Tunnel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.profileId = source["profileId"];
	        this.localHost = source["localHost"];
	        this.localPort = source["localPort"];
	        this.targetHost = source["targetHost"];
	        this.targetPort = source["targetPort"];
	        this.autoStart = source["autoStart"];
	    }
	}
	export class AuthConfig {
	    type: string;
	    password?: string;
	    keyPath?: string;
	    passphrase?: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.password = source["password"];
	        this.keyPath = source["keyPath"];
	        this.passphrase = source["passphrase"];
	    }
	}
	export class Profile {
	    id: string;
	    name: string;
	    host: string;
	    port: number;
	    username: string;
	    auth: AuthConfig;
	    hostKeyPolicy: string;
	    knownHostsPath?: string;
	    connectTimeoutSeconds?: number;
	    keepAliveSeconds?: number;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.auth = this.convertValues(source["auth"], AuthConfig);
	        this.hostKeyPolicy = source["hostKeyPolicy"];
	        this.knownHostsPath = source["knownHostsPath"];
	        this.connectTimeoutSeconds = source["connectTimeoutSeconds"];
	        this.keepAliveSeconds = source["keepAliveSeconds"];
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
	export class AppConfig {
	    profiles: Profile[];
	    tunnels: Tunnel[];
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profiles = this.convertValues(source["profiles"], Profile);
	        this.tunnels = this.convertValues(source["tunnels"], Tunnel);
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
	
	

}

export namespace main {
	
	export class EventResponse {
	    time: string;
	    level: string;
	    tunnelId?: string;
	    tunnelName?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new EventResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.level = source["level"];
	        this.tunnelId = source["tunnelId"];
	        this.tunnelName = source["tunnelName"];
	        this.message = source["message"];
	    }
	}
	export class SaveProfileRequest {
	    originalId: string;
	    profile: config.Profile;
	
	    static createFrom(source: any = {}) {
	        return new SaveProfileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.originalId = source["originalId"];
	        this.profile = this.convertValues(source["profile"], config.Profile);
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
	export class SaveTunnelRequest {
	    originalId: string;
	    tunnel: config.Tunnel;
	
	    static createFrom(source: any = {}) {
	        return new SaveTunnelRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.originalId = source["originalId"];
	        this.tunnel = this.convertValues(source["tunnel"], config.Tunnel);
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
	export class TunnelStatusResponse {
	    id: string;
	    name: string;
	    profileId: string;
	    profileName: string;
	    state: string;
	    running: boolean;
	    autoStart: boolean;
	    localAddress: string;
	    targetAddress: string;
	    activeConnections: number;
	    bytesIn: number;
	    bytesOut: number;
	    startedAt?: string;
	    lastError?: string;
	
	    static createFrom(source: any = {}) {
	        return new TunnelStatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.profileId = source["profileId"];
	        this.profileName = source["profileName"];
	        this.state = source["state"];
	        this.running = source["running"];
	        this.autoStart = source["autoStart"];
	        this.localAddress = source["localAddress"];
	        this.targetAddress = source["targetAddress"];
	        this.activeConnections = source["activeConnections"];
	        this.bytesIn = source["bytesIn"];
	        this.bytesOut = source["bytesOut"];
	        this.startedAt = source["startedAt"];
	        this.lastError = source["lastError"];
	    }
	}
	export class SnapshotResponse {
	    configPath: string;
	    config: config.AppConfig;
	    tunnels: TunnelStatusResponse[];
	    profiles: tunnel.ProfileStatus[];
	    events: EventResponse[];
	
	    static createFrom(source: any = {}) {
	        return new SnapshotResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configPath = source["configPath"];
	        this.config = this.convertValues(source["config"], config.AppConfig);
	        this.tunnels = this.convertValues(source["tunnels"], TunnelStatusResponse);
	        this.profiles = this.convertValues(source["profiles"], tunnel.ProfileStatus);
	        this.events = this.convertValues(source["events"], EventResponse);
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

}

export namespace tunnel {
	
	export class ProfileStatus {
	    id: string;
	    name: string;
	    address: string;
	    connected: boolean;
	    activeTunnels: number;
	
	    static createFrom(source: any = {}) {
	        return new ProfileStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.address = source["address"];
	        this.connected = source["connected"];
	        this.activeTunnels = source["activeTunnels"];
	    }
	}

}

