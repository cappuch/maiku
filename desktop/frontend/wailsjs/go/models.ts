export namespace core {
	
	export class CustomProvider {
	    id: string;
	    name: string;
	    baseUrl: string;
	    api?: string;
	    models?: string[];

	    static createFrom(source: any = {}) {
	        return new CustomProvider(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.api = source["api"];
	        this.models = source["models"];
	    }
	}

	export class SessionSummary {
	    id: string;
	    path: string;
	    timestamp: string;
	    cwd: string;
	    preview: string;
	    modTime: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.timestamp = source["timestamp"];
	        this.cwd = source["cwd"];
	        this.preview = source["preview"];
	        this.modTime = source["modTime"];
	        this.name = source["name"];
	    }
	}

}

export namespace main {
	
	export class APIKeyStatus {
	    provider: string;
	    name: string;
	    hasKey: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new APIKeyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.name = source["name"];
	        this.hasKey = source["hasKey"];
	        this.source = source["source"];
	    }
	}
	export class ImageAttachment {
	    mimeType: string;
	    data: string;
	    name?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageAttachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mimeType = source["mimeType"];
	        this.data = source["data"];
	        this.name = source["name"];
	    }
	}
	export class UIMessage {
	    id: string;
	    role: string;
	    text?: string;
	    thinking?: string;
	    toolName?: string;
	    toolCallId?: string;
	    args?: number[];
	    details?: any;
	    isError?: boolean;
	    streaming?: boolean;
	    images?: ImageAttachment[];
	
	    static createFrom(source: any = {}) {
	        return new UIMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.text = source["text"];
	        this.thinking = source["thinking"];
	        this.toolName = source["toolName"];
	        this.toolCallId = source["toolCallId"];
	        this.args = source["args"];
	        this.details = source["details"];
	        this.isError = source["isError"];
	        this.streaming = source["streaming"];
	        this.images = this.convertValues(source["images"], ImageAttachment);
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
	export class UsageTotals {
	    input: number;
	    output: number;
	    cacheRead: number;
	    cacheWrite: number;
	    totalTokens: number;
	    cost: number;
	    totalCost: number;
	    cacheRate: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageTotals(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input = source["input"];
	        this.output = source["output"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheWrite = source["cacheWrite"];
	        this.totalTokens = source["totalTokens"];
	        this.cost = source["cost"];
	        this.totalCost = source["totalCost"];
	        this.cacheRate = source["cacheRate"];
	    }
	}
	export class AppState {
	    cwd: string;
	    folderName: string;
	    userName: string;
	    provider: string;
	    modelId: string;
	    modelName: string;
	    thinking: string;
	    streaming: boolean;
	    sessionId: string;
	    sessionPath: string;
	    usage: UsageTotals;
	    hasApiKey: boolean;
	    messages: UIMessage[];
	    recentDirs: string[];
	    streamingSessionIds: string[];
	    streamText: string;
	    streamThinking: string;
	    mcp: mcp.Status;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cwd = source["cwd"];
	        this.folderName = source["folderName"];
	        this.userName = source["userName"];
	        this.provider = source["provider"];
	        this.modelId = source["modelId"];
	        this.modelName = source["modelName"];
	        this.thinking = source["thinking"];
	        this.streaming = source["streaming"];
	        this.sessionId = source["sessionId"];
	        this.sessionPath = source["sessionPath"];
	        this.usage = this.convertValues(source["usage"], UsageTotals);
	        this.hasApiKey = source["hasApiKey"];
	        this.messages = this.convertValues(source["messages"], UIMessage);
	        this.recentDirs = source["recentDirs"];
	        this.streamingSessionIds = source["streamingSessionIds"];
	        this.streamText = source["streamText"];
	        this.streamThinking = source["streamThinking"];
	        this.mcp = this.convertValues(source["mcp"], mcp.Status);
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
	export class CodexLoginInfo {
	    userCode: string;
	    verificationUri: string;
	
	    static createFrom(source: any = {}) {
	        return new CodexLoginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.userCode = source["userCode"];
	        this.verificationUri = source["verificationUri"];
	    }
	}
	
	export class MCPServerInput {
	    name: string;
	    kind?: string;
	    command?: string;
	    args?: string[];
	    env?: Record<string, string>;
	    url?: string;
	    headers?: Record<string, string>;
	    disabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MCPServerInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	    }
	}
	export class ModelInfo {
	    provider: string;
	    id: string;
	    name: string;
	    reasoning: boolean;
	    vision: boolean;
	    hasKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.reasoning = source["reasoning"];
	        this.vision = source["vision"];
	        this.hasKey = source["hasKey"];
	    }
	}
	export class PathSuggestion {
	    value: string;
	    label: string;
	    isDirectory: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PathSuggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	        this.isDirectory = source["isDirectory"];
	    }
	}
	export class PickedFile {
	    path: string;
	    relPath: string;
	    name: string;
	    isImage: boolean;
	    mimeType?: string;
	    data?: string;
	
	    static createFrom(source: any = {}) {
	        return new PickedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.relPath = source["relPath"];
	        this.name = source["name"];
	        this.isImage = source["isImage"];
	        this.mimeType = source["mimeType"];
	        this.data = source["data"];
	    }
	}
	

}

export namespace mcp {
	
	export class ServerStatus {
	    name: string;
	    kind: string;
	    command?: string;
	    args?: string[];
	    env?: Record<string, string>;
	    url?: string;
	    headers?: Record<string, string>;
	    disabled: boolean;
	    connected: boolean;
	    error?: string;
	    toolCount: number;
	    tools?: string[];
	    scope: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.env = source["env"];
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	        this.connected = source["connected"];
	        this.error = source["error"];
	        this.toolCount = source["toolCount"];
	        this.tools = source["tools"];
	        this.scope = source["scope"];
	    }
	}
	export class Status {
	    configured: number;
	    connected: number;
	    failed: number;
	    servers: ServerStatus[];
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.connected = source["connected"];
	        this.failed = source["failed"];
	        this.servers = this.convertValues(source["servers"], ServerStatus);
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

