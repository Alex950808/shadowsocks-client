export namespace config {
	
	export class ServerConfig {
	    name: string;
	    server: string;
	    port: number;
	    password: string;
	    method: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.server = source["server"];
	        this.port = source["port"];
	        this.password = source["password"];
	        this.method = source["method"];
	    }
	}

}

export namespace main {
	
	export class ServerInput {
	    idx: number;
	    name: string;
	    server: string;
	    port: number;
	    password: string;
	    method: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.idx = source["idx"];
	        this.name = source["name"];
	        this.server = source["server"];
	        this.port = source["port"];
	        this.password = source["password"];
	        this.method = source["method"];
	    }
	}
	export class StatusResponse {
	    running: boolean;
	    active_idx: number;
	    http_port: number;
	    socks_port: number;
	    servers: config.ServerConfig[];
	
	    static createFrom(source: any = {}) {
	        return new StatusResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.active_idx = source["active_idx"];
	        this.http_port = source["http_port"];
	        this.socks_port = source["socks_port"];
	        this.servers = this.convertValues(source["servers"], config.ServerConfig);
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
	export class ToggleResponse {
	    action: string;
	
	    static createFrom(source: any = {}) {
	        return new ToggleResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	    }
	}

}

