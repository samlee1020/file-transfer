import {
  Activity,
  ArrowDownToLine,
  Check,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eye,
  FileArchive,
  FileText,
  KeyRound,
  Lock,
  LogOut,
  RefreshCw,
  Search,
  Shield,
  Trash2,
  UploadCloud,
  X,
} from "lucide-react";
import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import css from "highlight.js/lib/languages/css";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import go from "highlight.js/lib/languages/go";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import markdown from "highlight.js/lib/languages/markdown";
import php from "highlight.js/lib/languages/php";
import python from "highlight.js/lib/languages/python";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";
import { FormEvent, type ComponentProps, type KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkMath from "remark-math";
import {
  ApiError,
  batchDeleteEvents,
  batchDeleteFiles,
  changePassword,
  deleteEvent,
  deleteFile,
  getAuthorizedBlob,
  getMe,
  getScratchpad,
  getTextPreview,
  healthCheck,
  listEvents,
  listFiles,
  login,
  logout,
  saveScratchpad,
  uploadFile,
  type EventQuery,
  type FileQuery,
} from "./api";
import type { Admin, FileEvent, FileItem, PageResult, Scratchpad, TextPreview, UploadResult } from "./types";
import { classNames, downloadUrl, formatBytes, formatDate, normalizeCode, statusText } from "./utils";

const tokenKey = "file-transfer-admin-token";

hljs.registerLanguage("bash", bash);
hljs.registerLanguage("shell", bash);
hljs.registerLanguage("sh", bash);
hljs.registerLanguage("c", c);
hljs.registerLanguage("cpp", cpp);
hljs.registerLanguage("c++", cpp);
hljs.registerLanguage("cc", cpp);
hljs.registerLanguage("css", css);
hljs.registerLanguage("diff", diff);
hljs.registerLanguage("dockerfile", dockerfile);
hljs.registerLanguage("docker", dockerfile);
hljs.registerLanguage("go", go);
hljs.registerLanguage("ini", ini);
hljs.registerLanguage("toml", ini);
hljs.registerLanguage("java", java);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("js", javascript);
hljs.registerLanguage("jsx", javascript);
hljs.registerLanguage("json", json);
hljs.registerLanguage("kotlin", kotlin);
hljs.registerLanguage("kt", kotlin);
hljs.registerLanguage("markdown", markdown);
hljs.registerLanguage("md", markdown);
hljs.registerLanguage("php", php);
hljs.registerLanguage("python", python);
hljs.registerLanguage("py", python);
hljs.registerLanguage("ruby", ruby);
hljs.registerLanguage("rb", ruby);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("rs", rust);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("swift", swift);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("ts", typescript);
hljs.registerLanguage("tsx", typescript);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("html", xml);
hljs.registerLanguage("yaml", yaml);
hljs.registerLanguage("yml", yaml);

const markdownComponents: Components = {
  code({ className, children, ...props }: ComponentProps<"code">) {
    const match = /language-([a-zA-Z0-9_-]+)/.exec(className ?? "");
    const language = match?.[1]?.toLowerCase();
    const code = String(children).replace(/\n$/, "");
    if (!language || !hljs.getLanguage(language)) {
      return (
        <code className={className} {...props}>
          {children}
        </code>
      );
    }
    return (
      <code
        className={classNames("hljs", className)}
        dangerouslySetInnerHTML={{ __html: hljs.highlight(code, { language, ignoreIllegals: true }).value }}
        {...props}
      />
    );
  },
};

type Mode = "public" | "admin";
type AdminView = "files" | "events" | "security";
type ToastKind = "success" | "error" | "info";

interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

interface PreviewState {
  file: FileItem;
  text?: TextPreview;
  blobUrl?: string;
}

export function App(): JSX.Element {
  const [mode, setMode] = useState<Mode>("public");
  const [adminView, setAdminView] = useState<AdminView>("files");
  const [token, setToken] = useState(() => localStorage.getItem(tokenKey) ?? "");
  const [admin, setAdmin] = useState<Admin | null>(null);
  const [backendOnline, setBackendOnline] = useState<boolean | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);

  const notify = (kind: ToastKind, message: string) => {
    const id = Date.now() + Math.random();
    setToasts((items) => [...items, { id, kind, message }]);
    window.setTimeout(() => setToasts((items) => items.filter((item) => item.id !== id)), 3600);
  };

  const handleUnauthorized = () => {
    localStorage.removeItem(tokenKey);
    setToken("");
    setAdmin(null);
    setMode("admin");
    notify("error", "管理员会话已过期，请重新登录");
  };

  useEffect(() => {
    healthCheck().then(setBackendOnline);
  }, []);

  useEffect(() => {
    if (!token) return;
    getMe(token)
      .then(setAdmin)
      .catch((error) => {
        if (error instanceof ApiError && error.status === 401) handleUnauthorized();
      });
  }, [token]);

  const adminActive = Boolean(token && admin);

  return (
    <main className="min-h-screen px-4 py-4 text-ink sm:px-6 lg:px-8">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-5">
        <header className="glass sticky top-4 z-20 flex flex-col gap-4 rounded-[28px] px-4 py-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
          <div className="flex items-center gap-3">
            <div className="grid h-11 w-11 place-items-center rounded-2xl bg-ink text-paper shadow-control">
              <FileArchive size={22} />
            </div>
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.24em] text-copper">File Transfer</p>
              <h1 className="text-xl font-semibold tracking-normal sm:text-2xl">Sam Lee开发的轻量文件传输台</h1>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <StatusPill online={backendOnline} />
            <Segmented value={mode} onChange={setMode} />
            {adminActive ? (
              <button
                className="focus-ring inline-flex h-10 items-center gap-2 rounded-full border hairline bg-white/65 px-4 text-sm font-medium text-ink transition hover:bg-white"
                onClick={async () => {
                  try {
                    await logout(token);
                  } catch {
                    /* best effort */
                  }
                  localStorage.removeItem(tokenKey);
                  setToken("");
                  setAdmin(null);
                  notify("info", "已退出管理员模式");
                }}
              >
                <LogOut size={16} />
                退出
              </button>
            ) : null}
          </div>
        </header>

        {mode === "public" ? (
          <PublicWorkspace token={adminActive ? token : ""} notify={notify} />
        ) : adminActive ? (
          <AdminWorkspace
            admin={admin}
            token={token}
            view={adminView}
            setView={setAdminView}
            notify={notify}
            onUnauthorized={handleUnauthorized}
          />
        ) : (
          <LoginPanel
            onLogin={(data) => {
              localStorage.setItem(tokenKey, data.access_token);
              setToken(data.access_token);
              setAdmin(data.admin);
              notify("success", "管理员登录成功");
            }}
            notify={notify}
          />
        )}
      </div>
      <ToastStack items={toasts} onDismiss={(id) => setToasts((items) => items.filter((item) => item.id !== id))} />
    </main>
  );
}

function StatusPill({ online }: { online: boolean | null }): JSX.Element {
  const text = online === null ? "检测中" : online ? "后端在线" : "后端未连接";
  return (
    <span
      className={classNames(
        "inline-flex h-10 items-center gap-2 rounded-full border px-3 text-sm font-medium",
        online ? "border-moss/20 bg-moss/10 text-moss" : "border-copper/20 bg-copper/10 text-copper",
      )}
    >
      <span className={classNames("h-2 w-2 rounded-full", online ? "bg-moss" : "bg-copper")} />
      {text}
    </span>
  );
}

function Segmented({ value, onChange }: { value: Mode; onChange: (value: Mode) => void }): JSX.Element {
  return (
    <div className="inline-grid h-10 grid-cols-2 rounded-full border hairline bg-white/55 p-1">
      {[
        ["public", "访客"] as const,
        ["admin", "管理员"] as const,
      ].map(([key, label]) => (
        <button
          key={key}
          className={classNames(
            "focus-ring rounded-full px-4 text-sm font-medium transition",
            value === key ? "bg-ink text-paper shadow-control" : "text-muted hover:text-ink",
          )}
          onClick={() => onChange(key)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function PublicWorkspace({
  token,
  notify,
}: {
  token: string;
  notify: (kind: ToastKind, message: string) => void;
}): JSX.Element {
  const [upload, setUpload] = useState<UploadResult | null>(null);
  const [code, setCode] = useState("");

  return (
    <section className="grid gap-5 lg:grid-cols-[1.15fr_0.85fr]">
      <UploadPanel
        token={token}
        title="上传小文件"
        caption="支持匿名上传；管理员登录后上传会记录为管理员行为。"
        onUploaded={setUpload}
        notify={notify}
      />

      <div className="grid gap-5">
        <section className="glass rounded-[28px] p-5 sm:p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-medium text-muted">凭 6 位文件码下载</p>
              <h2 className="mt-1 text-2xl font-semibold">请注意大小写</h2>
            </div>
            <div className="grid h-11 w-11 place-items-center rounded-2xl bg-copper/10 text-copper">
              <KeyRound size={21} />
            </div>
          </div>

          <form
            className="mt-6 flex flex-col gap-3 sm:flex-row"
            onSubmit={(event) => {
              event.preventDefault();
              const safeCode = normalizeCode(code);
              if (safeCode.length !== 6) {
                notify("error", "请输入 6 位文件码");
                return;
              }
              window.location.href = downloadUrl(safeCode);
            }}
          >
            <input
              className="focus-ring h-12 flex-1 rounded-2xl border hairline bg-white/75 px-4 font-mono text-lg uppercase tracking-[0.18em] text-ink placeholder:font-sans placeholder:tracking-normal"
              placeholder="AB3DE9"
              value={code}
              onChange={(event) => setCode(normalizeCode(event.target.value))}
              inputMode="text"
            />
            <button className="focus-ring inline-flex h-12 items-center justify-center gap-2 rounded-2xl bg-ink px-5 font-semibold text-paper shadow-control transition hover:bg-black">
              <ArrowDownToLine size={18} />
              下载
            </button>
          </form>
        </section>

        <section className="glass code-grid min-h-[250px] rounded-[28px] p-5 sm:p-6">
          {upload ? (
            <UploadReceipt result={upload} notify={notify} />
          ) : (
            <div className="flex h-full min-h-[218px] flex-col justify-between">
              <div>
                <p className="text-sm font-medium text-muted">上传完成后</p>
                <h2 className="mt-1 text-2xl font-semibold">文件码在此显示</h2>
              </div>
              <p className="max-w-md text-sm leading-6 text-muted">
                文件码是下载入口。访客无法看到服务器上的文件列表，管理员可在控制台查看、预览、下载和删除。
              </p>
            </div>
          )}
        </section>
      </div>
    </section>
  );
}

function UploadPanel({
  token,
  title,
  caption,
  onUploaded,
  notify,
}: {
  token?: string;
  title: string;
  caption: string;
  onUploaded: (result: UploadResult) => void;
  notify: (kind: ToastKind, message: string) => void;
}): JSX.Element {
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const submitFile = async (file?: File | null) => {
    if (!file) return;
    setBusy(true);
    try {
      const result = await uploadFile(file, token);
      onUploaded(result);
      notify("success", `上传成功，文件码 ${result.code}`);
    } catch (error) {
      notify("error", readableError(error, "上传失败"));
    } finally {
      setBusy(false);
      if (inputRef.current) inputRef.current.value = "";
    }
  };

  return (
    <section className="glass rounded-[28px] p-5 sm:p-7">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <p className="text-sm font-medium text-muted">{caption}</p>
          <h2 className="mt-2 text-3xl font-semibold tracking-normal sm:text-4xl">{title}</h2>
        </div>
        <div className="grid h-12 w-12 place-items-center rounded-2xl bg-ink text-paper">
          <UploadCloud size={24} />
        </div>
      </div>

      <label
        className={classNames(
          "focus-ring mt-8 flex min-h-[290px] flex-col items-center justify-center rounded-[24px] border border-dashed border-ink/18 bg-white/52 px-5 text-center transition",
          busy ? "opacity-70" : "hover:border-copper/60 hover:bg-white/78",
        )}
        onDragOver={(event) => event.preventDefault()}
        onDrop={(event) => {
          event.preventDefault();
          void submitFile(event.dataTransfer.files.item(0));
        }}
        tabIndex={0}
      >
        <input
          ref={inputRef}
          type="file"
          className="sr-only"
          disabled={busy}
          onChange={(event) => void submitFile(event.target.files?.item(0))}
        />
        <span className="grid h-16 w-16 place-items-center rounded-[22px] bg-ink text-paper shadow-control">
          {busy ? <RefreshCw className="animate-spin" size={26} /> : <FileText size={26} />}
        </span>
        <strong className="mt-6 text-xl font-semibold">{busy ? "正在上传" : "拖拽文件到这里，或点击选择"}</strong>
        <span className="mt-3 max-w-md text-sm leading-6 text-muted">单文件上传，默认上限 50 MiB。上传成功后会立即生成 6 位文件码。</span>
      </label>
    </section>
  );
}

function UploadReceipt({
  result,
  notify,
}: {
  result: UploadResult;
  notify: (kind: ToastKind, message: string) => void;
}): JSX.Element {
  return (
    <div className="flex min-h-[218px] flex-col justify-between gap-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-muted">上传成功</p>
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <span className="font-mono text-5xl font-semibold tracking-[0.18em] text-ink">{result.code}</span>
            <button
              className="focus-ring grid h-10 w-10 place-items-center rounded-full bg-white/75 text-ink shadow-control transition hover:bg-white"
              title="复制文件码"
              onClick={() => {
                void navigator.clipboard.writeText(result.code);
                notify("success", "文件码已复制");
              }}
            >
              <Copy size={17} />
            </button>
          </div>
        </div>
        <span className="rounded-full bg-moss/10 px-3 py-1 text-sm font-medium text-moss">{statusText(result.preview_kind)}</span>
      </div>
      <div className="grid gap-3 text-sm text-muted sm:grid-cols-2">
        <span className="truncate">文件：{result.original_name}</span>
        <span>大小：{formatBytes(result.size_bytes)}</span>
        <span>类型：{result.mime_type}</span>
        <span>时间：{formatDate(result.uploaded_at)}</span>
      </div>
      <a
        className="focus-ring inline-flex h-12 items-center justify-center gap-2 rounded-2xl bg-ink px-5 font-semibold text-paper shadow-control transition hover:bg-black"
        href={result.download_url}
      >
        <ArrowDownToLine size={18} />
        下载此文件
      </a>
    </div>
  );
}

function LoginPanel({
  onLogin,
  notify,
}: {
  onLogin: (result: Awaited<ReturnType<typeof login>>) => void;
  notify: (kind: ToastKind, message: string) => void;
}): JSX.Element {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);

  const onSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      onLogin(await login(password));
      setPassword("");
    } catch (error) {
      notify("error", readableError(error, "登录失败"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="grid gap-5 lg:grid-cols-[0.9fr_1.1fr]">
      <div className="glass rounded-[28px] p-6 sm:p-8">
        <div className="grid h-12 w-12 place-items-center rounded-2xl bg-ink text-paper">
          <Shield size={23} />
        </div>
        <h2 className="mt-8 text-3xl font-semibold">管理员登录</h2>
        <p className="mt-3 text-sm leading-6 text-muted">登录后可查看文件列表、预览文本、图片、视频和 PDF，管理事件记录，并执行删除操作。</p>
        <form className="mt-8 grid gap-3" onSubmit={onSubmit}>
          <label className="grid gap-2 text-sm font-medium">
            密码
            <input
              className="focus-ring h-12 rounded-2xl border hairline bg-white/76 px-4 text-ink"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              placeholder="请输入管理员密码"
            />
          </label>
          <button
            className="focus-ring mt-2 inline-flex h-12 items-center justify-center gap-2 rounded-2xl bg-ink px-5 font-semibold text-paper shadow-control transition hover:bg-black disabled:opacity-60"
            disabled={busy || password.length === 0}
          >
            {busy ? <RefreshCw className="animate-spin" size={18} /> : <Lock size={18} />}
            登录
          </button>
        </form>
      </div>
      <div className="glass code-grid flex min-h-[360px] flex-col justify-between rounded-[28px] p-6 sm:p-8">
        <div>
          <p className="text-sm font-medium text-copper">Console</p>
          <h2 className="mt-2 max-w-2xl text-4xl font-semibold tracking-normal sm:text-5xl">文件管理界面登录后可见</h2>
        </div>
        <div className="grid gap-3 text-sm text-muted sm:grid-cols-3">
          <span>管理员可见全部文件</span>
          <span>访客仅凭码下载</span>
          <span>上传/下载/删除均有事件记录</span>
        </div>
      </div>
    </section>
  );
}

function AdminWorkspace({
  admin,
  token,
  view,
  setView,
  notify,
  onUnauthorized,
}: {
  admin: Admin | null;
  token: string;
  view: AdminView;
  setView: (view: AdminView) => void;
  notify: (kind: ToastKind, message: string) => void;
  onUnauthorized: () => void;
}): JSX.Element {
  const [latestUpload, setLatestUpload] = useState<UploadResult | null>(null);

  return (
    <section className="grid gap-5">
      <div className="glass flex flex-col gap-4 rounded-[28px] p-5 sm:flex-row sm:items-center sm:justify-between sm:p-6">
        <div>
          <p className="text-sm font-medium text-muted">当前管理员：{admin?.username ?? "admin"}</p>
          <h2 className="mt-1 text-2xl font-semibold">文件与事件控制台</h2>
        </div>
        <nav className="inline-flex flex-wrap gap-2">
          {[
            ["files", "文件"] as const,
            ["events", "事件"] as const,
            ["security", "安全"] as const,
          ].map(([key, label]) => (
            <button
              key={key}
              className={classNames(
                "focus-ring h-10 rounded-full px-4 text-sm font-semibold transition",
                view === key ? "bg-ink text-paper shadow-control" : "border hairline bg-white/60 text-muted hover:text-ink",
              )}
              onClick={() => setView(key)}
            >
              {label}
            </button>
          ))}
        </nav>
      </div>

      {view === "files" ? (
        <FilesPanel token={token} notify={notify} onUnauthorized={onUnauthorized} latestUpload={latestUpload} setLatestUpload={setLatestUpload} />
      ) : view === "events" ? (
        <EventsPanel token={token} notify={notify} onUnauthorized={onUnauthorized} />
      ) : (
        <SecurityPanel token={token} notify={notify} admin={admin} />
      )}

      <ScratchpadPanel token={token} notify={notify} onUnauthorized={onUnauthorized} />
    </section>
  );
}

function FilesPanel({
  token,
  notify,
  onUnauthorized,
  latestUpload,
  setLatestUpload,
}: {
  token: string;
  notify: (kind: ToastKind, message: string) => void;
  onUnauthorized: () => void;
  latestUpload: UploadResult | null;
  setLatestUpload: (result: UploadResult) => void;
}): JSX.Element {
  const [query, setQuery] = useState<FileQuery>({
    page: 1,
    pageSize: 20,
    status: "available",
    q: "",
    previewKind: "",
    sort: "-uploaded_at",
  });
  const [data, setData] = useState<PageResult<FileItem> | null>(null);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [preview, setPreview] = useState<PreviewState | null>(null);

  const load = async () => {
    setLoading(true);
    try {
      setData(await listFiles(token, query));
      setSelected([]);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onUnauthorized();
      else notify("error", readableError(error, "加载文件失败"));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, [query.page, query.pageSize, query.status, query.previewKind, query.sort]);

  useEffect(() => {
    const id = window.setTimeout(() => void load(), 260);
    return () => window.clearTimeout(id);
  }, [query.q]);

  const stats = useMemo(() => {
    const items = data?.items ?? [];
    return {
      total: data?.total ?? 0,
      bytes: items.reduce((sum, item) => sum + item.size_bytes, 0),
      downloads: items.reduce((sum, item) => sum + item.download_count, 0),
    };
  }, [data]);

  const toggleSelected = (id: number) => {
    setSelected((items) => (items.includes(id) ? items.filter((item) => item !== id) : [...items, id]));
  };

  const handlePreview = async (file: FileItem) => {
    try {
      if (file.preview_kind === "pdf" || file.preview_kind === "image" || file.preview_kind === "video") {
        const { blob } = await getAuthorizedBlob(token, `/api/v1/admin/files/${file.id}/preview`);
        setPreview({ file, blobUrl: URL.createObjectURL(blob) });
        return;
      }
      const text = await getTextPreview(token, file.id);
      setPreview({ file, text });
    } catch (error) {
      notify("error", readableError(error, "预览失败"));
    }
  };

  const handleDownload = async (file: FileItem) => {
    try {
      const { blob, filename } = await getAuthorizedBlob(token, `/api/v1/admin/files/${file.id}/download`);
      saveBlob(blob, filename ?? file.original_name);
      notify("success", "下载已开始");
      void load();
    } catch (error) {
      notify("error", readableError(error, "下载失败"));
    }
  };

  return (
    <>
      <section className="grid gap-5 xl:grid-cols-[360px_1fr]">
        <div className="grid gap-5">
          <UploadPanel
            token={token}
            title="管理员上传"
            caption="上传后会写入管理员身份和事件记录。"
            onUploaded={(result) => {
              setLatestUpload(result);
              void load();
            }}
            notify={notify}
          />
          {latestUpload ? (
            <section className="glass rounded-[28px] p-5">
              <UploadReceipt result={latestUpload} notify={notify} />
            </section>
          ) : null}
        </div>

        <section className="glass rounded-[28px] p-4 sm:p-5">
          <div className="grid gap-3 border-b hairline pb-4 md:grid-cols-3">
            <Metric label="文件总数" value={String(stats.total)} />
            <Metric label="当前页体积" value={formatBytes(stats.bytes)} />
            <Metric label="当前页下载" value={String(stats.downloads)} />
          </div>

          <div className="mt-4 grid gap-3 lg:grid-cols-[1fr_auto_auto_auto]">
            <label className="relative">
              <Search className="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-muted" size={17} />
              <input
                className="focus-ring h-11 w-full rounded-2xl border hairline bg-white/70 pl-11 pr-4 text-sm"
                placeholder="搜索文件名或文件码"
                value={query.q}
                onChange={(event) => setQuery((old) => ({ ...old, page: 1, q: event.target.value }))}
              />
            </label>
            <Select value={query.status} onChange={(value) => setQuery((old) => ({ ...old, page: 1, status: value as FileQuery["status"] }))}>
              <option value="available">可用</option>
              <option value="deleted">已删除</option>
              <option value="all">全部</option>
            </Select>
            <Select value={query.previewKind} onChange={(value) => setQuery((old) => ({ ...old, page: 1, previewKind: value as FileQuery["previewKind"] }))}>
              <option value="">全部类型</option>
              <option value="text">文本</option>
              <option value="markdown">Markdown</option>
              <option value="pdf">PDF</option>
              <option value="image">图片</option>
              <option value="video">视频</option>
              <option value="none">不可预览</option>
            </Select>
            <Select value={query.sort} onChange={(value) => setQuery((old) => ({ ...old, page: 1, sort: value as FileQuery["sort"] }))}>
              <option value="-uploaded_at">最新上传</option>
              <option value="uploaded_at">最早上传</option>
              <option value="original_name">文件名</option>
              <option value="size_bytes">大小</option>
            </Select>
          </div>

          <div className="mt-4 overflow-hidden rounded-[22px] border hairline bg-white/45">
            <div className="hidden grid-cols-[44px_1.1fr_0.65fr_0.5fr_0.5fr_0.7fr_150px] gap-3 border-b hairline px-4 py-3 text-xs font-semibold uppercase tracking-[0.12em] text-muted lg:grid">
              <span />
              <span>文件</span>
              <span>文件码</span>
              <span>大小</span>
              <span>预览</span>
              <span>上传</span>
              <span className="text-right">操作</span>
            </div>
            <div className="divide-y divide-ink/10">
              {loading ? <EmptyState text="正在加载文件" spinning /> : null}
              {!loading && data?.items.length === 0 ? <EmptyState text="没有匹配文件" /> : null}
              {!loading &&
                data?.items.map((file) => (
                  <FileRow
                    key={file.id}
                    file={file}
                    checked={selected.includes(file.id)}
                    onCheck={() => toggleSelected(file.id)}
                    onPreview={() => void handlePreview(file)}
                    onDownload={() => void handleDownload(file)}
                    onDelete={async () => {
                      if (!window.confirm(`删除文件 ${file.original_name}？`)) return;
                      try {
                        await deleteFile(token, file.id);
                        notify("success", "文件已删除");
                        void load();
                      } catch (error) {
                        notify("error", readableError(error, "删除失败"));
                      }
                    }}
                  />
                ))}
            </div>
          </div>

          <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <button
              className="focus-ring inline-flex h-10 items-center justify-center gap-2 rounded-full border hairline bg-white/60 px-4 text-sm font-semibold text-copper transition hover:bg-white disabled:opacity-45"
              disabled={selected.length === 0}
              onClick={async () => {
                if (!window.confirm(`批量删除 ${selected.length} 个文件？`)) return;
                try {
                  const result = await batchDeleteFiles(token, selected);
                  notify("success", `已处理：删除 ${result.summary.deleted} 个`);
                  void load();
                } catch (error) {
                  notify("error", readableError(error, "批量删除失败"));
                }
              }}
            >
              <Trash2 size={16} />
              批量删除 {selected.length ? selected.length : ""}
            </button>
            <Pagination
              page={query.page}
              total={data?.total ?? 0}
              pageSize={query.pageSize}
              hasMore={data?.has_more ?? false}
              onPage={(page) => setQuery((old) => ({ ...old, page }))}
            />
          </div>
        </section>
      </section>

      {preview ? (
        <PreviewModal
          state={preview}
          onClose={() => {
            if (preview.blobUrl) URL.revokeObjectURL(preview.blobUrl);
            setPreview(null);
          }}
        />
      ) : null}
    </>
  );
}

function FileRow({
  file,
  checked,
  onCheck,
  onPreview,
  onDownload,
  onDelete,
}: {
  file: FileItem;
  checked: boolean;
  onCheck: () => void;
  onPreview: () => void;
  onDownload: () => void;
  onDelete: () => void;
}): JSX.Element {
  return (
    <article className="grid gap-3 px-4 py-4 text-sm lg:grid-cols-[44px_1.1fr_0.65fr_0.5fr_0.5fr_0.7fr_150px] lg:items-center">
      <label className="flex items-center gap-3">
        <input className="h-4 w-4 accent-[#1e201c]" type="checkbox" checked={checked} onChange={onCheck} />
        <span className="font-medium lg:hidden">{file.original_name}</span>
      </label>
      <div className="min-w-0">
        <p className="hidden truncate font-semibold lg:block">{file.original_name}</p>
        <p className="mt-1 truncate text-xs text-muted">{file.mime_type}</p>
      </div>
      <span className="w-fit rounded-full bg-ink px-3 py-1 font-mono text-xs tracking-[0.16em] text-paper">{file.code}</span>
      <span className="text-muted">{formatBytes(file.size_bytes)}</span>
      <span className="text-muted">{statusText(file.preview_kind)}</span>
      <span className="text-muted">{formatDate(file.uploaded_at)}</span>
      <div className="flex items-center justify-start gap-2 lg:justify-end">
        <IconButton label="预览" disabled={file.preview_kind === "none" || file.status !== "available"} onClick={onPreview}>
          <Eye size={16} />
        </IconButton>
        <IconButton label="下载" disabled={file.status !== "available"} onClick={onDownload}>
          <ArrowDownToLine size={16} />
        </IconButton>
        <IconButton label="删除" disabled={file.status === "deleted"} onClick={onDelete}>
          <Trash2 size={16} />
        </IconButton>
      </div>
    </article>
  );
}

function EventsPanel({
  token,
  notify,
  onUnauthorized,
}: {
  token: string;
  notify: (kind: ToastKind, message: string) => void;
  onUnauthorized: () => void;
}): JSX.Element {
  const [query, setQuery] = useState<EventQuery>({ page: 1, pageSize: 20, eventType: "", actorRole: "", result: "", fileCode: "" });
  const [data, setData] = useState<PageResult<FileEvent> | null>(null);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);

  const load = async () => {
    setLoading(true);
    try {
      setData(await listEvents(token, query));
      setSelected([]);
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onUnauthorized();
      else notify("error", readableError(error, "加载事件失败"));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, [query.page, query.eventType, query.actorRole, query.result]);

  useEffect(() => {
    const id = window.setTimeout(() => void load(), 260);
    return () => window.clearTimeout(id);
  }, [query.fileCode]);

  return (
    <section className="glass rounded-[28px] p-4 sm:p-5">
      <div className="flex flex-col gap-3 border-b hairline pb-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <p className="text-sm font-medium text-muted">Event Stream</p>
          <h2 className="text-2xl font-semibold">上传、下载、删除记录</h2>
        </div>
        <div className="grid gap-2 sm:grid-cols-4">
          <Select value={query.eventType} onChange={(value) => setQuery((old) => ({ ...old, page: 1, eventType: value as EventQuery["eventType"] }))}>
            <option value="">全部事件</option>
            <option value="upload">上传</option>
            <option value="download">下载</option>
            <option value="delete">删除</option>
          </Select>
          <Select value={query.actorRole} onChange={(value) => setQuery((old) => ({ ...old, page: 1, actorRole: value as EventQuery["actorRole"] }))}>
            <option value="">全部角色</option>
            <option value="anonymous">访客</option>
            <option value="admin">管理员</option>
            <option value="system">系统</option>
          </Select>
          <Select value={query.result} onChange={(value) => setQuery((old) => ({ ...old, page: 1, result: value as EventQuery["result"] }))}>
            <option value="">全部结果</option>
            <option value="success">成功</option>
            <option value="failed">失败</option>
          </Select>
          <input
            className="focus-ring h-10 rounded-2xl border hairline bg-white/70 px-3 font-mono text-sm uppercase tracking-[0.12em]"
            placeholder="文件码"
            value={query.fileCode}
            onChange={(event) => setQuery((old) => ({ ...old, page: 1, fileCode: normalizeCode(event.target.value) }))}
          />
        </div>
      </div>

      <div className="mt-4 overflow-hidden rounded-[22px] border hairline bg-white/45">
        <div className="hidden grid-cols-[44px_0.5fr_0.6fr_0.8fr_0.7fr_1fr_80px] gap-3 border-b hairline px-4 py-3 text-xs font-semibold uppercase tracking-[0.12em] text-muted lg:grid">
          <span />
          <span>类型</span>
          <span>文件码</span>
          <span>角色</span>
          <span>结果</span>
          <span>时间</span>
          <span className="text-right">操作</span>
        </div>
        <div className="divide-y divide-ink/10">
          {loading ? <EmptyState text="正在加载事件" spinning /> : null}
          {!loading && data?.items.length === 0 ? <EmptyState text="暂无事件" /> : null}
          {!loading &&
            data?.items.map((event) => (
              <article key={event.id} className="grid gap-3 px-4 py-4 text-sm lg:grid-cols-[44px_0.5fr_0.6fr_0.8fr_0.7fr_1fr_80px] lg:items-center">
                <input
                  className="h-4 w-4 accent-[#1e201c]"
                  type="checkbox"
                  checked={selected.includes(event.id)}
                  onChange={() => setSelected((items) => (items.includes(event.id) ? items.filter((id) => id !== event.id) : [...items, event.id]))}
                />
                <span className="font-semibold">{statusText(event.event_type)}</span>
                <span className="w-fit rounded-full bg-ink px-3 py-1 font-mono text-xs tracking-[0.16em] text-paper">{event.file_code}</span>
                <span className="text-muted">{statusText(event.actor_role)}</span>
                <span className={classNames("font-medium", event.result === "success" ? "text-moss" : "text-copper")}>{statusText(event.result)}</span>
                <span className="text-muted">{formatDate(event.occurred_at)}</span>
                <div className="flex justify-start lg:justify-end">
                  <IconButton
                    label="删除事件"
                    onClick={async () => {
                      try {
                        await deleteEvent(token, event.id);
                        notify("success", "事件已隐藏");
                        void load();
                      } catch (error) {
                        notify("error", readableError(error, "删除事件失败"));
                      }
                    }}
                  >
                    <Trash2 size={16} />
                  </IconButton>
                </div>
              </article>
            ))}
        </div>
      </div>

      <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <button
          className="focus-ring inline-flex h-10 items-center justify-center gap-2 rounded-full border hairline bg-white/60 px-4 text-sm font-semibold text-copper transition hover:bg-white disabled:opacity-45"
          disabled={selected.length === 0}
          onClick={async () => {
            try {
              const result = await batchDeleteEvents(token, selected);
              notify("success", `已隐藏 ${result.summary.deleted} 条事件`);
              void load();
            } catch (error) {
              notify("error", readableError(error, "批量删除事件失败"));
            }
          }}
        >
          <Trash2 size={16} />
          批量隐藏 {selected.length ? selected.length : ""}
        </button>
        <Pagination
          page={query.page}
          total={data?.total ?? 0}
          pageSize={query.pageSize}
          hasMore={data?.has_more ?? false}
          onPage={(page) => setQuery((old) => ({ ...old, page }))}
        />
      </div>
    </section>
  );
}

function SecurityPanel({
  token,
  notify,
  admin,
}: {
  token: string;
  notify: (kind: ToastKind, message: string) => void;
  admin: Admin | null;
}): JSX.Element {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [busy, setBusy] = useState(false);

  return (
    <section className="grid gap-5 lg:grid-cols-[0.85fr_1.15fr]">
      <div className="glass rounded-[28px] p-6">
        <div className="grid h-12 w-12 place-items-center rounded-2xl bg-ink text-paper">
          <Lock size={22} />
        </div>
        <h2 className="mt-7 text-2xl font-semibold">修改管理员密码</h2>
        <p className="mt-2 text-sm leading-6 text-muted">新密码长度需要 8 到 128 个字符。修改成功后，其他管理员会话会被吊销。</p>
        <form
          className="mt-6 grid gap-3"
          onSubmit={async (event) => {
            event.preventDefault();
            setBusy(true);
            try {
              await changePassword(token, oldPassword, newPassword);
              setOldPassword("");
              setNewPassword("");
              notify("success", "密码已更新");
            } catch (error) {
              notify("error", readableError(error, "修改密码失败"));
            } finally {
              setBusy(false);
            }
          }}
        >
          <input
            className="focus-ring h-12 rounded-2xl border hairline bg-white/70 px-4"
            type="password"
            placeholder="旧密码"
            value={oldPassword}
            onChange={(event) => setOldPassword(event.target.value)}
          />
          <input
            className="focus-ring h-12 rounded-2xl border hairline bg-white/70 px-4"
            type="password"
            placeholder="新密码"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
          />
          <button className="focus-ring h-12 rounded-2xl bg-ink font-semibold text-paper shadow-control disabled:opacity-60" disabled={busy || newPassword.length < 8}>
            保存新密码
          </button>
        </form>
      </div>
      <div className="glass code-grid rounded-[28px] p-6">
        <p className="text-sm font-medium text-muted">Account</p>
        <h2 className="mt-2 text-3xl font-semibold">admin</h2>
        <div className="mt-8 grid gap-3 text-sm text-muted sm:grid-cols-2">
          <span>ID：{admin?.id ?? "-"}</span>
          <span>用户名：{admin?.username ?? "-"}</span>
          <span className="sm:col-span-2">上次改密：{formatDate(admin?.password_changed_at)}</span>
        </div>
      </div>
    </section>
  );
}

function ScratchpadPanel({
  token,
  notify,
  onUnauthorized,
}: {
  token: string;
  notify: (kind: ToastKind, message: string) => void;
  onUnauthorized: () => void;
}): JSX.Element {
  const [draft, setDraft] = useState("");
  const [server, setServer] = useState<Scratchpad | null>(null);
  const [dirty, setDirty] = useState(false);
  const [remoteChanged, setRemoteChanged] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const serverUpdatedAt = server?.updated_at ?? null;

  const load = async (mode: "initial" | "manual" | "poll" = "manual") => {
    if (mode !== "poll") setLoading(true);
    try {
      const next = await getScratchpad(token);
      if (dirty && next.updated_at !== serverUpdatedAt) setRemoteChanged(true);
      setServer(next);
      if (!dirty) {
        setDraft(next.content);
        setRemoteChanged(false);
      }
      if (mode === "manual") notify("success", "草稿本已刷新");
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onUnauthorized();
      else if (mode !== "poll") notify("error", readableError(error, "读取草稿本失败"));
    } finally {
      if (mode !== "poll") setLoading(false);
    }
  };

  useEffect(() => {
    void load("initial");
  }, [token]);

  useEffect(() => {
    const id = window.setInterval(() => void load("poll"), 3000);
    return () => window.clearInterval(id);
  }, [token, dirty, serverUpdatedAt]);

  const size = new Blob([draft]).size;
  const max = server?.max_bytes ?? 1048576;
  const tooLarge = size > max;
  const renderedDraft = draft.trim().length > 0 ? draft : "_草稿本还是空的。_";

  const handleEditorKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Tab") return;
    event.preventDefault();
    const input = event.currentTarget;
    const value = input.value;
    const start = input.selectionStart;
    const end = input.selectionEnd;
    const tab = "\t";

    if (start === end) {
      const next = value.slice(0, start) + tab + value.slice(end);
      setDraft(next);
      setDirty(true);
      window.requestAnimationFrame(() => input.setSelectionRange(start + tab.length, start + tab.length));
      return;
    }

    const lineStart = value.lastIndexOf("\n", start - 1) + 1;
    const lineEnd = value.indexOf("\n", end);
    const blockEnd = lineEnd === -1 ? value.length : lineEnd;
    const before = value.slice(0, lineStart);
    const block = value.slice(lineStart, blockEnd);
    const after = value.slice(blockEnd);
    const lines = block.split("\n");

    if (event.shiftKey) {
      let removed = 0;
      const nextBlock = lines
        .map((line) => {
          if (line.startsWith(tab)) {
            removed += 1;
            return line.slice(1);
          }
          if (line.startsWith("  ")) {
            removed += 2;
            return line.slice(2);
          }
          return line;
        })
        .join("\n");
      const next = before + nextBlock + after;
      setDraft(next);
      setDirty(true);
      window.requestAnimationFrame(() => input.setSelectionRange(Math.max(lineStart, start - 1), Math.max(lineStart, end - removed)));
      return;
    }

    const nextBlock = lines.map((line) => tab + line).join("\n");
    const next = before + nextBlock + after;
    setDraft(next);
    setDirty(true);
    window.requestAnimationFrame(() => input.setSelectionRange(start + tab.length, end + lines.length * tab.length));
  };

  return (
    <section className="glass rounded-[28px] p-5 sm:p-6">
      <div className="flex flex-col gap-4 border-b hairline pb-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <p className="text-sm font-medium text-muted">Shared Scratchpad</p>
          <h2 className="mt-1 text-2xl font-semibold">管理员草稿本</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">左侧编辑 Markdown，右侧实时预览；内容保存在服务器 `data/scratchpad.txt`。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={classNames(
              "inline-flex h-10 items-center rounded-full border px-3 text-sm font-medium",
              remoteChanged ? "border-copper/20 bg-copper/10 text-copper" : dirty ? "border-ink/10 bg-white/62 text-muted" : "border-moss/20 bg-moss/10 text-moss",
            )}
          >
            {remoteChanged ? "服务器有新内容" : dirty ? "有未保存修改" : "已同步"}
          </span>
          <button
            className="focus-ring inline-flex h-10 items-center gap-2 rounded-full border hairline bg-white/65 px-4 text-sm font-semibold text-ink transition hover:bg-white disabled:opacity-50"
            disabled={loading}
            onClick={() => void load("manual")}
          >
            <RefreshCw className={loading ? "animate-spin" : ""} size={16} />
            刷新
          </button>
          <button
            className="focus-ring inline-flex h-10 items-center gap-2 rounded-full bg-ink px-4 text-sm font-semibold text-paper shadow-control transition hover:bg-black disabled:opacity-50"
            disabled={saving || !dirty || tooLarge}
            onClick={async () => {
              setSaving(true);
              try {
                const saved = await saveScratchpad(token, draft);
                setServer(saved);
                setDraft(saved.content);
                setDirty(false);
                setRemoteChanged(false);
                notify("success", "草稿本已保存");
              } catch (error) {
                if (error instanceof ApiError && error.status === 401) onUnauthorized();
                else notify("error", readableError(error, "保存草稿本失败"));
              } finally {
                setSaving(false);
              }
            }}
          >
            {saving ? <RefreshCw className="animate-spin" size={16} /> : <Check size={16} />}
            保存
          </button>
        </div>
      </div>

      {remoteChanged ? (
        <div className="mt-4 flex flex-col gap-3 rounded-2xl border border-copper/20 bg-copper/10 px-4 py-3 text-sm text-copper sm:flex-row sm:items-center sm:justify-between">
          <span>其他设备已经保存了新内容。你可以继续保存覆盖，或加载服务器版本。</span>
          <button
            className="focus-ring h-9 rounded-full bg-white/75 px-3 font-semibold text-copper"
            onClick={() => {
              setDraft(server?.content ?? "");
              setDirty(false);
              setRemoteChanged(false);
            }}
          >
            加载服务器版本
          </button>
        </div>
      ) : null}

      <div className="mt-4 grid gap-4 xl:grid-cols-2">
        <div className="min-w-0">
          <div className="mb-2 flex items-center justify-between px-1 text-sm">
            <span className="font-semibold text-ink">编辑</span>
            <span className="text-muted">Markdown / LaTeX / Code</span>
          </div>
          <textarea
            className="focus-ring min-h-[420px] w-full resize-y rounded-[22px] border hairline bg-white/68 p-4 font-mono text-sm leading-6 text-ink placeholder:font-sans placeholder:text-muted"
            value={draft}
            placeholder={"在这里输入 Markdown...\n\n例如：\n\n## 今日临时记录\n\n行内公式 $E = mc^2$\n\n```go\nfmt.Println(\"hello\")\n```\n\n$$\n\\int_0^1 x^2 dx = \\frac{1}{3}\n$$"}
            spellCheck={false}
            onChange={(event) => {
              setDraft(event.target.value);
              setDirty(true);
            }}
            onKeyDown={handleEditorKeyDown}
          />
        </div>

        <div className="min-w-0">
          <div className="mb-2 flex items-center justify-between px-1 text-sm">
            <span className="font-semibold text-ink">预览</span>
            <span className="text-muted">实时渲染</span>
          </div>
          <div className="markdown-body min-h-[420px] overflow-auto rounded-[22px] border hairline bg-white/72 p-5 text-sm leading-7 text-ink">
            <ReactMarkdown remarkPlugins={[remarkMath]} rehypePlugins={[rehypeKatex]} components={markdownComponents}>
              {renderedDraft}
            </ReactMarkdown>
          </div>
        </div>
      </div>

      <div className="mt-3 flex flex-col gap-2 text-sm text-muted sm:flex-row sm:items-center sm:justify-between">
        <span className={tooLarge ? "font-medium text-copper" : ""}>
          {formatBytes(size)} / {formatBytes(max)}
        </span>
        <span>服务器更新时间：{formatDate(server?.updated_at)}</span>
      </div>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="rounded-[20px] bg-white/50 px-4 py-3">
      <p className="text-xs font-semibold uppercase tracking-[0.15em] text-muted">{label}</p>
      <p className="mt-1 text-2xl font-semibold">{value}</p>
    </div>
  );
}

function Select({
  value,
  onChange,
  children,
}: {
  value: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <select
      className="focus-ring h-10 rounded-2xl border hairline bg-white/70 px-3 text-sm font-medium text-ink"
      value={value}
      onChange={(event) => onChange(event.target.value)}
    >
      {children}
    </select>
  );
}

function IconButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <button
      className="focus-ring grid h-9 w-9 place-items-center rounded-full border hairline bg-white/64 text-ink transition hover:bg-white disabled:opacity-35"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function Pagination({
  page,
  total,
  pageSize,
  hasMore,
  onPage,
}: {
  page: number;
  total: number;
  pageSize: number;
  hasMore: boolean;
  onPage: (page: number) => void;
}): JSX.Element {
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  return (
    <div className="flex items-center justify-between gap-3 text-sm text-muted sm:justify-end">
      <span>
        {from}-{to} / {total}
      </span>
      <div className="flex gap-2">
        <IconButton label="上一页" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <ChevronLeft size={16} />
        </IconButton>
        <IconButton label="下一页" disabled={!hasMore} onClick={() => onPage(page + 1)}>
          <ChevronRight size={16} />
        </IconButton>
      </div>
    </div>
  );
}

function EmptyState({ text, spinning }: { text: string; spinning?: boolean }): JSX.Element {
  return (
    <div className="grid min-h-[180px] place-items-center px-4 py-10 text-center text-sm text-muted">
      <div>
        {spinning ? <RefreshCw className="mx-auto mb-3 animate-spin" size={22} /> : <Activity className="mx-auto mb-3" size={22} />}
        {text}
      </div>
    </div>
  );
}

function PreviewModal({ state, onClose }: { state: PreviewState; onClose: () => void }): JSX.Element {
  return (
    <div className="fixed inset-0 z-40 grid place-items-center bg-ink/38 p-4 backdrop-blur-sm" role="dialog" aria-modal="true">
      <section className="glass flex max-h-[88vh] w-full max-w-5xl flex-col overflow-hidden rounded-[28px]">
        <div className="flex items-center justify-between gap-4 border-b hairline px-5 py-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-muted">{state.file.code}</p>
            <h2 className="truncate text-xl font-semibold">{state.file.original_name}</h2>
          </div>
          <button className="focus-ring grid h-10 w-10 place-items-center rounded-full bg-white/70" aria-label="关闭预览" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <div className="min-h-[55vh] overflow-auto bg-white/58 p-5">
          {state.blobUrl && state.file.preview_kind === "image" ? (
            <div className="grid min-h-[55vh] place-items-center rounded-2xl bg-ink/5 p-3">
              <img className="max-h-[68vh] max-w-full rounded-xl object-contain" alt={state.file.original_name} src={state.blobUrl} />
            </div>
          ) : state.blobUrl && state.file.preview_kind === "video" ? (
            <video className="max-h-[68vh] w-full rounded-2xl bg-black" src={state.blobUrl} controls preload="metadata" />
          ) : state.blobUrl ? (
            <iframe className="h-[65vh] w-full rounded-2xl border hairline bg-white" title={state.file.original_name} src={state.blobUrl} />
          ) : state.text?.kind === "markdown" ? (
            <div className="markdown-body max-w-none text-sm leading-7">
              <ReactMarkdown>{state.text.content}</ReactMarkdown>
            </div>
          ) : (
            <pre className="whitespace-pre-wrap break-words rounded-2xl bg-ink/5 p-4 font-mono text-sm leading-6 text-ink">{state.text?.content}</pre>
          )}
        </div>
      </section>
    </div>
  );
}

function ToastStack({ items, onDismiss }: { items: Toast[]; onDismiss: (id: number) => void }): JSX.Element {
  return (
    <div className="fixed bottom-4 right-4 z-50 grid w-[min(360px,calc(100vw-2rem))] gap-2">
      {items.map((item) => (
        <button
          key={item.id}
          className={classNames(
            "focus-ring flex items-start gap-3 rounded-2xl border px-4 py-3 text-left text-sm shadow-control backdrop-blur-xl",
            item.kind === "error"
              ? "border-copper/20 bg-white/90 text-copper"
              : item.kind === "success"
                ? "border-moss/20 bg-white/90 text-moss"
                : "border-ink/10 bg-white/90 text-ink",
          )}
          onClick={() => onDismiss(item.id)}
        >
          <Check size={17} />
          {item.message}
        </button>
      ))}
    </div>
  );
}

function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 500);
}

function readableError(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    if (error.code === "PAYLOAD_TOO_LARGE") return "文件超过上传上限";
    if (error.code === "NOT_FOUND") return "文件不存在或已失效";
    return error.message || fallback;
  }
  if (error instanceof Error) return error.message;
  return fallback;
}
