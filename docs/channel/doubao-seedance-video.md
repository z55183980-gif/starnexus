# 星域互联（Starnexus）Doubao Seedance 系列视频生成接入规范

**文档版本：** 1.2
**适用范围：** DoubaoVideo2.0 渠道（渠道类型 62）的 Seedance 系列视频模型
**读者：** API 调用方、业务后端、客户端 SDK 和运维人员

本文说明如何通过星域互联（Starnexus）统一视频接口提交 DoubaoVideo2.0 视频任务。该渠道把 URL、数据 URL 或上传文件直接转换为上游 `content`，不创建、不查询也不依赖素材库资源。

## 1. 接口流程

```mermaid
flowchart LR
    A[调用方] -->|Bearer API Key| B[星域互联统一视频接口]
    B --> C[请求校验与 content 规范化]
    C --> D[DoubaoVideo2.0 上游]
    D --> E[任务状态]
    E --> F[鉴权内容接口]
```

调用方负责：

1. 调用视频生成接口；
2. 传入可访问的 HTTPS 图片 URL、数据 URL 或纯 Base64 图片；
3. 保存接口返回的公开任务 ID；
4. 轮询任务状态，并通过内容接口读取结果。

调用方和网关均不需要管理素材组、素材凭据或内部素材引用。公网 URL 必须在上游读取期间保持可用；上传文件则由网关直接转换为数据 URL 后转发。

## 2. 认证

```http
Authorization: Bearer sk-your-api-key
Content-Type: application/json
```

API Key 仅用于星域互联（Starnexus）服务。不要把管理员密钥、素材凭据或其他内部配置发送给最终用户。

## 3. 创建视频任务

```http
POST /v1/video/generations
```

示例：

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "人物轻微转头，保持面部稳定，固定机位",
  "images": [
    "https://cdn.example.com/portrait.jpg"
  ],
  "seconds": "5",
  "metadata": {
    "resolution": "720p"
  }
}
```

字段说明：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---:|---|
| `model` | string | 是 | Doubao Seedance 系列模型名称，以服务端模型列表为准 |
| `prompt` | string | 是 | 视频生成提示词 |
| `images` | string[] | 否 | 图片 URL、数据 URL 或纯 Base64；多图数量以具体模型能力为准 |
| `image` | string | 否 | 单图兼容字段，服务端会转换为 `images` |
| `seconds` | string | 否 | 视频时长，建议传字符串，例如 `"5"` |
| `duration` | integer | 否 | 时长兼容字段；优先使用 `seconds` |
| `metadata` | object | 否 | 模型支持的扩展字段，例如 `resolution`、`ratio` 等 |

创建成功后，响应中的 `id` 或 `task_id` 是公开任务 ID。调用方不得依赖或猜测内部任务标识。

### 3.1 OpenAI Videos 兼容接口

```http
POST /v1/videos
```

JSON 请求可以使用对象形式（推荐）或历史字符串形式的 `input_reference`：

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "人物轻微转头，保持面部稳定",
  "input_reference": {
    "image_url": "https://cdn.example.com/portrait.jpg"
  },
  "seconds": "8",
  "size": "1280x720"
}
```

也可直接传 DoubaoVideo2.0 上游兼容的顶层 `content` 数组。`content` 与 `input_reference`、`image`、`images` 互斥；一个请求中最多有一个文本项、一个 `first_frame` 和一个 `last_frame`，且 `last_frame` 必须同时提供 `first_frame`。

## 4. 图片传输规范

### 4.1 支持的传输形式

HTTPS URL：

```json
{
  "images": ["https://cdn.example.com/portrait.jpg"]
}
```

数据 URL：

```json
{
  "images": ["data:image/png;base64,iVBORw0KGgoAAA..."]
}
```

也支持不带 `data:` 前缀的纯 Base64 字符串，但推荐携带完整 MIME 前缀，便于格式识别和问题排查。

### 4.2 支持的图片格式

| 格式 | MIME 类型 | 说明 |
|---|---|---|
| JPEG/JPG | `image/jpeg`、`image/jpg` | 支持 EXIF 方向校正 |
| PNG | `image/png` | 支持透明图；必要时会转换为 JPEG |
| WebP | `image/webp` | 支持静态 WebP |
| GIF | `image/gif` | 检查基于解码后的图像帧 |

暂不支持 HEIC/HEIF、BMP、TIFF、SVG、PDF，以及其他无法解码的非光栅图片格式。

### 4.3 本地文件

原生接口 `/v1/video/generations` 使用 JSON，不接受把二进制图片作为 multipart 文件提交。本地文件可先转换为数据 URL，或上传到稳定的对象存储。

兼容接口 `/v1/videos` 额外接受名为 `input_reference` 的 multipart 图片文件：

```bash
curl https://gateway.example.com/v1/videos \
  -H "Authorization: Bearer sk-your-api-key" \
  -F "model=doubao-seedance-2-0-260128" \
  -F "prompt=人物轻微转头" \
  -F "seconds=8" \
  -F "size=1280x720" \
  -F "input_reference=@portrait.jpg"
```

文件必须是 JPEG、PNG、GIF 或 WebP，大小为 1 byte～20 MB。网关只进行格式和大小校验，然后在内存中编码为数据 URL 直接转发；不会上传素材库，也不会持久化原始文件。

## 5. 公网 URL 要求

当传入 URL 时，DoubaoVideo2.0 上游会读取该 URL，因此图片 URL 必须满足：

- 使用 HTTPS；
- 无需 Cookie、登录态或临时请求头即可访问；
- 返回正确的图片 `Content-Type`；
- 不跳转到登录页或 HTML 页面；
- 在任务完成前持续有效；
- 不限制服务端或生成节点的访问来源。

如果无法保证 URL 稳定性，推荐直接传带 MIME 的 Base64 数据 URL。

## 6. 图片限制

- URL 下载大小受服务端配置限制，默认不超过 64 MB；
- 请求体默认不超过 128 MB；
- `/v1/videos` multipart 单文件不超过 20 MB；
- JSON URL 或数据 URL 仍受网关请求体和上游限制；
- 最终尺寸、比例及引用数量必须符合所选 Seedance 模型能力。

推荐优先使用经过压缩的 JPEG、PNG 或 WebP，避免在请求中发送超大 Base64 图片。

### 6.1 计费配置

DoubaoVideo2.0 的模型基础倍率仍由管理端模型倍率配置决定。若需要按分辨率和是否含视频输入区分倍率，应为实际启用的模型配置 `VideoTokenPrice`；尤其是新增的 mini、2.5 或未来模型，不应套用其他型号的未验证价格。

## 7. 查询任务

```http
GET /v1/video/generations/{task_id}
```

必须使用创建任务响应中的公开任务 ID。

常见状态：

| 状态 | 含义 |
|---|---|
| `QUEUED` / `queued` | 已排队 |
| `SUBMITTED` | 已提交生成服务 |
| `IN_PROGRESS` / `in_progress` | 生成中 |
| `RETRYING` | 正在进行一次自动素材恢复，应继续等待 |
| `SUCCESS` / `completed` | 已完成 |
| `FAILURE` / `failed` | 失败 |

建议以 2～5 秒间隔轮询。`RETRYING` 属于处理中状态，不应立即创建重复任务。

## 8. 读取视频内容

任务完成后调用：

```http
GET /v1/videos/{task_id}/content
```

该接口需要 API Key 或有效登录态，并支持 HTTP Range，可用于断点下载和播放器拖动。

## 9. 错误处理

| 错误码 | HTTP | 含义 | 调用方建议 |
|---|---:|---|---|
| `invalid_image` | 422 | 图片格式、尺寸、比例或解码失败 | 更换或重新编码图片 |
| `invalid_reference_role` | 400 | 首尾帧角色组合无效 | 修正 `content[].role` 后重新提交 |
| `reference_image_download_failed` | 失败任务 | 上游无法下载图片 URL | 改用稳定公网 URL 或 multipart 文件 |
| `video_generation_failed` | 失败任务 | 上游生成失败且没有可公开的详细原因 | 使用任务 ID 联系管理员查询内部日志 |

如果创建请求超时且未收到完整响应，不要立即重复提交，以免产生重复计费任务。应先依据业务侧记录查询任务；无法确认时，请结合请求时间和请求 ID 联系服务管理员排查。

## 10. 推荐接入策略

1. 图片统一转换为 JPEG、PNG 或 WebP；
2. 使用稳定 HTTPS URL 或带 MIME 的 Base64 数据 URL；
3. 5 秒视频传 `seconds: "5"`；
4. 创建成功后立即保存公开任务 ID；
5. 按 2～5 秒间隔轮询，遇到 `RETRYING` 继续等待；
6. 完成后通过 `/v1/videos/{task_id}/content` 获取视频；
7. 不创建或管理素材组、素材凭据、`file_id` 或内部素材引用。
