# 自研功能保护清单

本文档只记录当前代码中仍需保护的自研功能和业务契约。内容以当前 `main` 工作树为准，作为后续合并和代码变更的当前验收入口；不保存变更来源、映射或更新时间线。

## 1. 当前范围

- 当前第 4 节包含 36 项现行自研功能和业务契约。
- 条目只描述当前必须保留的行为、数据边界、配置键、代码位置和验证入口。
- 上游实现只有在当前行为满足本条契约时才可采用；不以文件名称、历史提交或冲突结果代替行为验证。
- 已退休、已被上游完整覆盖或仅属测试治理的内容不列入当前保护项。

## 2. 当前合并规则

1. 合并或重构前逐项检查第 4 节全部保护项，记录当前代码是否仍满足契约。
2. 采用上游等价实现时，必须确认当前路径、数据/配置兼容性和验证入口仍成立。
3. 不用整文件 `ours`/`theirs` 解决语义冲突，不以“上游已重构”解释当前功能消失。
4. 保持现有数据库表、`options` 键、任务记录、账本记录、订阅余额和用户会话兼容。
5. 现行单前端代码位于 `web/src`；不恢复已退休的 `web/default` 或 `web/classic`。
6. 新增或修改自研功能时，更新对应当前保护项；没有对应条目时新增当前保护项。

## 3. 当前验收规则

1. 对每个保护项核对当前位置、数据/配置边界和验证入口。
2. 检查删除路径、迁移路径、配置读取、缓存失效、API 响应和前后端类型契约。
3. 运行与改动范围相称的 Go、前端类型检查和目标测试。
4. P-01 的 Actions、P-05 的响应式 smoke、P-39 的 Canvas 浏览器验收保留为用户或发布流程入口。
5. 本文档不保存提交哈希、历史映射或功能更新记录。

## 4. 当前自研功能保护项

### P-01 Fork 镜像发布与多架构产物

- 必须保留：GitHub Actions 为 `QLHazyCoder/new-api` 发布 GHCR 镜像；`main` 推送生成
  amd64、arm64 和多架构 manifest，并更新 `latest`。构建摘要和 manifest 失败信息必须
  保持可读，不能退回到上游作者的镜像命名或只发布单架构镜像。
- 当前位置：`.github/workflows/docker-build.yml`。
- 合并检查：确认 owner、小写 GHCR 路径、`main-<short-sha>` 标签、`latest`、两个架构
  和 manifest Job 均仍存在；以 GitHub Actions 为构建验收，不在本机做 Docker 构建替代。
- 验证入口：推送 `main` 后由 GitHub Actions 自动运行；需要追溯时使用
  `gh run view <head-sha> --repo QLHazyCoder/new-api` 确认 amd64、arm64 和 manifest Job。
  本轮只登记入口，不等待或重复执行 Actions。

### P-03 钱包默认自定义充值金额

- 必须保留：管理员可配置默认自定义充值金额；前端在配置缺失、非法或低于当前最小
  充值额时仍给出安全可用的默认值，不把金额置零或造成不可提交状态。
- 当前位置：`setting/operation_setting/payment_setting.go`、`controller/topup.go`、
  `web/src/features/wallet/lib/payment.ts`、`web/src/features/wallet/index.tsx`。
- 数据/配置：现有支付设置中的 `default_topup_amount` 必须兼容保留。
- 验证入口：`setting/operation_setting/payment_setting_test.go`、
  `web/src/features/wallet/lib/payment.test.ts`。

### P-04 支付回跳后的钱包刷新

- 必须保留：Stripe、Epay、Creem、Waffo 及订阅支付回跳后，钱包页通过统一返回标记
  刷新余额、订单或订阅状态；Safari 同页表单提交不能因导航竞态丢失该标记。
- 当前位置：`controller/return_path.go`、`service/return_path.go`、
  `web/src/features/wallet/lib/payment-return.ts`、`web/src/features/wallet/index.tsx`。
- 验证入口：`controller/return_path_test.go`、
  `web/src/features/wallet/lib/payment-return.test.ts`。

### P-05 钱包与订阅面板的稳定布局

- 必须保留：钱包移动端/窄宽布局正常流式展示，订阅面板高度受约束但不遮挡内容，
  不因上游样式替换导致支付卡片、订阅列表或钱包操作不可见。
- 当前位置：`web/src/features/wallet/**`。
- 合并检查：在改动钱包布局时保留订阅卡、充值表单、邀请奖励卡和支付返回状态的
  可达性；不要以旧 Classic 样式覆盖当前单前端实现。
- 验证入口：`web/src/features/wallet/__tests__/copy.test.tsx`、
  `web/src/features/wallet/components/dialogs/__tests__/billing-history-pricing-audit.test.tsx`；
  390px、768px 和桌面宽度的响应式可见性由用户自行人工/浏览器 smoke 验收，本轮不作为
  代理完成门禁。

### P-06 性能指标的分组可见性、聚合与失败归类

- 必须保留：性能指标的可用率颜色、成功率聚合和统计口径正确，普通用户只看到其可用
  的真实分组数据；虚拟 `auto` 分组对所有用户可见；管理员/根用户按权限查看真实分组。
- 必须保留：图片 Relay 收到上游原始 HTTP 400 时不写入新的性能请求/失败样本；原始状态
  必须在渠道状态码映射前记录。图片上游 5xx/429、本地或映射产生的 400，以及非图片请求
  仍按原规则计入；该规则只影响发布后的新样本，不回算或修改历史聚合数据。
- 当前位置：`pkg/perf_metrics/**`、`controller/perf_metrics.go`、
  `web/src/features/performance-metrics/**`、仪表盘性能组件。
- 数据/配置：性能统计依赖现有日志/统计数据，不得因合并清空或改写历史聚合数据。
- 验证入口：`controller/perf_metrics_test.go`、`pkg/perf_metrics/metrics_test.go`。

### P-07 Relay 协议兼容与缓存写入计费

- 必须保留：公开 Chat Completions 请求不会被自动改写为 Responses；Responses 的
  cache creation/cache write 用量会进入账务和日志；旧音频完成倍率配置继续有效。
- 必须保留：Claude 流式响应缺少上游 output usage 时，按已接收文本和 tool-use 数量进行
  本地估算，向客户端补齐 `output_tokens`，并在账务快照中标记为 estimated；上游已经给出
  的 usage 不被覆盖，部分字段只补缺失值。
- 当前位置：`relay/chat_completions_via_responses.go`、
  `service/openai_chat_responses_mode.go`、`service/openai_chat_responses_compat.go`、
  `relaykit/relayconvert/**`、`relay/channel/openai/relay_responses.go`、
  `relay/channel/claude/relay-claude.go`、
  `relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp.go`、
  `service/billing.go`、`service/log_info_generate.go`、相关 `setting` 配置。
- 合并检查：区分协议转换、上游请求显示用量和实际扣费用量，不能只因上游返回字段
  更少就丢弃 cache-write 计费；不要重新打开公共 Chat-to-Responses 自动转换。
- 验证入口：`relay/chat_completions_via_responses_test.go`、
  `relay/channel/openai/relay_responses_billing_test.go`、相关 relay/service 测试。

### P-08 分组描述与配置编辑的保留

- 必须保留：分组倍率/可用分组的可视化编辑器在切换、保存和重新加载后不丢失
  `GroupDescriptions`；分组特殊可见规则继续可配置。
- 当前位置：`service/group.go`、`setting/ratio_setting/group_ratio.go`、
  `web/src/features/system-settings/models/group-ratio-form.tsx`、
  `web/src/features/system-settings/models/group-ratio-visual-editor.tsx`。
- 数据/配置：`GroupRatio`、`GroupGroupRatio`、`UserUsableGroups`、
  `group_ratio_setting.group_descriptions`、
  `group_ratio_setting.group_special_usable_group` 都是已有生产配置，不能在迁移中归零。
- 验证入口：`web/src/features/system-settings/models/__tests__/group-settings.test.tsx`、
  `service/group_test.go`。

### P-11 分组可见性、定价接口脱敏与使用日志分组选择

- 必须保留：`UserUsableGroups` 定义公开分组；用户自身同名分组始终可用；
  `GroupSpecialUsableGroup` 的加减规则生效。任何分组名称，包括 `default`，都不具有
  硬编码的公开/私有语义。
- 必须保留：`/api/pricing` 只返回至少有一个可见分组的模型，并且每个返回模型的
  `enable_groups` 只包含该用户可见的分组，不能向普通用户泄露同一模型上的其他私有
  分组；过滤必须使用副本，不能污染共享的定价缓存。
- 必须保留：使用日志的分组筛选来自定价接口的可见分组；历史 URL 中仍存在的分组值
  仍可显示，不应因下拉框改造丢失筛选状态。
- 当前位置：`service/group.go`、`controller/pricing.go`、
  `controller/pricing_test.go`、`web/src/features/pricing/**`、
  `web/src/features/usage-logs/components/common-logs-filter-bar.tsx`。
- 验证入口：`controller/pricing_test.go`；未来合并应追加普通用户、同名私有分组和
  管理员三类接口断言。

### P-12 日志保留保护、错误日志权限与工具附加费标记

- 必须保留：日志异步清理不能删除保留期内的数据；`LogRetentionDays` 的上限校验同时
  在选项更新和实际系统任务中生效，不能只保留前端或管理接口校验。
- 必须保留：错误日志（type 5）在自助日志视图一律不可见——自己的日志列表、自助导出
  流和 Token 日志对所有角色（含管理员）都排除 type 5，管理员通过全局日志视图查看；
  敏感词审计（type 8）仍只对非管理员排除。这不是删除错误日志数据，而是访问层过滤，
  前端同时对非管理范围隐藏 Error 类型筛选项。
- 必须保留：工具调用附加费仍计入实际账务和日志数据，但当前用户界面不展示附加费
  标记。
- 当前位置：`controller/option.go`、`model/option.go`、`model/log.go`、
  `service/system_task.go`、`web/src/features/usage-logs/components/log-cost-display.tsx`。
- 验证入口：`controller/log_test.go`、`service/system_task_test.go`、
  `model/log_format_test.go`、
  `web/src/features/usage-logs/components/__tests__/cost-display.test.tsx`。

### P-13 Playground 多供应商图片能力与路由

- 必须保留：Playground 图片模式支持本项目已接入的多供应商能力；模型可见性、
  OpenAI/Gemini/xAI 的图片路由和模型名保持一致，不能因上游模型识别变化错误地把
  图片请求送到文本渠道或重新开放已明确移除的 Grok 图片路径。
- 必须保留：Playground 与画布图片模式共用全局 `aspect_ratio + resolution` 能力，模型是否
  属于图片工作台只由持久化模型元数据端点决定。旧的 `/data/image-capabilities.json`、
  `IMAGE_CAPABILITY_CONFIG_FILE` 和逐模型热加载规则属于历史方案，不得恢复为第二套事实源。
- 必须保留：`gpt-image-2`、`gpt-image-2.5`、`gpt-image-2.6` 及其 Flare/Sunburst 变体和
  `chatgpt-image-latest` 共享完整的
  GPT 图片分辨率能力，包括横竖 4K 规格；能力注册、前端模型选项和图片 relay 必须保持一致。
- 当前位置：`pkg/imagecapability/**`、`service/image_capability.go`、
  `controller/playground.go`、`controller/user.go`、`router/api-router.go`、
  `middleware/distributor.go`、`relay/channel/gemini/**`、`relay/channel/xai/**`、
  `web/src/features/playground/**`。
- 合并检查：模型能力注册、用户可见分组、渠道选择、实际 relay DTO 和前端模型筛选
  必须同时存在；只迁移其中一层会造成“看得到但不能生成”或“能路由但用户看不到”。
- 验证入口：`pkg/imagecapability/defaults_test.go`、`service/canvas_test.go`、
  `controller/playground_test.go`、Gemini/xAI 图片 relay 测试。
- 模型广场与插件协议的交叉验证见 `docs/model-catalog-endpoint-capabilities.md`：
  定价端点及图片参数必须使用启用渠道、映射与配置，不能从公开模型名或插件内部协议名猜测。

### P-14 Playground 图片请求构造、编辑与规格选项

- 必须保留：GPT 图片请求按所需规格拆分；参考图编辑、直接预览/灯箱、质量/尺寸
  选项和 `auto` 尺寸都能正确落到请求载荷。工作台自己的默认规格不能变成公共
  Images API 的强制默认值；`gpt-image-2` 及后续型号的客户端没有提供 `size`、
  `quality` 时应保持省略，旧模型兼容默认值仍沿用上游。
- 必须保留：原生图片参数沿用上游转发设计，不恢复 `gpt-image-2*` 的本地尺寸或
  输出格式白名单。工作台的画幅/分辨率转换只在对应渠道适配层执行，显式 `size`
  优先，并覆盖生图、JSON 编辑与 multipart 编辑。
- 必须保留：GPT-Image-2.6 Flare/Sunburst 的 `complexity`、`markup`、`svg`、
  `frame_count` 和 `frame_duration_ms` 等上游字段不能在模型扩展时被过滤或改写；
  图片模型的编辑能力由已验证的模型能力 profile 决定，不得硬编码为单一模型名。
- 当前位置：`relaykit/dto/openai_image.go`、`dto/image_capability.go`、
  `web/src/features/playground/components/playground-image-input.tsx`、
  `web/src/features/playground/hooks/use-playground-image-options.ts`、
  `web/src/features/playground/lib/image-payload-builder.ts`。
- 合并检查：不要为了特殊 `auto` 值提前返回而跳过其他输入校验；参考图、尺寸、质量、
  数量和供应商特有参数都要保留。
- 验证入口：`web/src/features/playground/lib/image-payload-builder.test.ts`、
  `web/src/features/playground/lib/image-generation-capabilities.test.ts`、
  `relay/helper/valid_image_request_test.go`。

### P-15 Playground 异步任务、持久队列与全局并发

- 必须保留：图片生成是可持久化的异步任务；任务状态、租约、重试、中断恢复和工作器
  处理跨实例有效，不能退回浏览器本地历史或内存队列。
- 必须保留：`PlaygroundImageMaxConcurrency` 是数据库中必须存在的 option。启动迁移只
  在缺失时写入默认 `0`，绝不覆盖旧值；`0` 表示不限并发。队列为空时不查询该 option，
  有待处理任务但 option 缺失/非法时必须明确失败，不能持续 `record not found` 高频查询
  或悄悄使用旧进程缓存回退。
- 当前位置：`model/playground_image.go`、`model/required_option.go`、`model/main.go`、
  `service/playground_image_worker.go`、`controller/playground_image_task.go`。
- 数据/迁移：保留 `playground_image_tasks`、相关批次/文件记录和 `options` 中既有并发值；
  标准/快速迁移均必须 seed 并校验 required option。
- 验证入口：`model/playground_image_test.go`、`model/required_option_test.go`、
  `model/main_migration_test.go`、`service/playground_image_worker_test.go`。

### P-16 Playground 图片历史、删除与任务卡交互

- 必须保留：图片结果按用户保留上限 200 条；终态结果默认保留 24 小时，超额或到期时
  同时清理文件和数据库行。前端图片模式显示可关闭的保留提示；删除是硬删除，不允许旧
  任务历史在页面刷新后复活。
- 必须保留：前端快速重复点击同一删除按钮时先同步锁定任务、禁用重复动作并保留成功
  tombstone；后端 DELETE 幂等，重复删除返回成功而不是 `image task not found`。这不影响
  不同任务的独立删除。
- 必须保留：任务卡的重试、刷新、下载、完整预览、生成中占位、控件可用性和布局保持
  可操作，不因上游 UI 改动丢失。
- 必须保留：成功图片的保留清理在每次事务中最多读取固定批次（当前为 500 条），并在
  保留上限之后使用显式 SQL `LIMIT`；不能恢复无界的 `OFFSET` 查询，避免用户图片历史
  增长后造成大查询或锁事务失控。
- 当前位置：`model/playground_image.go`、`controller/playground_image_task.go`、
  `service/playground_image_worker.go`、
  `web/src/features/playground/components/playground-image-task-grid.tsx`、
  `web/src/features/playground/hooks/use-image-generation-handler.ts`、
  `web/src/features/playground/index.tsx`。
- 验证入口：`model/playground_image_test.go`、`controller/playground_image_task_test.go`、
  前端 Playground 图片库测试。

### P-17 邀请奖励账本与额度换算

- 必须保留：注册奖励、充值返利和邀请余额划转都在所属业务事务中完成；账本为追加式
  `affiliate_reward_events`，使用幂等键，不能由账户累计字段反推或补造历史事件。
- 必须保留：邀请额度与普通额度转换使用正确的 quota 单位；邀请人数从
  `users.inviter_id` 为事实来源，硬删除会原子维护 `aff_count`，周期核对可以收敛冗余
  计数但不能改写历史奖励。
- 必须保留：Epay 充值奖励使用订单创建时保存的 `credited_quota`、奖励基点和资格快照，
  回调不能按当前 QPU 或当前奖励比例重新计算；奖励、普通额度和追加式账本必须在同一
  事务中完成，`affiliate_reward_events.reward_rate_bps` 记录实际采用的比例。
- 必须保留：钱包及邀请额度的原始值通过 `*_raw` 十进制字符串传输；转移接口接受数字
  或字符串形式的 `int64`，前端不得将原始额度转换为 JavaScript `Number` 后再提交。
- 当前位置：`model/affiliate_reward.go`、`model/topup.go`、`model/user.go`、
  `controller/topup.go`、`web/src/features/wallet/components/affiliate-rewards-card.tsx`。
- 数据/迁移：`affiliate_reward_events` 是生产账本表，禁止删除、重建或用非事务 SQL
  批量重算；详细语义见 [affiliate-reward-ledger.md](./affiliate-reward-ledger.md)。
- 验证入口：`model/affiliate_reward_test.go`、`model/payment_method_guard_test.go`。

### P-18 缺省开启请求与错误日志 IP 记录

- 必须保留：用户设置中没有 `record_ip_log` 键时，普通请求和错误日志默认记录客户端 IP；
  用户显式设置 `false` 时停止记录。缺省值与显式关闭必须可区分，不能改成非指针布尔值
  的非指针布尔值语义。
- 当前位置：`relaykit/dto/user_settings.go`、`controller/user.go`、`model/log.go`、
  `web/src/features/profile/lib/user-settings.ts`、`web/src/features/profile/api.ts`、
  `web/src/features/security/components/privacy-card.tsx`。
- 设置写入必须保留字段 presence：请求省略 `record_ip_log` 时保留数据库已有值，数据库缺省值继续
  表示开启；只有显式 `false` 才关闭。通知设置保存不得从前端默认值重新注入该字段，避免覆盖安全设置。
- 合并检查：分别验证设置缺失、显式 `true` 和显式 `false`；错误日志与消费日志使用一致规则；
  前端缺省显示为开启，通知设置独立保存不改变 IP 记录设置。
- 验证入口：`controller/user_manage_test.go`、`model/log_other_test.go`、
  `web/src/features/profile/__tests__/settings.test.tsx`、
  `web/src/features/security/components/__tests__/privacy-card.test.tsx`。

### P-19 订阅适用分组与管理界面

- 必须保留：订阅计划和用户订阅都支持 `ApplicableGroup`；空值表示全分组，非空值仅在
  使用分组匹配时扣订阅额度。管理员创建/更新计划会校验分组，用户界面显示完整计划名
  并提供“适用分组”选择及翻译。
- 当前位置：`model/subscription.go`、`controller/subscription.go`、
  `web/src/features/subscriptions/components/subscriptions-mutate-drawer.tsx`、
  `web/src/features/subscriptions/**`。
- 数据/迁移：保留订阅计划和用户订阅中的 `applicable_group` 字段，不能在上游迁移中
  置空或丢列。
- 验证入口：`model/subscription_applicable_group_test.go`、订阅 controller/前端测试。

### P-20 订阅优先的混合扣费

- 必须保留：`subscription_first` 在订阅余额不足时可以按规则使用钱包补足；严格计划
  的钱包回退语义正确。混合扣费必须保存订阅/钱包分摊，结算负差额先退钱包，正差额
  继续按正确来源追扣，任务退款同样按分摊回滚。
- 必须保留：普通 relay 请求开始时预扣成功、最终实际费用更高时，`subscription_first`
  必须在结算锁内先扣完订阅剩余，再把 overflow 记到钱包并允许形成欠费；`subscription_only`
  必须先扣完订阅剩余，再把 overflow 写成独立的 `unbilled` 审核记录。结算策略随
  `settlement_request` 持久化，恢复任务不得按当前余额重新猜测；历史旧意图不得在部署
  后被自动改成新策略。
- 当前位置：`service/funding_source.go`、`service/billing_session.go`、
  `service/task_billing.go`、`model/subscription.go`、钱包订阅偏好界面。
- 数据/迁移：既有任务 `private_data` 中的 billing allocations 是账务事实，不能把
  mixed 任务当成单一订阅或单一钱包任务处理。
- 验证入口：`service/task_billing_test.go`、
  `service/billing_session_settlement_test.go`、`model/billing_operation_database_matrix_test.go`。

### P-21 用户管理、缓存一致性与硬删除边界

- 必须保留：用户管理支持按 ID 搜索、显示最后使用时间、识别覆盖分组；管理员配额
  覆盖会同时更新数据库、缓存和认证版本，不留下旧额度或旧会话可见状态。
- 必须保留：管理操作不作用于软删除用户；专用永久删除路径仍可读取目标并完整清理
  认证相关数据、缓存、邀请计数及相关业务数据。不能把软删除与永久删除重新混为一谈。
- 必须保留：`PUT /api/user/self` 的资料和密码修改只能更新自助字段白名单；
  `quota`、`used_quota`、`request_count` 等账务字段不得从请求或旧快照写回，侧边栏与
  语言偏好也只能更新 `setting` 列。
- 当前位置：`controller/user.go`、`model/user.go`、`model/user_auth_cache.go`、
  `web/src/features/users/**`。
- 验证入口：`controller/user_manage_test.go`、`controller/user_self_update_test.go`、
  `model/user_update_test.go`、`model/user_authentication_test.go`、
  `model/user_cache_auth_version_test.go`。

### P-22 注册来源分组策略

- 必须保留：新建用户可按密码、微信、GitHub/其他 OAuth 来源应用
  `RegistrationGroupPolicy`；策略从数据库实时读取，非法/缺失配置安全回退默认分组。
  已存在用户、OAuth 绑定/登录和管理员直接创建用户不得被重新分组。
- 当前位置：`model/registration_group_policy.go`、`controller/user.go`、
  `controller/oauth.go`、`controller/wechat.go`。
- 数据/配置：`options.RegistrationGroupPolicy` 必须保留；详细 JSON 契约见
  [registration-group-policy.md](./registration-group-policy.md)。
- 验证入口：`model/registration_group_policy_test.go`、
  `controller/registration_group_policy_test.go`。

### P-23 本地化同步的英文基准与翻译键保留

- 必须保留：`sync-i18n` 固定以英文作为稳定源语言，不根据哪个 locale 当前键数最多而切换；
  同步前合并所有 locale 已有键，缺少英文源值时以 key 本身补齐，并将旧根命名空间键迁入
  `translation` 而不覆盖现有值，避免同步丢弃本地翻译或键。
- 当前位置：`web/scripts/sync-i18n.mjs`。
- 验证入口：`cd web && bun run i18n:sync`；检查英文基准键、七种 locale JSON 可解析且
  静态键集合无回退删除。
- 合并检查：对比同步前后的各 locale 键和值；标准 BCP-47 语言码和 `Intl` 归一化属于
  上游已有的标准行为，不是本项的保护内容。保护对象是本地同步规则与自研功能翻译，
  不要求恢复上游随退休界面一并删除、且没有自研调用的翻译键。

### P-24 排行榜的本地自然日与管理员访问边界

- 必须保留：排行榜按应用时区的本地自然日计算 today/yesterday/week/month/year，查询和
  分桶使用半开区间，缓存跨本地零点失效；“昨天”是完整上一个本地自然日。
- 必须保留：排行榜界面入口只给管理员/根用户，未登录跳登录，普通用户跳 403；这不
  擅自改变后端模块开关和接口认证语义。
- 当前位置：`service/rankings.go`、`model/usedata_rankings.go`、
  `web/src/features/rankings/**`、`web/src/routes/rankings/index.tsx`。
- 数据/配置：数据库实现必须同时兼容 SQLite、MySQL、PostgreSQL；详细时间契约见
  [rankings-time-boundaries.md](./rankings-time-boundaries.md)。
- 验证入口：`service/rankings_test.go`、`model/usedata_rankings_test.go`、
  `web/src/features/rankings/access.test.ts`。

### P-29 按用户分组的支付金额折扣与充值定价快照

- 必须保留：精确充值金额折扣仍使用正整数金额到 `0 < rate <= 1` 的映射；只有管理员
  选择的可用分组才能获得折扣。可用分组来自未软删除用户（启用和禁用用户都计入），
  新增选择必须在保存时有用户，历史已选分组即使后来无人仍可保留并移除；空分组数组
  表示不向任何分组发放折扣，并且 API/运行时序列化为 `[]` 而不是 `null`。
- 必须保留：定价时服务端读取数据库中的当前用户分组，客户端不能提交用于计价的分组；
  Epay 使用金额/分组倍率/折扣公式和版本化充值定价快照，Epay 待支付订单使用创建时
  保存的金额和快照，不因策略变更重新计价。Waffo、Waffo Pancake、Stripe、Creem、
  订阅和兑换码不纳入本次 Epay 固定结算扩展，继续使用各自现有流程。
- 当前位置：`setting/operation_setting/payment_setting.go`、`model/payment_group.go`、
  `model/topup_pricing_snapshot.go`、`controller/payment_discount_policy.go`、
  `controller/topup_pricing.go`、`controller/topup*.go`、
  `web/src/features/system-settings/integrations/amount-discount-visual-editor.tsx`、
  `web/src/features/wallet/components/dialogs/billing-history-dialog.tsx`。
- 数据/配置：保留 `payment_setting.amount_discount`、
  `payment_setting.amount_discount_eligible_groups`、既有 `top_up` 订单金额/状态和
  定价快照字段；Epay 新增 `credited_quota`、`quoted_money_minor`、奖励基点和结算版本，
  只做扩展式迁移，不回写或重算历史订单，不直接修改生产 MySQL 数据。
- 验证入口：`setting/operation_setting/payment_setting_test.go`、
  `model/payment_group_test.go`、`model/topup_pricing_snapshot_test.go`、
  `controller/payment_discount_policy_test.go`、`controller/topup_pricing_test.go`、
  `web/src/features/system-settings/integrations/__tests__/amount-discount-visual-editor.test.tsx`、
  `web/src/features/wallet/components/dialogs/__tests__/billing-history-pricing-audit.test.tsx`。
- 设计与阶段记录：[payment-amount-discount-group-policy.md](./payment-amount-discount-group-policy.md)。

### P-30 敏感词规则、内容审计与用户违规控制

- 必须保留：策略支持全局规则和绑定定价分组的局部规则，规则以统一表格管理；局部
  分组只能来自 `ratio_setting.GetGroupRatioCopy()`，不得接受 `auto` 或不存在的分组。
  规则词条支持批量文本/TXT 导入、去重、编辑、`block/observe/off` 模式和确认删除，
  运行时快照更新无需重启；动作由规则自身决定，不恢复全局处理模式。
- 必须保留：Relay 在 token 估算、预扣费、计费、渠道选择、上游调用和重试之前检查
  规范化提示词；自动分组检查全部候选分组。同一请求无论命中多少词只计一次，命中
  返回不可重试的 HTTP 422 `sensitive_words_detected`；已封禁账号后续请求返回 HTTP
  403 `user_banned`，并使用协议对应的错误封装。
- 必须保留：使用日志类型 8 `敏感词审计` 通过 request ID 关联主库审计事件；普通用户
  `/api/log/self` 和 API Key 的 `/api/log/token` 在数据库查询及计数阶段完全排除该类型，
  管理员/超级管理员的全局和本人视图才可查看。管理员列表明确区分拦截、白名单放行与
  观察；审计详情要求 `AuditRead` 才能查看完整规范化提示词、命中规则、片段和规则版本。
  新日志只写 `other.admin_info.keyword_filter`，不写公开结果；界面不解读历史顶层结构。
  审计证据不写入原始请求体、API Key 或普通日志字段。
- 必须保留：用户字段保存敏感词违规次数和白名单开关。白名单命中仍记录审计和日志但
  不拦截、不计数；观察模式只记录。普通用户按行锁事务原子递增，达到可配置阈值（新环境
  默认 50）时禁用
  账户、刷新认证版本并撤销会话；封禁、清零次数、解封和白名单操作均不得清空或修改
  `quota`、余额、历史账务或历史审计证据。
- 必须保留：常规用户资料/权限编辑的前端 payload 不含违规次数和白名单字段，后端只在请求
  显式携带时更新；对应控件变更时必须能提交 `0` 和 `false`。不要将表单默认值或陈旧快照
  当作显式变更写回，以免覆盖并发产生的命中或白名单更新。
- 必须保留：默认客户端警示文案明确说明“余额不退”和严重情形报警，但这是提示文本，
  不是余额处理指令；管理员可在用户编辑左抽屉维护违规次数、清零次数和个人白名单，
  敏感词页面不承载白名单名单或审计列表，审计复核入口统一在使用日志。
- 必须保留：规则编辑弹窗在 TXT 导入按钮左侧提供实时查找。查找只针对当前未保存的
  `draft.wordsText` 做不区分大小写的普通包含匹配，首个命中自动选中并滚动，`Enter`/
  `Shift+Enter` 循环定位；搜索不支持正则、不重建或过滤文本，草稿编辑时不得抢占
  文本框光标，关闭弹窗必须清空搜索状态，保存请求仍只提交原始解析后的词条。
- 必须保留：管理员从用户列表启用账户或调用专用解封接口时，必须在同一行锁事务内恢复
  `status` 并将当前违规次数清零；历史审计、使用日志、`quota`、`used_quota` 和白名单状态
  不得改变。禁用状态转启用才递增 `auth_version`、刷新认证缓存并撤销旧会话；重复启用
  不得重复执行这些认证动作。操作审计使用 `sensitive_word.enable_reset`，并记录入口来源
  与 `balance_changed:false`。
- 必须保留：审计完整提示词在 MySQL 使用 `MEDIUMTEXT`、在 PostgreSQL/SQLite 使用 `TEXT`，
  写入前执行合法 UTF-8 的字符/字节双重截断。`observe` 模式仅对明确的审计落库失败放行并记录
  降级；规则、用户或其他事务错误以及 `block` 模式仍失败关闭返回 503，不能借故绕过策略。
- 当前位置：`model/sensitive_word_{types,rules,runtime,audit,migration}.go`、
  `model/user.go`、`model/main.go`、`model/log.go`、`model/log_other.go`、
  `relay/request_billing.go`、`controller/relay.go`、`controller/sensitive_word.go`、
  `controller/user.go`、`middleware/auth.go`、`middleware/sensitive_word.go`、
  `router/api-router.go`、`web/src/features/system-settings/request-policies/sensitive-words/`、
  `web/src/features/users/lib/user-form.ts`、
  `web/src/features/users/components/users-columns.tsx`、
  `web/src/features/users/components/users-mutate-drawer.tsx`、
  `web/src/features/usage-logs/**`。
- 数据/配置：保留旧 `SensitiveWords` Option 的一次性迁移，不保留运行时兼容回退；新增规则、词条、分组、
  审计表及用户字段必须同时兼容标准/快速迁移。审计事件写主库，日志库只保存结构化
  摘要；配置关闭证据留存时不得写入完整提示词或片段。
- 验证入口：`model/sensitive_word_test.go`、`controller/sensitive_word_test.go`、
  `controller/relay_test.go`、`controller/user_manage_test.go`、
  `web/src/features/users/lib/__tests__/user-form.test.ts`、
  `web/src/features/users/components/__tests__/permissions.test.tsx`、
  `relaykit/**`，以及敏感词页面、用户抽屉和使用日志的前端 typecheck/build/lint。
- 长提示词/观察模式故障修复涉及 `model/sensitive_word.go`、`controller/relay.go` 及其
  模型/控制器回归测试；完整提示词字段、UTF-8 截断和审计失败类型必须继续保持。

### P-31 CC Switch 多来源导入

- 必须保留：密钥页面的 CC Switch 导入支持由集中来源注册表驱动；当前支持 Claude、Codex、
  Claude Desktop、Gemini、Antigravity CLI、Grok Build、OpenCode、OpenClaw、Hermes、Pi、
  MiniMax Code、Kimi Code、DeepSeek Harness、Oh My Pi、GitHub Copilot CLI 和 WorkBuddy
  共十六个来源；Claude Desktop 使用 Anthropic 格式，不再显示旧的“不支持”分支。
- 必须保留：来源专属 endpoint、模型字段和默认名称保持独立；服务地址规范化会去除尾部
  `/v1` 和斜杠，深链参数必须进行 URL 编码，缺少必填模型时不能生成导入链接。
- 当前位置：`web/src/features/keys/lib/cc-switch-sources.ts`、
  `web/src/features/keys/components/dialogs/cc-switch-dialog.tsx`、相关 locale 和测试。
- 数据/配置：无数据库迁移；新增来源必须同步更新注册表、界面选择、国际化静态键和来源
  构造测试，不能把来源配置重新散落回弹窗组件。
- 验证入口：`web/src/features/keys/lib/__tests__/cc-switch-sources.test.ts`、
  `web/src/features/keys/components/__tests__/cc-switch-dialog.test.tsx`。

### P-32 钱包额度 int64 与计费额度边界分离

- 必须保留：用户钱包、Token 剩余额度/已用额度、充值、兑换码、签到、邀请奖励、返利、
  审计累计字段使用有符号 `int64`/`BIGINT`；`MaxWalletQuota` 和 `MinWalletQuota` 只是
  防止技术溢出的边界，不重新设置 int32 产品余额上限。现有额度单位和 `QuotaPerUnit` 不变。
- 必须保留：单次请求费用、任务费用和历史消费日志仍在 int32 charge domain 中，异常价格
  或请求参数经过饱和/严格错误处理，不能为了放开钱包而取消单次计费溢出保护。
- 必须保留：数据库是唯一可授信的额度来源；钱包预扣使用 `quota >= amount` 的条件
  SQL，有限 Token 同时检查剩余额度与双字段整数边界，无限 Token 仍可欠费但不可溢出。
  充值、兑换、签到及管理员调整与业务记录原子提交。Redis 只缓存身份和策略元数据，
  不缓存可支出的额度；数据库故障不得用旧缓存余额授信。
- 必须保留：`BATCH_UPDATE_ENABLED=true` 仅输出弃用与 SQL 压力告警，不再排队钱包、
  Token 或累计用量；不得恢复 Redis 额度先写、进程队列延迟刷库的协议。
- 必须保留：计费会话的初始预扣、图片追加预扣、差额结算与明确失败退款使用持久操作
  状态和事务，Token、钱包、订阅同时提交；退款先持久标记、可重试且不重复加账。任务
  提交结果先与 settlement intent 同事务持久化，资金、Token、分摊、用量和任务最终状态
  再同事务提交；进程退出可重试已确认结算。Midjourney 在请求上游前原子预留钱包与
  Token，收到响应后先持久记录上游任务 ID/响应码，再把任务与 settlement/refund 状态
  同事务关联；明确拒绝可重试释放预留。传输错误、结果未知或任务事务失败时保留预留，
  用持久上游 ID 供人工核查，禁止按超时自动退款/补扣。无 BillingSession 的实时流兼容
  预扣/结算也必须共享 `initial` 根记录与幂等阶段；中途退出的预扣进入同一待核查清单。
- 必须保留：旧数据库值不缩放、不重算；标准/快速迁移后校验所有钱包字段为有符号 BIGINT，
  并记录超出旧 int32 范围但被保留的历史值。API 写入兼容 JSON number/string，查询提供
  `quota_raw`、`used_quota_raw`、`remain_quota_raw` 等精确字符串字段，前端使用 BigInt。
- 必须保留：`GET /api/usage/token/` 使用认证中间件身份，现有额度字段仍为 JSON number，
  `total_granted_raw`、`total_used_raw`、`total_available_raw` 提供精确十进制字符串；
  总额允许超过 `int64`，以大整数计算。旧 billing 展示接口在查询失败时返回原有错误结构。
- 必须保留：成功的视频任务查询响应可从任务的持久账务快照投影 `usage.total_tokens`；
  没有 token usage、任务失败或数值超出 charge domain 时省略该字段，不伪造 usage。插件
  adaptor 必须原样保留该响应字段。
- 必须保留：用户余额提醒的阈值运算继续处于钱包 `int64` 域，不能通过 `int` 窄化或
  未检查的浮点转整数溢出；极大阈值必须按钱包上限安全限制。
- 必须保留：MySQL 计费事务每次尝试使用 `BILLING_LOCK_WAIT_TIMEOUT_SECONDS` 限制锁等待，
  默认 10 秒，设为 `0` 时使用数据库服务端默认值；事务结束前恢复连接 session 原值，
  非 MySQL 方言保持原有事务路径和重试语义。
- 当前位置：`common/wallet_quota.go`、`common/quota_math.go`、`model/wallet_quota.go`、
  `model/wallet_quota_schema.go`、`model/quota_reserve.go`、`model/user.go`、
  `model/token.go`、`model/billing_operation.go`、`model/task_billing_operation.go`、
  `model/midjourney.go`、`model/midjourney_billing_operation.go`、`model/subscription.go`、
  `service/billing_session.go`、`service/task_billing.go`、`service/task_polling.go`、
  `service/billing_recovery.go`、`service/midjourney.go`、`relay/mjproxy_handler.go`、
  `controller/token.go`、`controller/billing.go`、
  `controller/misc.go`、`controller/user.go`、`service/quota.go`、
  `relaykit/dto/user_settings.go`、`relaykit/dto/openai_video.go`、
  `relay/channel/task/jsplugin/adaptor.go`、`model/billing_transaction.go`、`.env.example`、
  `web/src/lib/format.ts`。
- 数据/迁移：钱包字段迁移必须保留默认值、索引、空值行为和历史 raw quota；写入大额度后不能
  回滚到仍带有旧钱包 int32 校验的版本。`billing_operations` 与 `midjourneys` 只追加账务状态、
  请求关联及上游结果列；操作唯一键为 `(request_id, phase)`，既有行不回填、不改账；发布必须
  停止旧实例处理在途账务后再切流，
  历史不可判定差额只生成审计报告，不自动改账。
- 验证入口：`common/wallet_quota_test.go`、`model/wallet_quota_test.go`、
  `model/quota_reserve_test.go`、`model/billing_operation_database_matrix_test.go`、
  `controller/token_quota_int64_test.go`、`service/task_billing_test.go`、
  `controller/midjourney_billing_test.go`、`controller/user_manage_test.go`、
  `controller/topup_quota_limit_test.go`、`model/task_openai_video_test.go`、
  `relay/channel/task/jsplugin/adaptor_test.go`、相关前端额度格式化测试。

### P-34 CC Switch 所选密钥授权模型与可访问下拉框

- 必须保留：CC Switch 的模型列表必须由当前所选 API Key 授权的 `/v1/models` 决定，
  不得改用登录用户的全局 `/api/user/models`。请求显式携带所选 Key 的 Bearer token，
  并使用 `skipSessionAuthorization` 防止共享客户端把 session JWT 覆盖到该请求上。
- 必须保留：切换 Key、刷新中或请求失败时隐藏旧模型、禁用模型选择及导入；不得将已
  缓存但不再授权的模型用于深链导入。
- 必须保留：模型 Combobox 采用可脱离 Dialog 裁剪的 Portal 结构，支持过滤、键盘选择、
  Escape 关闭及异步模型到达；名称输入不应意外打开模型下拉框。上游具备等价或更完整
  UI 行为时优先采用上游组件布局和测试，保留本项的模型数据提供者与来源注册表边界。
- 当前位置：`web/src/features/keys/api.ts`、
  `web/src/features/keys/lib/cc-switch-sources.ts`、
  `web/src/features/keys/components/dialogs/cc-switch-dialog.tsx`、
  `web/src/features/keys/components/dialogs/__tests__/cc-switch-dialog.test.tsx`。
- 验证入口：所选 Key 的 `/v1/models` 授权、加载/失败隐藏、Portal、键盘、异步更新及
  七类来源的深链构造测试。

### P-35 使用日志模型诊断的管理员可见性与历史迁移

- 必须保留：`is_model_mapped`、`upstream_model_name`、`response_model` 等诊断字段
  存入 `other.admin_info`。普通用户的 `/api/log/self`、`/api/log/token` 和任务 DTO
  不得暴露这些字段；管理员/根用户可在使用日志详情中查看。不能只隐藏前端徽标而仍从
  普通用户 API 返回原始模型名。
- 必须保留：`cmd/migrate-log-model-visibility` 提供显式 dry-run/apply，将历史
  `logs.other` 顶层诊断字段迁入 `admin_info`，保留无关字段并按批次事务处理；服务启动时
  不得自动改写日志库，也不得改写任务表 `properties`。apply 前先发布新读写逻辑、备份并
  核对候选数；旧写入器仍在线时不得迁移。
- 当前位置：`model/log.go`、`model/log_other.go`、`model/log_model_visibility_migration.go`、
  `controller/log.go`、`controller/task.go`、`service/log_info_generate.go`、
  `service/task_billing.go`、`cmd/migrate-log-model-visibility/main.go`、
  `web/src/features/usage-logs/**`。
- 数据/迁移：仅重写日志 JSON，不删日志、不改账务；`admin_info` 同名新值优先，非法 JSON
  或非法对象结构必须报错。精确回滚依赖迁移前备份，不能声称可逐字节无损反向迁移。
- 验证入口：`controller/log_test.go`、`controller/task_generic_test.go`、
  `model/log_format_test.go`、`model/log_other_test.go` 和普通用户/管理员接口响应断言。
- 运维步骤：[log-model-visibility.md](./maintenance/log-model-visibility.md)。本清单只证明
  代码和迁移工具存在，不代表生产日志已执行迁移。

### P-36 普通用户任务日志隐藏插件标识

- 必须保留：普通角色请求 `/api/task/self` 时，具有 `TaskPluginSnapshot` 的插件任务
  不返回 `platform`。该任务的 `platform` 即插件 key；不得通过 `platform`、
  `admin_info` 或 `root_info` 的插件诊断投影向普通用户暴露插件 key、名称、版本或作者。
  仅在前端隐藏字段不满足要求，API 响应本身必须省略标识。
- 必须保留：管理员和 Root 的平台及插件诊断可见性不变；没有插件执行快照的历史任务和
  原生任务继续返回平台。前端遇到缺失的 `platform` 时，任务 ID 副标题只显示 action，
  详情不显示 Platform 行，不渲染空分隔符。
- 必须保留：成功的 Suno 插件任务在隐藏平台后通过 `legacy_audio_available` 选择旧音频
  预览；该布尔标记不得包含插件身份。此行为只改变任务日志响应投影，不改任务存储、
  筛选条件或原生 `/v1` 任务响应，也不需要数据库迁移。
- 当前位置：`controller/task.go`、`dto/task.go`、
  `web/src/features/usage-logs/types.ts`、任务日志列与详情组件、`web/src/features/usage-logs/lib/task-artifacts.ts`。
- 合并检查：验证普通角色的 `/api/task/self` JSON 不含插件任务 `platform` 和插件元数据；
  管理员/Root 仍可查看，普通原生任务仍显示平台，缺失平台时 UI 不显示平台标识或多余
  分隔符，Suno 音频预览继续可用。若上游提供等价或更强实现，可采用上游实现，但上述
  角色隔离和回归场景必须继续成立。
- 验证入口：`controller/task_log_view_test.go`、
  `controller/task_generic_test.go`、
  `web/src/features/usage-logs/components/__tests__/task-logs-columns.test.tsx`、
  `web/src/features/usage-logs/components/__tests__/task-details-dialog.test.tsx`、
  `web/src/features/usage-logs/__tests__/artifacts.test.ts`。

### P-37 文档管理的数据边界与公开阅读路由

- 必须保留：New API 在 `/docs` 与 `/docs/{slug}` 提供匿名公开阅读，并复用 `PublicLayout`
  和站点顶部导航。目录搜索、折叠、移动端抽屉、排序后首篇选择、文章深链和未发布文章
  404 必须保持可用；同源 `docs_link` 在当前标签页内导航，外部链接仍按外链处理。
- 必须保留：公开文档页与其他页面使用同一套站点标签标题，不在文章组件中单独覆写
  `document.title`；切换文章或离开 `/docs` 时不得残留文章名称。
- 必须保留：公开阅读以 PC 为主，外层铺满视口，使用 380px 文档目录、自适应剩余宽度的
  正文和贴右的 380px H2/H3 文章目录；不得恢复外层居中宽度上限或正文 960px 上限。
  长目录名称可换行。文章目录点击在当前标签更新 hash 并定位章节，中文和重复标题锚点不
  冲突；深链在图片/字体加载后校正定位，用户主动交互后停止校正。滚动跟随当前章节及文末
  短章节；小于 1280px 隐藏章节目录，不提供手机章节抽屉。原有窄屏文档选择入口保留，
  无章节时正文使用全部剩余宽度，不留空目录栏。
- 必须保留：普通 fenced code 右上角可复制原始代码，保留缩进和空行，不包含 Markdown
  围栏、操作文案或渲染器添加的换行；行内代码、公式、流程和时序图不增加复制按钮。共享
  Markdown 默认调用行为不变。标准/裸 URL 可点击并保留目标参数，危险链接协议不可执行。
- 必须保留：浏览器阅读仅请求同域 `/api/public/docs` 与 `/api/public/docs/{slug}`，不出现
  `/tools/` 页面资源或 API 请求；公开响应只包含已发布快照，不返回草稿。HTTPS 图片、独立
  图片 URL 的扩展名限制以及作者 HTML 禁用继续生效。
- 必须保留：New API 只负责公开阅读。Tools `/tools/documents` 仍负责目录、Markdown 草稿、
  发布/下架和永久删除；`docs.db` 与公开 API 是内容来源，不增加第二份数据库、图片存储或
  后端文档实现。Caddy 仅在 `coderapi.vip` 将 `/docs*` 转发到 `new-api-proxy`，将
  `/api/public/docs*` 保留原 URI 转发到 `newapi-tools`；现有 `/tools` 管理路由不变。
- 当前位置：`web/src/routes/docs/**`、`web/src/features/docs/**`、
  `web/src/lib/navigation-target.ts`、`web/src/hooks/use-top-nav-links.ts`、
  `web/src/components/layout/components/public-header.tsx`、
  `web/src/features/home/components/sections/hero.tsx`；Tools 管理与数据实现位于
  `/opt/qlh-main/new_api_tools/frontend/src/features/docs/` 和 `backend/internal/service/docs/`，
  公网转发配置位于 `/opt/qlh-main/caddy/Caddyfile`。
- 交互职责：`web/src/features/docs/article-toc.tsx`、`use-article-navigation.ts` 和
  `web/src/components/ui/markdown.tsx`；文件映射与阶段验收见
  `docs/documentation-reader-enhancements.md`。
- 合并检查：验证 `/docs` 与深链匿名加载、顶部导航保留、同源链接不新开标签、同域 API 路径
  不含 `/tools/`、仅发布内容可见、撤下文章返回 404，以及标准/独立行/自链接图片规则和原始
  HTML 安全边界。不要把公开阅读重新挂回 Tools 登录壳，也不要改变 Tools 草稿/发布契约。
- 合并检查：验证 1920/2560/3828px 下左侧目录贴左、正文填满两栏间区域、章节目录贴右，
  不能只检查不溢出；同时验证长标题换行、H2/H3 目录定位及高亮、窄屏隐藏章节导航、
  带片段地址刷新、切换文章清理章节状态、代码复制成功/失败和状态恢复，以及共享 Markdown
  的公式/图表渲染回归。语言变更不得破坏被观察的标题节点，主动滚动后不得被图片加载拉回
  原章节。不得将普通代码中的 URL 转为链接。
- 验证入口：`web/src/features/docs/__tests__/reading.test.tsx`、`api.test.ts`、
  `markdown.test.tsx`、`tree.test.ts`、`web/src/lib/__tests__/navigation-target.test.ts`、
  `web/src/components/ui/__tests__/markdown.test.tsx`。

### P-38 Responses 流终态 usage、断流 drain 与临时估算计费

- 必须保留：共享 `StreamScannerHandler` 检测到下游 client gone 后，停止向已断开的
  客户端写入，但继续读取上游、解析协议事件、累计 usage、工具/图片用量并在同一请求
  结算流程中完成 cleanup。所有共享 scanner 流式调用方适用，不按渠道类型、客户端类型
  或端点设置白名单，也不设置进程内 drain 并发上限。
- 必须保留：`CLIENT_GONE_DRAIN_TIMEOUT` 是单请求 drain 的绝对超时，默认 60 秒，值为
  `0` 表示立即关闭上游；既有 `STREAMING_TIMEOUT` 仍表示流式空闲超时，`RELAY_TIMEOUT`
  若更早触发仍可终止 HTTP 客户端。EOF、`[DONE]`、上游读取错误和超时必须保持明确的
  终态优先级，不能遗留 scanner、data handler 或 ping goroutine。
- 必须保留：`stream_status` 中的 `end_reason`、`response_status` 和 `drain_result`
  相互独立；`drain_result` 只表示 drain 的传输结果（例如 `ended`、`timeout`、
  `idle_timeout`、`scanner_error`、`handler_stop`、`disabled`），不代表一定取得权威
  usage。下游写失败不能通过共享 `sr.Stop` 提前截断上游 usage 读取。
- 必须保留：OpenAI Responses HTTP SSE 与 Responses WebSocket 共用
  `ResponsesUsageAccumulator`。终态事件提供的 prompt/output/cache/tool/image usage 优先
  保留；没有权威 output usage 时才按已累计输出文本使用 `tokenkit.Count` 本地估算。明确
  `response.failed`、`response.error`、`error` 或失败状态事件不进入本地估算：有上游
  usage 只保留该 usage，没有 usage 按 0 token 结算；普通断流没有明确失败事件时仍沿用
  估算策略。
- 必须保留：临时运营规则只作用于本地 `tokenkit.Count` 结果恰好为 1 的、没有明确失败
  事件的 Responses 估算值，按 `RelayInfo.RequestId` 的 CRC32 稳定映射到 30--90；request
  ID 为空使用 30。上游明确报告的 `output_tokens=1`、已有正值 usage、缓存字段、工具和
  图片计数不得被改写。该值进入 `Usage`、`TotalTokens`、`PostTextConsumeQuota`、消费
  日志和性能统计，不是只修改页面显示，也不会写入跨请求状态或影响下一个用户。
- 必须保留：该临时 30--90 规则仅覆盖 Responses accumulator；`/v1/messages`、普通
  `/v1/chat/completions` 及不走共享 scanner 的自定义 SSE/WebSocket 路径保持各自原有
  行为。当前观察到的其他协议 `completion_tokens=1` 不能反向证明 Responses 规则失效。
- 必须保留：本项不猜测修复缓存计费问题，不手工重复扣减 `cached_tokens`。只有在同一
  request ID 的终态 SSE/转换 DTO/账务快照确认字段丢失层级后，才修改对应适配层；无
  数据库迁移，不回写历史日志，不改变现有价格表达式。
- 当前位置：`relay/helper/stream_scanner.go`、`relay/helper/common.go`、
  `relay/common/stream_status.go`、`service/log_info_generate.go`、`common/init.go`、
  `constant/env.go`、`.env.example`、`relay/channel/openai/relay_responses.go`、
  `relay/responses_websocket.go`、`service/responses_usage.go`、`service/text_quota.go`。
- 验证入口：`relay/helper/stream_scanner_test.go`、
  `relay/channel/openai/relay_responses_billing_test.go`、`service/responses_usage_test.go`、
  `controller/responses_websocket_test.go`、`service/text_quota_test.go`；至少覆盖
  client gone 后终态/EOF/drain 超时/空闲超时/read error/handler stop、`0` 秒关闭、
  终态优先、本地估算稳定范围以及真实上游 output=1 不改写。
- 数据/配置：仅使用既有 `CLIENT_GONE_DRAIN_TIMEOUT` 环境变量和日志 `stream_status`；
  不新增表或字段，不改变预扣/结算接口。临时估算是运营兜底，待上游终态 usage 对账
  修复后删除 helper、测试和本保护项中的临时分支。

### P-39 Infinite Canvas 免 Key 子应用与持久媒体

- 必须保留：Infinite Canvas 作为登录后的独立子应用运行，不接收 API Key、PAT 或其他
  登录凭据；服务器会话负责身份、分组、权限和计费。`/api/canvas/*` 统一经过 Canvas
  session、用户归属和能力检查，项目、资产、偏好、运行记录及私有媒体按 `user_id`
  隔离，不能退回浏览器本地存储或复用任意用户任务。
- 必须保留：Canvas 的 Responses、图片、编辑、音频、视频和任务 artifact 路径继续复用
  既有 relay/plugin 结算链；项目 CAS、引用保护、私有文件原子写入、运行归档/取消、
  过期清理和 opaque-origin sandbox 行为保持。静态 `/canvas-app` 入口不得重新引入旧
  FileServer 重定向循环或把 session Authorization 传给子应用。
- 必须保留：当前嵌入式 Canvas 源码由 `infinite-canvas/**` 管理，包含 Agent 面板与本地
  Agent 连接、插件宿主与插件管理、媒体分组类型目录、图片/音频/视频文件上传、视频
  参考素材公开发布、图片/视频/音频生成设置和窄屏交互。文件和生成结果继续写入服务端
  用户隔离存储，能力和路由必须来自当前分组与模型目录，不能回退为静态模型名称猜测。
- 当前位置：`router/canvas-router.go`、`middleware/canvas.go`、`middleware/canvas_static.go`、
  `model/canvas.go`、`service/canvas_catalog.go`、`service/canvas_storage.go`、
  `service/canvas_image_publish.go`、`controller/canvas.go`、`controller/canvas_run.go`、
  `web/src/features/canvas/**`、`web/src/routes/_authenticated/canvas/**`、
  `infinite-canvas/web/**`、`infinite-canvas/canvas-agent/**`、
  `infinite-canvas/plugins/**`。
- 验证入口：`model/canvas_test.go`、`controller/canvas_test.go`、`service/canvas_test.go`、
  `middleware/canvas_test.go`、`web/src/features/canvas/__tests__/bridge.test.ts`、
  `web/src/features/canvas/__tests__/host.test.tsx`；真实浏览器验收使用
  `infinite-canvas/browser-product-check.mjs`，由用户自行执行，本轮只登记入口
  且不替代后端测试。

### P-40 分组类型配置、路由和授权选择

- 必须保留：`group_ratio_setting.group_types` 与 `group_ratio_setting.group_type_map` 是
  成对持久化的配置；更新必须经过名称、保留 ID、倍率和分组归属校验，在同一事务写入，
  成功后再替换运行时快照。自动分组 `__auto__` 不是普通类型，旧 `auto` assignment
  只允许按兼容规则归一化。
- 必须保留：`/api/option/group-types`、`/api/user/groups.group_types`、API Key 分组类型
  选择、可用分组路由和系统设置编辑器保持同一份类型/映射事实源；部分更新不得清空另一
  张表或留下数据库与内存不一致。
- 必须保留：分组定价表在保存时按类型配置顺序、同类型倍率升序排序，未分类分组位于有
  类型分组之后，`auto` 固定置底；保存前保持当前编辑顺序，新增分组置顶，输入倍率时不
  实时跳行。排序不改变映射语义或并发的未保存编辑。
- 必须保留：分组定价搜索栏提供类型筛选（全部、各配置类型、未分类）和公开状态筛选
  （全部、公开、未公开），两者默认全部；公开状态直接取“用户可选”复选框，不另设
  持久化字段。
- 当前位置：`model/option.go`、`setting/ratio_setting/group_ratio.go`、`service/group.go`、
  `controller/group.go`、`controller/option.go`、`router/api-router.go`、
  `web/src/features/keys/**`、`web/src/features/system-settings/models/group-type.ts`、
  `web/src/features/system-settings/models/group-type-manager-dialog.tsx`、
  `web/src/features/system-settings/models/group-ratio-visual-editor.tsx`。
- 验证入口：`controller/billing_option_test.go`、`model/option_test.go`、
  `service/group_test.go`、`web/src/features/system-settings/models/__tests__/group-type.test.tsx`、
  `web/src/features/system-settings/models/__tests__/group-settings.test.tsx`、
  `web/src/features/keys/components/__tests__/api-keys-mutate-drawer.test.tsx`。

### P-41 动态分组定价目录与类型分类

- 必须保留：`/api/pricing` 返回的 `groups`、`group_types` 和 `group_catalog_version` 只由
  当前用户可见分组、配置类型和启用渠道能力生成；模型的 `group_type_ids` 使用稳定类型
  ID，不能以显示名称猜测或把隐藏分组泄露给普通用户。旧 `usable_group/group_ratio`
  响应继续兼容，`auto` 仍作为独立的旧数值入口处理。
- 必须保留：前端定价页按类型和分组筛选、模型卡/详情展示、动态/固定/任务价格排序及
  删除或移动类型后的归一化保持；分类目录不得改变共享模型对象或绕过后端可见性裁剪。
- 当前位置：`controller/pricing.go`、`model/pricing.go`、`model/pricing_endpoint_resolver.go`、
  `service/group.go`、`setting/ratio_setting/group_ratio.go`、`web/src/features/pricing/**`。
- 验证入口：`controller/pricing_test.go`、`service/group_test.go`、
  `model/pricing_endpoint_test.go`、`web/src/features/pricing/lib/__tests__/group-categories.test.ts`、
  `web/src/features/pricing/__tests__/pricing-navigation.test.tsx`。

### P-42 系统通知居中确认与版本快照

- 必须保留：系统 Notice 和 Announcements 通过居中 Dialog 展示，内容区域可滚动、宽度受
  约束，外部点击不能在用户确认前关闭；通知铃铛、tab 更新徽标和关闭按钮保持可访问。
- 必须保留：通知版本按实际内容快照计算；打开期间冻结快照，只有显式关闭时才确认当前
  Notice/Timeline 版本。旧 localStorage 状态迁移到版本字段，不因请求竞态、登出、重新
  聚焦或用户切换而把另一账户的内容标记为已读。
- 当前位置：`web/src/components/notification-popover.tsx`、`web/src/hooks/use-notifications.ts`、
  `web/src/stores/notification-store.ts`、`web/src/components/layout/components/app-header.tsx`、
  `web/src/components/layout/components/public-header.tsx`。
- 验证入口：`web/src/components/__tests__/notification-popover.test.tsx`、
  `web/src/hooks/__tests__/notifications.test.tsx`。

### P-43 New API 健康探针、配置快照发布与计费事务观测

- 必须保留：`/health` 是无数据库、无配置锁、无外部请求的进程存活端点，容器健康检查和
  `new-api-proxy` 健康检查使用该端点；业务状态接口 `/api/status` 继续提供完整配置快照，
  但不能承担进程存活探针职责。
- 必须保留：`/api/status` 读取 `OptionMap` 时只复制所需字段并立即释放读锁，响应组装和
  JSON 写出不能持有 `OptionMapRWMutex`。配置同步必须在锁外完成数据库读取、规范化和运行时
  后处理，最后一次性发布内存选项，不能逐项持写锁更新。
- 必须保留：计费事务每次尝试、重试、最终成功/失败、单次耗时、总耗时和重试次数继续通过
  `/metrics` 暴露；指标按真实计费调用方区分，并且中间 1205/1213 重试不能直接等同于最终
  失败。现有 `BILLING_LOCK_WAIT_TIMEOUT_SECONDS`、整事务重试、幂等阶段和账务语义继续有效。
- 当前位置：`controller/misc.go`、`controller/health_test.go`、`router/main.go`、
  `model/option.go`、`model/request_policy.go`、`model/billing_transaction.go`、
  `pkg/billing_metrics/**`、`deploy/docker-compose.yml`。
- 数据/配置：`BILLING_LOCK_WAIT_TIMEOUT_SECONDS` 默认 10 秒，设为 0 使用数据库服务端默认值；
  `/metrics` 不写业务表，不改变 quota、结算、退款或历史账务记录；健康检查切换不改变公开
  `/api/status` 响应契约。
- 验证入口：`controller/health_test.go`、`controller/misc_observation_status_test.go`、
  `model/option_test.go`、`model/request_policy_test.go`、`model/billing_operation_database_matrix_test.go`、
  `pkg/billing_metrics/metrics_test.go`；发布后检查容器 health、`/health`、`/api/status`、
  `/metrics` 和 MySQL `data_lock_waits`/`handler commit`。

### P-44 Task Plugin 图片协议异步提交与查询

- 必须保留：`openai_image` 继续支持原有同步图片生成/编辑请求；声明
  `supports: ["background"]` 的插件可接受 `background: true`，提交立即返回可查询的任务
  对象，`GET /v1/images/generations/:task_id` 和 `GET /v1/images/edits/:task_id` 按任务归属
  返回排队、进行中、完成或失败状态，完成时仍由插件 `render` 生成标准图片响应。
- 必须保留：异步图片任务复用现有 task 持久化、轮询、计费预扣/结算和用户归属检查；后台
  任务必须保留 `task.data`，同步图片的即时终态仍可按原契约不保留。查询渲染只恢复无凭据的
  协议上下文，不保存客户端认证头或上传文件字节；历史裸字符串/缺少 supports 的图片声明
  继续按同步能力兼容。
- 当前位置：`pkg/jsplugin/routing.go`、`pkg/jsplugin/registry.go`、
  `router/task-plugin-protocol-router.go`、`controller/plugin_protocol_image.go`、
  `controller/relay.go`、`model/task.go`、`plugins/tasks/alibaba/plugin.js`、
  `plugins/tasks/doubao/plugin.js`、`docs/plugin-api/v1.md`、`docs/plugin-api/v1.schema.json`。
- 验证入口：`controller/plugin_protocol_test.go`、`controller/relay_task_plugin_test.go`、
  `pkg/jsplugin/routing_test.go`、`router/task_plugin_protocol_router_test.go`；至少覆盖同步兼容、
  background 提交不等待、即时结果保留、任务查询渲染和失败状态。


## 5. 当前审查状态

- 当前保护项数量：36。
- 当前条目中的路径、配置、验证入口和行为描述是唯一维护对象。
- P-01 的 GitHub Actions、P-05 的 390px/768px/桌面响应式 smoke、P-39 的 Canvas 真实浏览器验收由用户或发布流程执行。
- 任何后续维护只更新当前功能状态，不追加提交历史或变更时间线。
