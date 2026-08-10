# ZQBAPI 视频素材自动化

ZQBAPI 渠道可在本地检查图片，在需要时自动上传素材、创建素材记录、等待素材生效，再提交视频任务。该逻辑仅对 `ZQBAPI` 渠道生效，不改变其他 DoubaoVideo 渠道。

## 服务端配置

素材接口凭据只从服务端环境变量读取，不写入渠道设置或任务数据：

- `ZQBAPI_MATERIAL_ACCESS_KEY_ID`：素材 API Access Key ID。
- `ZQBAPI_MATERIAL_SECRET_ACCESS_KEY`：素材 API Secret Access Key。
- `ZQBAPI_MATERIAL_BASE_URL`：可选，默认使用 ZQBAPI 渠道默认域名。
- `ZQBAPI_MATERIAL_GROUP_ID`：可选的全局素材组 ID；渠道可覆盖。
- `ZQBAPI_MATERIAL_PROJECT_NAME`：可选，默认 `default`；渠道可覆盖。
- `ZQBAPI_MATERIAL_REGION`：可选，默认 `cn-beijing`。

生产视频归档为可选能力：

- `VIDEO_RESULT_CACHE_DIR`：启用归档的共享绝对目录。S1/S2/S3 必须挂载同一个共享目录；未配置时内容代理回退到上游结果 URL。
- `VIDEO_RESULT_CACHE_MAX_BYTES`：单个视频最大字节数，默认 1 GiB，允许 1 MiB 到 10 GiB。
- `VIDEO_RESULT_CACHE_RETENTION_DAYS`：归档保留天数，默认 7 天。

## 渠道设置

渠道编辑页的“ZQBAPI 素材设置”支持以下模式：

- `off`：不做素材处理。
- `retry_only`：先直接提交；仅在上游明确返回 `may contain real person` 后自动创建素材并重试一次。
- `face_preflight`：默认。本地轻量人脸检测命中时先创建素材；误判为无脸但被上游拒绝时仍可自动恢复。
- `always`：所有图片都先创建素材。

渠道可设置素材组 ID、项目名、素材组类型以及是否允许自动规范化。真人素材组应只包含同一个已获授权的人物。

## 处理与容错

- 本地检查不调用 LLM，不产生模型调用费用。
- JPEG、PNG、GIF、WebP 可在本地解码；JPEG EXIF 方向会被校正。当前本地预处理不解码 HEIC，HEIC 会返回明确的图片格式错误。
- 不符合尺寸或文件大小限制的图片会在允许时缩放并重新编码。
- 素材缓存键包含渠道、上游、素材组、图片 SHA-256 和规范化版本，不保存原图、公开 URL、AK 或 SK。
- 数据库唯一键和租约避免多节点重复创建；`CreateAsset` 不进行盲重试。上传和状态查询仅对安全的瞬时错误做有限重试。
- 确定性素材失败会短期负缓存；限流和 5xx 会很快重新尝试。
- 搜索日志关键字 `ZQBAPI material` 可查看创建、复用、生效和失败事件；日志不会输出素材源 URL 或密钥。
