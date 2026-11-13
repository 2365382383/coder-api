# 图片模型能力与 Playground 参数

New API 的图片工作台使用一份全局图片参数配置。Playground 与内嵌无限画布共享同一份模型目录和能力，不再为每个模型维护独立能力表。

## 模型发现

图片模型只依据持久化模型元数据中的公开端点进入图片工作台：

```json
{
  "image-generation": {
    "path": "/v1/images/generations",
    "method": "POST"
  }
}
```

模型名称、标签、价格和渠道默认端点都不能替代这条声明。新增图片模型时，先在模型管理中创建或更新元数据端点，再确认用户分组内的启用渠道提供该模型。

## 统一参数

- 画幅与分辨率：`aspect_ratio + resolution`。
- 默认值：`1:1 + 1K`。
- 分辨率选项：`1K`、`2K`、`4K`。
- 画幅包含常用比例、现有 Gemini/xAI 扩展比例以及 `auto`。
- 质量、输出格式、编辑、审核和压缩能力统一提供。
- 参数只描述可用选项，不保证每个上游模型都接受每个组合；上游拒绝时返回真实错误，不自动降级。

## 请求转换

| 渠道 | 请求字段 |
| --- | --- |
| OpenAI 兼容 | 将画幅和分辨率转换为合法 `size`，再移除 `aspect_ratio`、`resolution` |
| Gemini | 使用 `aspectRatio` 与 `imageSize` |
| xAI | 使用 `aspect_ratio` 与 `resolution` |

工作台预设由 OpenAI 渠道适配器转换，生图、JSON 编辑和 multipart 编辑使用同一转换入口。预设转换保证宽高按 16 像素对齐、单边不超过 3840，并满足 `3840x2160` 像素上限。`auto` 保持为 `size=auto`。

客户端显式提供 `size` 时保持原值，不使用工作台预设覆盖；没有提供尺寸或预设时，也不补写尺寸。公共 Images API 入口不按 `gpt-image-2` 前缀校验尺寸、输出格式或质量，不补写该模型族的默认值，具体参数合法性由上游判断。上游已有的模型必填、数量上限、旧 DALL-E 兼容与计费数量校验继续保留。

`complexity`、`markup`、`svg`、`frame_count`、`frame_duration_ms` 使用已合并的上游 DTO 与转发实现。工作台的选项列表不构成客户端 API 请求的参数白名单。

## 配置边界

- 不再读取或创建 `/data/image-capabilities.json`。
- 不再支持 `IMAGE_CAPABILITY_CONFIG_FILE`。
- `pkg/imagecapability` 只保留能力类型、全局默认值和 OpenAI 尺寸转换。
- Playground 的 `/api/user/image-models` 与画布的 `/api/canvas/bootstrap` 返回相同图片能力。
- `/api/pricing` 仍按既有价格和端点缓存返回图片能力，不改变计费事实。

## 回归检查

- 元数据声明图片端点的模型出现在 Playground 和画布图片工作台。
- 未声明图片端点的模型不会因名称或渠道类型进入图片工作台。
- 两个页面均显示 `1K`、`2K`、`4K`。
- OpenAI、Gemini、xAI 的请求字段符合各自协议。
- OpenAI 生图、JSON 编辑和 multipart 编辑均保留原生参数，且工作台预设转换后不丢失尺寸；显式零值和 markup 文件保持不变。
- 验证入口：`relay/helper/valid_image_request_test.go`、`relay/channel/openai/adaptor_image_test.go`；数量和溢出保护继续由 `relay/helper/openai_image_request_test.go` 覆盖。
- 删除旧能力文件后，服务日志不再出现该文件的加载记录。

本次参数边界恢复已通过 `relay/helper`、`relay/channel/openai`、`relay/channel/gemini`、`relay/channel/xai`、`pkg/imagecapability` 的完整包测试，以及 `relay`、`middleware` 的图片/Playground 集成用例。主模块 `go build ./...`、独立 relaykit 模块的 DTO 测试和 `GOWORK=off go build ./...` 均通过。

Controller 图片/Playground 测试仍受已有测试编译错误阻塞：`auth_flow_test.go` 的两处 OAuth 函数调用缺少参数，`plugin_native_e2e_test.go` 的两处额度类型不匹配。使用原始 `9f4188d37` 的生产源码临时覆盖后可复现相同错误，不属于本次参数边界变更。
