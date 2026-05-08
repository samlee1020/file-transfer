# File Transfer

轻量文件传输系统，前后端在同一个仓库中。

- 后端：Go 标准库 HTTP 服务、SQLite 元数据、文件系统存储正文
- 前端：React + Vite + Tailwind CSS + TypeScript
- 数据：默认持久化到本地 `./data`
- 构建产物：前端生产包输出到 `./dist`

核心能力：

1. 访客和管理员都可以上传小文件，上传成功后获得 6 位文件码。
2. 访客不能查看文件列表，只能凭文件码下载。
3. 管理员可以登录、查看文件列表、下载、预览、删除和批量删除文件。
4. 管理员可以查看上传、下载、删除事件，并删除事件记录。

## 目录结构

```text
.
├── main.go                  # Go 后端入口
├── docker-compose.yml       # Docker Compose 后端服务
├── index.html               # Vite 前端入口
├── src/                     # React 前端源码
├── dist/                    # 前端构建产物
├── doc/                     # 需求、API、数据模型文档
└── data/                    # 本地持久化数据，运行后生成
```

## 推荐本地启动

### 1. 启动 Docker Compose 后端

当前 `docker-compose.yml` 已将容器内 `8080` 映射到本机 `19090`：

```bash
docker compose up -d --build
```

检查后端：

```bash
curl http://127.0.0.1:19090/healthz
```

预期响应：

```json
{"status":"ok"}
```

查看日志：

```bash
docker compose logs -f
```

停止后端：

```bash
docker compose down
```

### 2. 启动前端

首次启动先安装依赖：

```bash
npm install
```

启动 Vite，并把 API 代理到 Docker 后端 `19090`：

```bash
VITE_API_PROXY_TARGET=http://127.0.0.1:19090 npm run dev
```

浏览器打开：

```text
http://127.0.0.1:5173/
```

## 前端构建

```bash
npm run build
```

构建成功后会生成或更新：

```text
dist/
```

本地预览生产构建：

```bash
npm run preview
```

## 后端本机运行

不使用 Docker 时，可以直接运行 Go 后端：

```bash
go test ./...
DATA_DIR=./data ADDR=:8080 go run .
```

如果后端使用默认 `8080`，前端可直接启动：

```bash
npm run dev
```

如果后端使用其他端口，用 `VITE_API_PROXY_TARGET` 指定：

```bash
VITE_API_PROXY_TARGET=http://127.0.0.1:19090 npm run dev
```

## 默认管理员

- 用户名：`admin`
- 初始密码：见项目文档或部署配置

部署后应尽快在前端“安全”页修改管理员密码。

## 常用测试流程

访客流程：

1. 打开 `http://127.0.0.1:5173/`
2. 上传一个小文件
3. 复制上传成功后的 6 位文件码
4. 在下载区域输入文件码并下载

管理员流程：

1. 点击右上角“管理员”
2. 登录管理员账号
3. 查看文件列表
4. 测试预览、下载、删除、批量删除
5. 切换到“事件”查看上传、下载、删除记录
6. 切换到“安全”修改管理员密码

命令行快速验证：

```bash
curl http://127.0.0.1:19090/healthz

echo 'hello frontend' > /tmp/hello.txt
curl -sS http://127.0.0.1:19090/api/v1/files \
  -F 'file=@/tmp/hello.txt'
```

## 主要配置

后端环境变量：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP 监听地址 |
| `DATA_DIR` | `/data` | SQLite 与上传文件根目录 |
| `MAX_UPLOAD_BYTES` | `52428800` | 单文件上传上限 |
| `TEXT_PREVIEW_BYTES` | `1048576` | 文本/Markdown 预览读取上限 |
| `ADMIN_SESSION_TTL_HOURS` | `24` | 管理员会话有效期 |
| `PUBLIC_BASE_URL` | 空 | 可选，用于生成完整下载链接 |

前端开发环境变量：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VITE_API_PROXY_TARGET` | `http://127.0.0.1:8080` | Vite 开发服务器 API 代理目标 |

## API 文档

前端联调与接口说明见：

```text
doc/前端开发说明.md
doc/API设计.md
doc/数据模型设计.md
```
