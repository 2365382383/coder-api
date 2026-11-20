# New-API 集成边界

此目录已经是 New-API 仓库的一部分，画布生产构建由 `build.sh` 直接执行。
`web/src/embedded/` 保存宿主桥、服务端存储、认证请求和禁用扩展；
`web/src/embedded/playground/` 直接引用仓库根部 `web/src/features/playground/` 的图片类型、
能力归一化和请求构造，避免画布与 Playground 各自维护一套参数规则。

画布仍通过 `/canvas-app` 的 opaque-origin iframe 运行，iframe 是运行时安全边界；源码、
构建和测试都由 New-API 仓库维护，不再从外部仓库检出或在构建时应用源码补丁。
