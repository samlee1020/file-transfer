import type {
  Admin,
  ApiEnvelope,
  BatchResult,
  FileEvent,
  FileItem,
  LoginResult,
  PageResult,
  PreviewKind,
  TextPreview,
  UploadResult,
} from "./types";

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

async function parseResponse<T>(res: Response): Promise<T> {
  if (res.status === 204) return undefined as T;
  const contentType = res.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) {
    if (!res.ok) throw new ApiError(res.status, "HTTP_ERROR", res.statusText || "请求失败");
    return undefined as T;
  }
  const payload = (await res.json()) as ApiEnvelope<T> | { error?: { code: string; message: string } };
  if (!res.ok || "error" in payload) {
    const err = "error" in payload ? payload.error : undefined;
    throw new ApiError(res.status, err?.code ?? "HTTP_ERROR", err?.message ?? "请求失败");
  }
  return (payload as ApiEnvelope<T>).data;
}

function authHeaders(token: string): HeadersInit {
  return { Authorization: `Bearer ${token}` };
}

function withQuery(path: string, params: Record<string, string | number | boolean | undefined>): string {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") query.set(key, String(value));
  });
  const qs = query.toString();
  return qs ? `${path}?${qs}` : path;
}

export async function healthCheck(): Promise<boolean> {
  try {
    const res = await fetch("/healthz");
    return res.ok;
  } catch {
    return false;
  }
}

export async function uploadFile(file: File, token?: string): Promise<UploadResult> {
  const body = new FormData();
  body.append("file", file);
  const res = await fetch("/api/v1/files", {
    method: "POST",
    headers: token ? authHeaders(token) : undefined,
    body,
  });
  return parseResponse<UploadResult>(res);
}

export async function login(password: string): Promise<LoginResult> {
  const res = await fetch("/api/v1/admin/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username: "admin", password }),
  });
  return parseResponse<LoginResult>(res);
}

export async function getMe(token: string): Promise<Admin> {
  const res = await fetch("/api/v1/admin/me", { headers: authHeaders(token) });
  return parseResponse<Admin>(res);
}

export async function logout(token: string): Promise<void> {
  const res = await fetch("/api/v1/admin/logout", {
    method: "POST",
    headers: authHeaders(token),
  });
  await parseResponse<void>(res);
}

export async function changePassword(token: string, oldPassword: string, newPassword: string): Promise<void> {
  const res = await fetch("/api/v1/admin/password", {
    method: "PATCH",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ old_password: oldPassword, new_password: newPassword }),
  });
  await parseResponse<void>(res);
}

export interface FileQuery {
  page: number;
  pageSize: number;
  status: "available" | "deleted" | "all";
  q: string;
  previewKind: "" | PreviewKind;
  sort: "-uploaded_at" | "uploaded_at" | "original_name" | "size_bytes";
}

export async function listFiles(token: string, query: FileQuery): Promise<PageResult<FileItem>> {
  const res = await fetch(
    withQuery("/api/v1/admin/files", {
      page: query.page,
      page_size: query.pageSize,
      status: query.status,
      q: query.q,
      preview_kind: query.previewKind,
      sort: query.sort,
    }),
    { headers: authHeaders(token) },
  );
  return parseResponse<PageResult<FileItem>>(res);
}

export async function deleteFile(token: string, id: number): Promise<void> {
  const res = await fetch(`/api/v1/admin/files/${id}`, {
    method: "DELETE",
    headers: authHeaders(token),
  });
  await parseResponse<void>(res);
}

export async function batchDeleteFiles(token: string, ids: number[]): Promise<BatchResult> {
  const res = await fetch("/api/v1/admin/files/batch-delete", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ file_ids: ids }),
  });
  return parseResponse<BatchResult>(res);
}

export async function getTextPreview(token: string, id: number): Promise<TextPreview> {
  const res = await fetch(`/api/v1/admin/files/${id}/preview`, { headers: authHeaders(token) });
  return parseResponse<TextPreview>(res);
}

export async function getAuthorizedBlob(token: string, path: string): Promise<{ blob: Blob; filename?: string }> {
  const res = await fetch(path, { headers: authHeaders(token) });
  if (!res.ok) {
    await parseResponse<never>(res);
  }
  return { blob: await res.blob(), filename: filenameFromDisposition(res.headers.get("content-disposition")) };
}

function filenameFromDisposition(value: string | null): string | undefined {
  if (!value) return undefined;
  const utf8 = /filename\*=UTF-8''([^;]+)/i.exec(value);
  if (utf8?.[1]) return decodeURIComponent(utf8[1]);
  const plain = /filename="?([^";]+)"?/i.exec(value);
  return plain?.[1];
}

export interface EventQuery {
  page: number;
  pageSize: number;
  eventType: "" | "upload" | "download" | "delete";
  actorRole: "" | "anonymous" | "admin" | "system";
  result: "" | "success" | "failed";
  fileCode: string;
}

export async function listEvents(token: string, query: EventQuery): Promise<PageResult<FileEvent>> {
  const res = await fetch(
    withQuery("/api/v1/admin/events", {
      page: query.page,
      page_size: query.pageSize,
      event_type: query.eventType,
      actor_role: query.actorRole,
      result: query.result,
      file_code: query.fileCode,
      sort: "-occurred_at",
    }),
    { headers: authHeaders(token) },
  );
  return parseResponse<PageResult<FileEvent>>(res);
}

export async function deleteEvent(token: string, id: number): Promise<void> {
  const res = await fetch(`/api/v1/admin/events/${id}`, {
    method: "DELETE",
    headers: authHeaders(token),
  });
  await parseResponse<void>(res);
}

export async function batchDeleteEvents(token: string, ids: number[]): Promise<BatchResult> {
  const res = await fetch("/api/v1/admin/events/batch-delete", {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ event_ids: ids }),
  });
  return parseResponse<BatchResult>(res);
}
