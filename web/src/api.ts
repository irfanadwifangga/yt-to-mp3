import { bootstrapToken } from "./token";

const token = bootstrapToken();

/** Token dibutuhkan modul lain yang memanggil fetch sendiri, seperti aliran SSE. */
export const sessionToken = token;

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
    /**
     * Konteks terstruktur dari server, misalnya kunci setelan yang ditolak.
     * Bukan kalimat siap tampil: teksnya tetap dirangkai klien dari `code`.
     */
    readonly details: Record<string, string> = {},
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
    let details: Record<string, string> = {};
    try {
      const body = await res.json();
      code = body?.error?.code ?? code;
      details = body?.error?.details ?? {};
    } catch {
      // Respons bukan JSON; pertahankan kode default.
    }
    throw new ApiError(code, res.status, details);
  }

  // 204 No Content tidak punya body untuk diurai.
  if (res.status === 204) return undefined as T;

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
  presets: () => request<{ presets: Preset[] }>("/presets"),
  settings: () => request<{ settings: Setting[] }>("/settings"),
  updateSettings: (values: Record<string, string>) =>
    request<{ settings: Setting[] }>("/settings", {
      method: "PUT",
      body: JSON.stringify(values),
    }),
  jobs: (status?: JobStatus) =>
    request<{ jobs: Job[]; next_cursor?: string }>(
      status ? `/jobs?status=${status}` : "/jobs",
    ),
  createJob: (url: string, presetId: string) =>
    request<Job>("/jobs", {
      method: "POST",
      body: JSON.stringify({ url, preset_id: presetId }),
    }),
  cancelJob: (id: string) =>
    request<{ status: string }>(`/jobs/${id}/cancel`, { method: "POST" }),
  retryJob: (id: string) => request<Job>(`/jobs/${id}/retry`, { method: "POST" }),
  deleteJob: (id: string) =>
    request<void>(`/jobs/${id}?delete_file=false`, { method: "DELETE" }),
  revealFile: (id: string) =>
    request<void>(`/files/${id}/reveal`, { method: "POST" }),
  /**
   * Membuka dialog pemilih folder native di komputer pengguna. Browser tidak
   * pernah memberi tahu halaman path absolut sebuah folder, jadi dialognya
   * dibuka oleh server lokal. Respons tertahan sampai dialog ditutup.
   */
  pickFolder: (title: string, start: string) =>
    request<{ path?: string; cancelled: boolean }>("/dialogs/folder", {
      method: "POST",
      body: JSON.stringify({ title, start }),
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

export type JobStatus =
  | "queued"
  | "resolving"
  | "downloading"
  | "converting"
  | "verifying"
  | "completed"
  | "failed"
  | "cancelling"
  | "cancelled";

export interface Job {
  id: string;
  source_url: string;
  source_key: string;
  title: string;
  status: JobStatus;
  preset_id: string;
  filename_mode: string;
  progress: number | null;
  phase?: string;
  attempt_count: number;
  error_code?: string;
  file_id?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

export interface Preset {
  id: string;
  label: string;
  format: string;
  mode: string;
  bitrate_kbps?: number;
  vbr_quality?: number;
  sample_rate?: number;
  channels: number;
}

/** Status terminal tidak akan berubah lagi. */
export const TERMINAL: readonly JobStatus[] = ["completed", "failed", "cancelled"];

export function isTerminal(status: JobStatus): boolean {
  return TERMINAL.includes(status);
}

/** URL unduhan dipakai lewat fetch, bukan sebagai href langsung: token
 *  dikirim di header, bukan di query string. */
export async function downloadFile(fileId: string, filename: string): Promise<void> {
  const headers = new Headers();
  if (token) headers.set("X-Session-Token", token);

  const res = await fetch(`/api/files/${encodeURIComponent(fileId)}`, { headers });
  if (!res.ok) {
    let code = "INTERNAL";
    try {
      code = (await res.json())?.error?.code ?? code;
    } catch {
      // respons bukan JSON
    }
    throw new ApiError(code, res.status);
  }

  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export interface Setting {
  key: string;
  value: string;
  kind: "string" | "int" | "bool" | "enum" | "path";
  options?: string[];
  requires_restart: boolean;
}
