# 文件传输后端 API 设计

版本：v0.1  
状态：设计稿  
基础路径：`/api/v1`

## 1. 设计原则

1. 非管理员只能上传文件和凭文件码下载文件，不能枚举、搜索或探测文件集合。
2. 管理员通过用户名和密码登录，管理员接口全部放在 `/admin` 命名空间下，并要求会话 Bearer Token。
3. 文件码是便捷下载标识，固定为 6 位字母数字；输入可大小写，服务端统一转大写；下载失败时不泄露文件曾经存在、已删除或无权限等细节。
4. 文件上传和下载使用流式处理，避免将完整文件读入内存。
5. JSON 接口使用统一响应结构；文件下载和 PDF、图片、视频预览直接返回二进制流。
6. 系统记录简单的上传、下载、删除事件明细，管理员可以查看并删除事件记录。

## 2. 通用约定

### 2.1 时间与编码

| 项 | 约定 |
| --- | --- |
| 时间格式 | UTC RFC3339，例如 `2026-05-08T10:20:30.123Z`。 |
| 字符编码 | JSON 使用 UTF-8。 |
| 文件大小 | 单位为字节。 |
| 文件码 | 固定 6 位，输入格式为 `^[0-9A-Za-z]{6}$`，服务端统一转换为大写后存储和查询。 |
| Content-Type | JSON 请求使用 `application/json`，上传使用 `multipart/form-data`。 |

### 2.2 认证

管理员先通过登录接口获取会话令牌：

```http
POST /api/v1/admin/login
```

管理员接口要求：

```http
Authorization: Bearer <admin_session_token>
```

认证规则：

1. 初始管理员用户名为 `admin`，初始密码为 `password123`。
2. 服务端首次启动时初始化管理员密码哈希；数据库已有管理员记录时不得重置密码。
3. 管理员登录成功后返回会话令牌，后续管理员接口使用 `Authorization: Bearer <admin_session_token>`。
4. Token 缺失、过期、已吊销或不匹配时返回 `401 Unauthorized`。
5. 非管理员接口不要求认证。
6. 上传接口如果携带有效管理员会话 Token，则 `uploaded_by_role` 记录为 `admin`，否则记录为 `anonymous`。
7. 生产环境必须通过 HTTPS 或可信内网反向代理访问。

### 2.3 JSON 成功响应

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {}
}
```

### 2.4 JSON 错误响应

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "file is required",
    "details": {
      "field": "file"
    }
  }
}
```

错误码约定：

| HTTP 状态码 | `error.code` | 说明 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 参数格式错误、文件码格式错误、请求体无效。 |
| 401 | `UNAUTHORIZED` | 管理员认证失败。 |
| 404 | `NOT_FOUND` | 文件不存在、已删除或不可访问。 |
| 405 | `METHOD_NOT_ALLOWED` | HTTP 方法不允许。 |
| 409 | `CONFLICT` | 状态冲突，通常用于并发删除等场景。 |
| 413 | `PAYLOAD_TOO_LARGE` | 上传文件超过限制。 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | 请求 Content-Type 不支持。 |
| 422 | `UNSUPPORTED_PREVIEW` | 文件类型不支持预览。 |
| 429 | `RATE_LIMITED` | 触发限流。 |
| 500 | `INTERNAL` | 服务端内部错误。 |

## 3. 数据结构

### 3.1 FilePublic

上传成功后返回给所有用户的文件信息。

```json
{
  "code": "AB3DE9",
  "original_name": "example.txt",
  "size_bytes": 12345,
  "mime_type": "text/plain",
  "sha256": "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
  "preview_kind": "text",
  "uploaded_at": "2026-05-08T10:20:30.123Z",
  "download_url": "/api/v1/files/AB3DE9/download"
}
```

### 3.2 FileAdmin

管理员列表和详情使用的文件信息。

```json
{
  "id": 42,
  "code": "AB3DE9",
  "original_name": "example.txt",
  "size_bytes": 12345,
  "mime_type": "text/plain",
  "extension": "txt",
  "sha256": "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
  "preview_kind": "text",
  "status": "available",
  "uploaded_by_role": "anonymous",
  "uploaded_at": "2026-05-08T10:20:30.123Z",
  "download_count": 3,
  "last_downloaded_at": "2026-05-08T11:30:00.000Z",
  "deleted_at": null
}
```

### 3.3 Page

```json
{
  "items": [],
  "page": 1,
  "page_size": 20,
  "total": 135,
  "has_more": true
}
```

### 3.4 AdminInfo

```json
{
  "id": 1,
  "username": "admin",
  "password_changed_at": "2026-05-08T10:20:30.123Z"
}
```

### 3.5 FileEvent

```json
{
  "id": 501,
  "file_id": 42,
  "file_code": "AB3DE9",
  "original_name": "example.txt",
  "event_type": "download",
  "actor_role": "anonymous",
  "admin_id": null,
  "result": "success",
  "error_code": null,
  "ip_address": "203.0.113.10",
  "user_agent": "Mozilla/5.0",
  "message": null,
  "occurred_at": "2026-05-08T11:30:00.000Z"
}
```

## 4. 公共接口

### 4.1 健康检查

```http
GET /healthz
```

成功响应：

```json
{
  "status": "ok"
}
```

说明：

1. 该接口不放在 `/api/v1` 下，便于 Docker、反向代理和编排系统探活。
2. 只检查进程可响应。更深层的数据库检查可后续增加 `/readyz`。

### 4.2 上传文件

```http
POST /api/v1/files
Content-Type: multipart/form-data
```

表单字段：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `file` | 是 | 待上传文件。MVP 每次请求只允许一个文件。 |

限制：

1. 单文件大小默认不超过 `MAX_UPLOAD_BYTES`，默认 50 MiB。
2. 文件名按 UTF-8 处理，规范化后长度必须为 1 到 255。
3. 文件内容流式写入临时文件，不允许一次性读入内存。
4. 上传成功后记录一条 `upload` 事件。

成功响应：

```http
HTTP/1.1 201 Created
Content-Type: application/json
```

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "code": "AB3DE9",
    "original_name": "example.txt",
    "size_bytes": 12345,
    "mime_type": "text/plain",
    "sha256": "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
    "preview_kind": "text",
    "uploaded_at": "2026-05-08T10:20:30.123Z",
    "download_url": "/api/v1/files/AB3DE9/download"
  }
}
```

可能错误：

| 状态码 | 错误码 | 场景 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT` | 缺少 `file` 字段或文件名非法。 |
| 413 | `PAYLOAD_TOO_LARGE` | 文件超过大小限制。 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | 请求不是 `multipart/form-data`。 |
| 500 | `INTERNAL` | 写入文件或数据库失败。 |

### 4.3 凭码下载文件

```http
GET /api/v1/files/{code}/download
HEAD /api/v1/files/{code}/download
```

路径参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `code` | 是 | 6 位文件码，输入可大小写，服务端统一转换为大写。 |

成功响应：

```http
HTTP/1.1 200 OK
Content-Type: text/plain
Content-Length: 12345
Content-Disposition: attachment; filename*=UTF-8''example.txt
ETag: "sha256-8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4"
Accept-Ranges: bytes
```

响应体为文件二进制内容。

下载规则：

1. 支持 `GET` 和 `HEAD`。
2. 建议支持 `Range` 请求，以便浏览器和 PDF 查看器处理大文件或断点读取。
3. 文件不存在、已删除、文件码格式错误时，统一返回 `404 NOT_FOUND`，避免泄露文件状态。
4. 成功打开文件并开始响应后，更新 `download_count` 和 `last_downloaded_at`。
5. 如果某个文件码对应的原文件已删除且该码后来被复用，公共下载接口只下载当前 `available` 文件。
6. 下载成功后记录一条 `download` 事件。

可能错误：

| 状态码 | 错误码 | 场景 |
| --- | --- | --- |
| 404 | `NOT_FOUND` | 文件码不存在、已删除或格式错误。 |
| 416 | `INVALID_ARGUMENT` | `Range` 请求范围非法。 |
| 429 | `RATE_LIMITED` | 下载尝试过于频繁。 |

## 5. 管理员接口

除登录接口外，所有管理员接口都要求：

```http
Authorization: Bearer <admin_session_token>
```

### 5.1 管理员登录

```http
POST /api/v1/admin/login
Content-Type: application/json
```

请求体：

```json
{
  "username": "admin",
  "password": "password123"
}
```

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "access_token": "adm_sess_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
    "token_type": "Bearer",
    "expires_at": "2026-05-09T10:20:30.123Z",
    "admin": {
      "id": 1,
      "username": "admin",
      "password_changed_at": "2026-05-08T10:20:30.123Z"
    }
  }
}
```

规则：

1. 初始用户名固定为 `admin`，初始密码为 `password123`。
2. 登录成功后，服务端只保存令牌哈希。
3. 密码错误返回 `401 UNAUTHORIZED`，响应中不得区分用户名不存在和密码错误。

### 5.2 获取当前管理员

```http
GET /api/v1/admin/me
```

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "id": 1,
    "username": "admin",
    "password_changed_at": "2026-05-08T10:20:30.123Z"
  }
}
```

### 5.3 修改管理员密码

```http
PATCH /api/v1/admin/password
Content-Type: application/json
```

请求体：

```json
{
  "old_password": "password123",
  "new_password": "new-password-456"
}
```

成功响应：

```http
HTTP/1.1 204 No Content
```

规则：

1. 必须提供当前密码。
2. 新密码建议不少于 8 个字符，最长不超过 128 个字符。
3. 修改成功后更新密码哈希和 `password_changed_at`。
4. 修改成功后建议吊销除当前会话外的其他管理员会话。

### 5.4 管理员退出登录

```http
POST /api/v1/admin/logout
```

成功响应：

```http
HTTP/1.1 204 No Content
```

规则：吊销当前会话令牌。重复退出或令牌已吊销时仍可返回 `204 No Content`。

### 5.5 文件列表

```http
GET /api/v1/admin/files
```

查询参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `page` | `1` | 页码，从 1 开始。 |
| `page_size` | `20` | 每页数量，范围 1 到 100。 |
| `status` | `available` | `available`、`deleted`、`all`。 |
| `q` | 空 | 文件名或文件码模糊查询。 |
| `preview_kind` | 空 | `none`、`text`、`markdown`、`pdf`、`image`、`video`。 |
| `mime_type` | 空 | MIME 类型精确过滤。 |
| `uploaded_from` | 空 | 上传起始时间，UTC RFC3339。 |
| `uploaded_to` | 空 | 上传结束时间，UTC RFC3339。 |
| `sort` | `-uploaded_at` | 可选：`-uploaded_at`、`uploaded_at`、`original_name`、`size_bytes`。 |

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "items": [
      {
        "id": 42,
        "code": "AB3DE9",
        "original_name": "example.txt",
        "size_bytes": 12345,
        "mime_type": "text/plain",
        "extension": "txt",
        "sha256": "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
        "preview_kind": "text",
        "status": "available",
        "uploaded_by_role": "anonymous",
        "uploaded_at": "2026-05-08T10:20:30.123Z",
        "download_count": 3,
        "last_downloaded_at": "2026-05-08T11:30:00.000Z",
        "deleted_at": null
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 135,
    "has_more": true
  }
}
```

### 5.6 文件详情

```http
GET /api/v1/admin/files/{file_id}
```

路径参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `file_id` | 是 | 文件记录内部 ID，来自管理员文件列表。 |

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "id": 42,
    "code": "AB3DE9",
    "original_name": "example.txt",
    "size_bytes": 12345,
    "mime_type": "text/plain",
    "extension": "txt",
    "sha256": "8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4",
    "preview_kind": "text",
    "status": "available",
    "uploaded_by_role": "anonymous",
    "uploaded_at": "2026-05-08T10:20:30.123Z",
    "download_count": 3,
    "last_downloaded_at": "2026-05-08T11:30:00.000Z",
    "deleted_at": null
  }
}
```

可能错误：

| 状态码 | 错误码 | 场景 |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | 管理员认证失败。 |
| 404 | `NOT_FOUND` | 文件记录不存在。 |

### 5.7 管理员下载文件

```http
GET /api/v1/admin/files/{file_id}/download
HEAD /api/v1/admin/files/{file_id}/download
```

说明：

1. 响应格式与公共下载接口一致。
2. 该接口便于管理端从列表直接下载，必须认证。
3. 文件已删除时返回 `404 NOT_FOUND`。

### 5.8 预览文件

```http
GET /api/v1/admin/files/{file_id}/preview
```

预览规则：

| `preview_kind` | 响应类型 | 说明 |
| --- | --- | --- |
| `text` | `application/json` | 返回文本内容。 |
| `markdown` | `application/json` | 返回 Markdown 原文，由前端决定如何渲染。 |
| `pdf` | `application/pdf` | 返回 inline PDF 流。 |
| `image` | `image/*` | 返回 inline 图片流。 |
| `video` | `video/*` | 返回 inline 视频流，支持 Range 请求。 |
| `none` | 错误 | 返回 `422 UNSUPPORTED_PREVIEW`。 |

文本和 Markdown 成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "kind": "text",
    "encoding": "utf-8",
    "content": "hello\n",
    "truncated": false,
    "bytes_read": 6
  }
}
```

PDF、图片、视频成功响应：

```http
HTTP/1.1 200 OK
Content-Type: application/pdf
Content-Disposition: inline; filename*=UTF-8''example.pdf
Content-Length: 123456
Accept-Ranges: bytes
```

可能错误：

| 状态码 | 错误码 | 场景 |
| --- | --- | --- |
| 401 | `UNAUTHORIZED` | 管理员认证失败。 |
| 404 | `NOT_FOUND` | 文件不存在或已删除。 |
| 422 | `UNSUPPORTED_PREVIEW` | 文件类型不支持预览，或文本无法解码。 |

### 5.9 删除单个文件

```http
DELETE /api/v1/admin/files/{file_id}
```

成功响应：

```http
HTTP/1.1 204 No Content
```

删除规则：

1. 只有管理员可以删除。
2. 删除会移除服务器上的实际文件。
3. 元数据保留，状态改为 `deleted`。
4. 对已经删除的文件再次删除，返回 `204 No Content`，保证幂等。
5. 文件记录不存在时，返回 `404 NOT_FOUND`。
6. 文件删除后，其 6 位文件码可被后续上传重新使用。
7. 删除成功后记录一条 `delete` 事件。

### 5.10 批量删除文件

```http
POST /api/v1/admin/files/batch-delete
Content-Type: application/json
```

请求体：

```json
{
  "file_ids": [
    42,
    43
  ]
}
```

限制：

1. `file_ids` 必须是非空数组。
2. 单次最多 100 个文件 ID。
3. 服务端会去重。

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "items": [
      {
        "id": 42,
        "code": "AB3DE9",
        "status": "deleted"
      },
      {
        "id": 43,
        "code": null,
        "status": "not_found"
      }
    ],
    "summary": {
      "deleted": 1,
      "already_deleted": 0,
      "not_found": 1,
      "failed": 0
    }
  }
}
```

单项状态：

| 状态 | 说明 |
| --- | --- |
| `deleted` | 本次已删除成功。 |
| `already_deleted` | 文件此前已经删除。 |
| `not_found` | 文件记录不存在。 |
| `failed` | 删除失败，`message` 字段会说明原因。 |

说明：

1. 只要请求格式合法，批量接口整体返回 `200 OK`，单个文件结果放入 `items`。
2. 如果 `file_ids` 为空、数量超过限制或包含非法 ID，返回 `400 INVALID_ARGUMENT`。

### 5.11 事件明细列表

```http
GET /api/v1/admin/events
```

查询参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `page` | `1` | 页码，从 1 开始。 |
| `page_size` | `20` | 每页数量，范围 1 到 100。 |
| `event_type` | 空 | `upload`、`download`、`delete`。 |
| `actor_role` | 空 | `anonymous`、`admin`、`system`。 |
| `result` | 空 | `success`、`failed`。 |
| `file_id` | 空 | 按文件内部 ID 过滤。 |
| `file_code` | 空 | 按 6 位文件码过滤。 |
| `occurred_from` | 空 | 事件起始时间，UTC RFC3339。 |
| `occurred_to` | 空 | 事件结束时间，UTC RFC3339。 |
| `include_deleted` | `false` | 是否包含已被管理员删除的事件记录。 |
| `sort` | `-occurred_at` | 可选：`-occurred_at`、`occurred_at`。 |

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "items": [
      {
        "id": 501,
        "file_id": 42,
        "file_code": "AB3DE9",
        "original_name": "example.txt",
        "event_type": "download",
        "actor_role": "anonymous",
        "admin_id": null,
        "result": "success",
        "error_code": null,
        "ip_address": "203.0.113.10",
        "user_agent": "Mozilla/5.0",
        "message": null,
        "occurred_at": "2026-05-08T11:30:00.000Z"
      }
    ],
    "page": 1,
    "page_size": 20,
    "total": 1,
    "has_more": false
  }
}
```

### 5.12 删除单条事件记录

```http
DELETE /api/v1/admin/events/{event_id}
```

成功响应：

```http
HTTP/1.1 204 No Content
```

规则：

1. 删除事件记录只影响事件列表展示，不删除文件本身。
2. 推荐软删除事件记录，记录 `deleted_at` 和删除者管理员 ID。
3. 重复删除同一条事件记录返回 `204 No Content`。
4. 事件记录不存在时返回 `404 NOT_FOUND`。

### 5.13 批量删除事件记录

```http
POST /api/v1/admin/events/batch-delete
Content-Type: application/json
```

请求体：

```json
{
  "event_ids": [
    501,
    502
  ]
}
```

成功响应：

```json
{
  "request_id": "req_01HX6J9E2Y7V9K5VN5Z9ZK0Q7A",
  "data": {
    "items": [
      {
        "id": 501,
        "status": "deleted"
      },
      {
        "id": 502,
        "status": "not_found"
      }
    ],
    "summary": {
      "deleted": 1,
      "already_deleted": 0,
      "not_found": 1,
      "failed": 0
    }
  }
}
```

限制：

1. `event_ids` 必须是非空数组。
2. 单次最多 100 条事件记录。
3. 批量接口整体返回 `200 OK`，单项结果放入 `items`。

## 6. 接口清单

| 方法 | 路径 | 认证 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/healthz` | 否 | 健康检查。 |
| `POST` | `/api/v1/files` | 否，可选管理员会话 Token | 上传文件。 |
| `GET` | `/api/v1/files/{code}/download` | 否 | 凭码下载。 |
| `HEAD` | `/api/v1/files/{code}/download` | 否 | 下载前检查元信息。 |
| `POST` | `/api/v1/admin/login` | 否 | 管理员登录。 |
| `GET` | `/api/v1/admin/me` | 是 | 获取当前管理员。 |
| `PATCH` | `/api/v1/admin/password` | 是 | 修改管理员密码。 |
| `POST` | `/api/v1/admin/logout` | 是 | 管理员退出登录。 |
| `GET` | `/api/v1/admin/files` | 是 | 管理员文件列表。 |
| `GET` | `/api/v1/admin/files/{file_id}` | 是 | 管理员文件详情。 |
| `GET` | `/api/v1/admin/files/{file_id}/download` | 是 | 管理员下载。 |
| `HEAD` | `/api/v1/admin/files/{file_id}/download` | 是 | 管理员下载前检查元信息。 |
| `GET` | `/api/v1/admin/files/{file_id}/preview` | 是 | 管理员预览。 |
| `DELETE` | `/api/v1/admin/files/{file_id}` | 是 | 删除单个文件。 |
| `POST` | `/api/v1/admin/files/batch-delete` | 是 | 批量删除文件。 |
| `GET` | `/api/v1/admin/events` | 是 | 事件明细列表。 |
| `DELETE` | `/api/v1/admin/events/{event_id}` | 是 | 删除单条事件记录。 |
| `POST` | `/api/v1/admin/events/batch-delete` | 是 | 批量删除事件记录。 |

## 7. 安全要求

1. 文件码固定为 6 位字母数字，必须随机生成，禁止使用自增 ID、时间戳、文件名哈希作为文件码。
2. 所有公共下载失败统一返回 `404 NOT_FOUND`，不区分不存在和已删除。
3. 管理员密码必须保存哈希，禁止保存明文；推荐使用 `argon2id` 或 `bcrypt`。
4. 管理员会话 Token 在数据库中只保存哈希，Token 比较应使用常量时间比较。
5. 上传文件名必须清理路径分隔符、控制字符和空白边界。
6. 实际存储路径只能由服务端生成，不能接受客户端路径。
7. 下载响应必须使用安全的 `Content-Disposition`，避免响应头注入。
8. Markdown 预览返回原文，前端渲染时必须做 XSS 防护。
9. 建议对公共上传、凭码下载、管理员登录添加 IP 级限流。
10. 建议在反向代理层配置请求体大小限制，与 `MAX_UPLOAD_BYTES` 保持一致。
11. 初始密码 `password123` 只用于首次初始化，部署后应尽快通过修改密码接口更换。

## 8. 并发与一致性

1. 上传完成前不返回文件码，也不写入可下载记录。
2. 下载和删除并发时，如果下载已经成功打开文件，可以允许本次下载继续完成。
3. 删除提交后，新的下载请求必须返回 `404 NOT_FOUND`。
4. 批量删除内部按单个文件逐项处理，避免一个失败导致整批回滚。
5. 管理员列表分页按 `uploaded_at DESC, id DESC` 稳定排序，避免同一时间上传导致翻页重复或遗漏。
6. 文件码只在当前 `available` 文件中唯一；删除事务提交后，该文件码可被后续上传重新使用。
7. 事件记录删除不影响文件删除、下载次数等业务数据。

## 9. 版本策略

1. 当前 API 版本为 `/api/v1`。
2. 向后兼容变更可以直接在 v1 增加字段，客户端必须忽略未知字段。
3. 删除字段、改变字段含义、改变错误语义时必须发布 `/api/v2`。
4. 错误码字符串视为稳定契约，不能随意改名。
