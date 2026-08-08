export type UsageTotals = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  totalTokens: number;
  cost: number;
  cacheRate: number;
};

export type ImageAttachment = {
  mimeType: string;
  data: string;
  name?: string;
};

export type PathSuggestion = {
  value: string;
  label: string;
  isDirectory: boolean;
};

export type PickedFile = {
  path: string;
  relPath: string;
  name: string;
  isImage: boolean;
  mimeType?: string;
  data?: string;
};

export type UIMessage = {
  id?: string;
  role: string;
  text?: string;
  toolName?: string;
  toolCallId?: string;
  args?: unknown;
  details?: unknown;
  isError?: boolean;
  streaming?: boolean;
  images?: ImageAttachment[];
};

export type AppState = {
  cwd: string;
  folderName: string;
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
};

export type ModelInfo = {
  provider: string;
  id: string;
  name: string;
  reasoning: boolean;
  vision: boolean;
  hasKey: boolean;
};

export type SessionSummary = {
  id: string;
  path: string;
  timestamp: string;
  cwd: string;
  preview: string;
  modTime: string;
  name?: string;
};

export type APIKeyStatus = {
  provider: string;
  name?: string;
  hasKey: boolean;
  source: string;
};

export const emptyUsage = (): UsageTotals => ({
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  totalTokens: 0,
  cost: 0,
  cacheRate: 0,
});
