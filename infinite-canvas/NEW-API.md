# New-API 集成边界

此目录已经是 New-API 仓库的一部分，画布生产构建由 `build.sh` 直接执行。
`web/src/embedded/` 保存宿主桥、服务端存储、认证请求和禁用扩展；
`web/src/embedded/playground/` 直接引用仓库根部 `web/src/features/playground/` 的图片类型、
能力归一化和请求构造，避免画布与 Playground 各自维护一套参数规则。

本地 Canvas 图片和视频参考素材通过 `/api/canvas/assets/:id/public-url` 发布：New API
使用服务端 `CANVAS_IMAGE_UPLOAD_API_KEY` 上传到图床/媒体床，浏览器和 iframe 不接触该 Key。
公开媒体链接默认有效 7 天；每次提交任务都会从本地 asset 重新发布，避免复用过期链接。
OpenAI 兼容及 Doubao 适配器会按各自请求格式传参考视频，具体模型能力仍由上游插件校验；
Sora 适配器不接受参考视频。

画布仍通过 `/canvas-app` 的 opaque-origin iframe 运行，iframe 是运行时安全边界；源码、
构建和测试都由 New-API 仓库维护，不再从外部仓库检出或在构建时应用源码补丁。
