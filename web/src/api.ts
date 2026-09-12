import { bootstrapToken } from "./token";

const token = bootstrapToken();

/** Kode error adalah set tertutup; lihat docs planning. */
export type ErrorCode =
  | "INVALID_URL"
  | "UNSUPPORTED_URL"
  | "LIVE_NOT_SUPPORTED"
  | "TOOL_MISSING"
  | "UNAUTHORIZED"
  | "FORBIDDEN_HOST"
  | "FORBIDDEN_ORIGIN"
  | "INTERNAL";

export class ApiError extends Error {
  constructor(
    readonly code: ErrorCode | string,
    readonly status: number,
  ) {
    super(`${code} (${status})`);
    this.name = "ApiError";
  }
}

export interface ToolStatus {
  available: boolean;
  version: string;
  path: string;
}

export interface Health {
  app: string;
  version: string;
  commit: string;
  status: string;
  uptime_seconds: number;
  spa_built: boolean;
  output_dir: string;
  tools: Record<string, ToolStatus>;
  queue: { active: number; queued: number; capacity: number };
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (token) headers.set("X-Session-Token", token);
  if (init.body) headers.set("Content-Type", "application/json");

  const res = await fetch(`/api${path}`, { ...init, headers });

  if (!res.ok) {
    let code = "INTERNAL";
    try {
      const body = await res.json();
      code = body?.error?.code ?? code;
    } catch {
      // Respons bukan JSON; pertahankan kode default.
    }
    throw new ApiError(code, res.status);
  }

  return (await res.json()) as T;
}

export const api = {
  health: () => request<Health>("/health"),
  shutdown: () => request<{ status: string }>("/shutdown", { method: "POST" }),
  tools: () => request<ToolsPayload>("/tools"),
  installTool: (name: string) =>
    request<ToolsPayload>("/tools/install", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  metadata: (url: string) =>
    request<Metadata>("/metadata", {
      method: "POST",
      body: JSON.stringify({ url }),
    }),
};

export interface Metadata {
  source_key: string;
  source_url: string;
  title: string;
  uploader: string;
  duration_ms: number;
  thumbnail_url: string;
  source_codec: string;
  sample_rate: number;
}

export interface ToolsPayload {
  tools: Record<string, ToolStatus>;
}
