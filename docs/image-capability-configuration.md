# 图片模型能力与 Playground 参数

New API 的图片工作台从启用能力和分组类型读取模型目录。被分配为 `图片` 类型的分组中的启用模型进入图片工作台；模型参数能力按实际图片渠道协议聚合，不按模型名称维护能力表。

## 模型发现

图片请求的实际路由仍依据持久化模型元数据中的公开端点：

```json
{
  "image-generation": {
    "path": "/v1/images/generations",
    "method": "POST"
  }
}
```

模型名称、标签、价格和渠道默认端点都不能替代这条声明。新增图片模型时，应把分组标记为 `图片`、确认启用能力，并为实际可生成的模型配置 `image-generation` 元数据和渠道映射。分组类型负责目录归类，端点元数据负责请求路由，两者职责不同。

## GPT-Image-2.6 模型核对

当前接入的两个模型为 `gpt-image-2.6-flare` 和 `gpt-image-2.6-sunburst`。两者均使用
OpenAI 图片协议，并通过持久化元数据的 `image-generation` 端点进入图片工作台；不能
因为模型名称包含 `gpt-image-2.6` 就在客户端或服务端复制一套逐模型能力规则。

两者共享当前 OpenAI 图片能力配置：`aspect_ratio + resolution`，默认 `1:1 + 1K`，
分辨率为 `1K`、`2K`、`4K`。OpenAI 官方模型页同时声明了图片生成和编辑端点；New API
的工作台发现仍以 `image-generation` 为入口，`/v1/images/edits` 继续沿用现有图片
编辑 relay 和同一分组/渠道模型清单，不把编辑端点误当成另一条模型目录。

Images 2.6 的原生字段 `complexity`、`markup`、`svg`、`frame_count`、
`frame_duration_ms` 必须由 OpenAI 请求 DTO 和适配器原样保留；工作台的画幅/分辨率
选项只负责预设转换，不构成公共 Images API 的字段白名单。官方能力依据：
[GPT-Image-2.6 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.6-flare)、
[GPT-Image-2.6 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.6-sunburst)
（2026-11-14 查询）。

价格与能力分开维护。官方页面列出的 2.6 token 价格不能直接替代本站图片数量结算；
本站当前的 `ModelPrice=1.5` 与 `image_count` 计量属于 New API 本地配置，必须以定价
快照和实际运行时读回为准，不能在本文件中表述为官方价格换算。

## 协议能力

- 画幅与分辨率：`aspect_ratio + resolution`。
- 默认值：`1:1 + 1K`。
- 分辨率选项：`1K`、`2K`、`4K`。
- 画幅包含常用比例、现有 Gemini/xAI 扩展比例以及 `auto`。
- OpenAI 兼容图片渠道使用统一工作台预设，并提供质量、输出格式、编辑、审核和压缩能力。
- Gemini 原生图片渠道使用 Gemini 支持的比例和分辨率，不提供 OpenAI 的质量、输出格式和编辑选项，单次最多 4 张。
- 同一模型在多个协议渠道提供时，公开能力取候选渠道的交集。
- 参数能力来自渠道协议和公开模型目录，不从模型名称推断；上游拒绝时返回真实错误，不自动降级。

## 请求转换

| 渠道 | 请求字段 |
| --- | --- |
| OpenAI 兼容 | 将画幅和分辨率转换为合法 `size`，再移除 `aspect_ratio`、`resolution` |
| Gemini | 使用 `aspectRatio` 与 `imageSize` |
| xAI | 使用 `aspect_ratio` 与 `resolution` |

工作台预设由 OpenAI 渠道适配器转换，生图、JSON 编辑和 multipart 编辑使用同一转换入口。预设转换保证宽高按 16 像素对齐、单边不超过 3840，并满足 `3840x2160` 像素上限。`auto` 保持为 `size=auto`。

客户端显式提供 `size` 时保持原值，不使用工作台预设覆盖；没有提供尺寸或预设时，也不补写尺寸。公共 Images API 入口不按 `gpt-image-2` 前缀校验尺寸、输出格式或质量，不补写该模型族的默认值，具体参数合法性由上游判断。上游已有的模型必填、数量上限、旧 DALL-E 兼容与计费数量校验继续保留。

multipart 编辑转发沿用上游原实现：`relay/channel/openai/adaptor.go` 的 `detectImageMimeType`
按文件扩展名推断文件 `Content-Type`，未知扩展名默认 `image/png`（上游 `0647872c1` 引入，
与 `upstream/main` 当前实现一致）。该行为属于上游逻辑，不做自研改动；文件格式合法性
最终由上游判断。

`complexity`、`markup`、`svg`、`frame_count`、`frame_duration_ms` 使用已合并的上游 DTO 与转发实现。工作台的选项列表不构成客户端 API 请求的参数白名单。

## 配置边界

- 不再读取或创建 `/data/image-capabilities.json`。
- 不再支持 `IMAGE_CAPABILITY_CONFIG_FILE`。
- `pkg/imagecapability` 保留能力类型、OpenAI 兼容默认值、Gemini 原生能力和尺寸转换。
- Playground 的 `/api/user/image-models` 与画布的 `/api/canvas/bootstrap` 按同一分组和渠道能力返回图片参数。
- `/api/pricing` 按既有价格、端点和渠道协议缓存返回图片能力，不改变计费事实。

## 回归检查

- `图片` 类型分组中的启用模型出现在 Playground 和画布图片工作台。
- 实际生成请求仍要求模型元数据声明图片端点并由渠道映射提供服务。
- OpenAI 兼容渠道与 Gemini 原生渠道显示各自协议支持的选项。
- OpenAI、Gemini、xAI 的请求字段符合各自协议。
- OpenAI 生图、JSON 编辑和 multipart 编辑均保留原生参数，且工作台预设转换后不丢失尺寸；显式零值和 markup 文件保持不变。
- 验证入口：`relay/helper/valid_image_request_test.go`、`relay/channel/openai/adaptor_image_test.go`；数量和溢出保护继续由 `relay/helper/openai_image_request_test.go` 覆盖。
- 删除旧能力文件后，服务日志不再出现该文件的加载记录。

本次参数边界恢复已通过 `relay/helper`、`relay/channel/openai`、`relay/channel/gemini`、`relay/channel/xai`、`pkg/imagecapability` 的完整包测试，以及 `relay`、`middleware` 的图片/Playground 集成用例。主模块 `go build ./...`、独立 relaykit 模块的 DTO 测试和 `GOWORK=off go build ./...` 均通过。

Controller 图片/Playground 测试仍受已有测试编译错误阻塞：`auth_flow_test.go` 的两处 OAuth 函数调用缺少参数，`plugin_native_e2e_test.go` 的两处额度类型不匹配。使用原始 `9f4188d37` 的生产源码临时覆盖后可复现相同错误，不属于本次参数边界变更。
