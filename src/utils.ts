export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes)) return "-";
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** index;
  return `${value >= 10 || index === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`;
}

export function formatDate(value?: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function normalizeCode(value: string): string {
  return value.replace(/[^0-9a-z]/gi, "").toUpperCase().slice(0, 6);
}

export function classNames(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(" ");
}

export function downloadUrl(code: string): string {
  return `/api/v1/files/${encodeURIComponent(code)}/download`;
}

export function statusText(status: string): string {
  const map: Record<string, string> = {
    available: "可用",
    deleted: "已删除",
    upload: "上传",
    download: "下载",
    delete: "删除",
    anonymous: "访客",
    admin: "管理员",
    system: "系统",
    success: "成功",
    failed: "失败",
    none: "不可预览",
    text: "文本",
    markdown: "Markdown",
    pdf: "PDF",
    image: "图片",
    video: "视频",
  };
  return map[status] ?? status;
}
