export type PreviewKind = "none" | "text" | "markdown" | "pdf" | "image" | "video";
export type FileStatus = "available" | "deleted";
export type EventType = "upload" | "download" | "delete";
export type ActorRole = "anonymous" | "admin" | "system";
export type Result = "success" | "failed";

export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface ApiEnvelope<T> {
  request_id: string;
  data: T;
}

export interface UploadResult {
  code: string;
  original_name: string;
  size_bytes: number;
  mime_type: string;
  sha256: string;
  preview_kind: PreviewKind;
  uploaded_at: string;
  download_url: string;
}

export interface Admin {
  id: number;
  username: string;
  password_changed_at: string;
}

export interface LoginResult {
  access_token: string;
  token_type: "Bearer";
  expires_at: string;
  admin: Admin;
}

export interface FileItem {
  id: number;
  code: string;
  original_name: string;
  size_bytes: number;
  mime_type: string;
  extension: string;
  sha256: string;
  preview_kind: PreviewKind;
  status: FileStatus;
  uploaded_by_role: ActorRole;
  uploaded_at: string;
  download_count: number;
  last_downloaded_at: string | null;
  deleted_at: string | null;
}

export interface PageResult<T> {
  items: T[];
  page: number;
  page_size: number;
  total: number;
  has_more: boolean;
}

export interface TextPreview {
  kind: "text" | "markdown";
  encoding: "utf-8";
  content: string;
  truncated: boolean;
  bytes_read: number;
}

export interface Scratchpad {
  content: string;
  updated_at: string | null;
  size_bytes: number;
  max_bytes: number;
}

export interface FileEvent {
  id: number;
  file_id: number | null;
  file_code: string;
  original_name: string | null;
  event_type: EventType;
  actor_role: ActorRole;
  admin_id: number | null;
  result: Result;
  error_code: string | null;
  ip_address: string | null;
  user_agent: string | null;
  message: string | null;
  occurred_at: string;
}

export interface BatchSummary {
  deleted: number;
  already_deleted: number;
  not_found: number;
  failed: number;
}

export interface BatchResult {
  items: Array<{ id: number; code?: string | null; status: string; message?: string }>;
  summary: BatchSummary;
}
