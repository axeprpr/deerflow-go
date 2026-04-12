# 上游 Issue 回归用例

本文把 `bytedance/deer-flow` 的上游 issue 转成适合当前 `deerflow-go` 环境的回归用例。

目标不是机械复现所有 issue，而是筛出：

- 和当前后端/runtime 直接相关
- 能在当前环境里稳定跑
- 对后续 runtime core 改造有约束价值

## 选择原则

优先纳入：

- skill/工具选择
- tool-call 连续性
- 长链路 summarization / loop detection
- provider / model 映射
- 搜索与外部工具可用性

暂不作为当前后端主回归目标：

- 纯前端状态问题
- 认证/会话签名问题
- 与当前部署模式不一致的上游专属问题

## 当前环境

- UI：直接使用原版 DeerFlow 前端
- 后端：`deerflow-go`
- 运行时：自定义 ReAct + `harness/harnessruntime`
- 接口约束：原版 UI 主接口必须保持一致

## 当前覆盖状态

| 优先级 | Case | 状态 | 当前依据 |
| --- | --- | --- | --- |
| P0 | P0-1 Skill 可发现且可触发 | 已覆盖 | `pkg/langgraphcompat/issue_regression_test.go`，`runtime_prompt/runtime_skill_paths` 相关回归 |
| P0 | P0-2 Tool-call 连续性不丢失 | 已覆盖 | `pkg/agent/issue_regression_test.go`，tool-call/tool-result 配对回归 |
| P0 | P0-3 长 CSV 分析不应重复执行已完成步骤 | 已覆盖 | `pkg/langgraphcompat/issue_regression_test.go` 的 deterministic golden + `live_behavior_test.go` 的真实 CSV 长链路回归 |
| P1 | P1-1 联网搜索工具可用 | 部分覆盖 | 默认 runtime tool surface 已暴露 `web_search/web_fetch/image_search`，且失败诊断已回归；尚未做稳定 live 网络 smoke |
| P1 | P1-2 模型映射和 provider 错误可诊断 | 已覆盖 | `runtime_model_resolution_test.go` + `pkg/agent/issue_regression_test.go` |
| P2 | P2-1 认证签名错误 | 未覆盖 | 当前仅记录，不纳入 runtime 主回归 |
| P2 | P2-2 切换聊天后内容为空 | 未覆盖 | 当前仅记录，不纳入后端/runtime 主回归 |

## P0 用例

### Case P0-1: Skill 可发现且可触发

来源：

- `#2143` [skill无法触发，即使明确要求也不行](https://github.com/bytedance/deer-flow/issues/2143)
- `#2125` [自定了SKILL，deer-flow找不到](https://github.com/bytedance/deer-flow/issues/2125)

问题类型：

- skill 目录发现
- custom/public 优先级
- skill prompt 暴露
- agent 是否实际读取 `SKILL.md`

前置条件：

- `skills/public/<name>/SKILL.md`
- `skills/custom/<name>/SKILL.md`
- `skills.path` 和 `container_path` 正常

测试输入：

- `请使用 scenic-spot-analytics skill 分析数据`
- `请按照 frontend-design skill 生成页面`

断言：

- skill 出现在运行时可见技能列表中
- custom skill 会覆盖同名 public skill
- 首轮或前几轮存在对 `/mnt/skills/.../SKILL.md` 的读取
- 不应错误回退到 `/mnt/user-data` 查找 skill

自动化建议：

- 单测：`runtime_prompt / runtime_skill_paths`
- live：检查 event stream 中是否出现 `read_file(/mnt/skills/.../SKILL.md)`

当前优先级：

- 最高

当前状态：

- 已覆盖

当前自动化依据：

- `pkg/langgraphcompat/issue_regression_test.go`
- `pkg/langgraphcompat/runtime_prompt_test.go`
- `pkg/langgraphcompat/runtime_skill_paths` 相关回归

### Case P0-2: Tool-call 连续性不丢失

来源：

- `#2123` [为什么任务进行到一半总是报错啊 LLM400](https://github.com/bytedance/deer-flow/issues/2123)

问题类型：

- `assistant tool_calls -> tool messages` 配对丢失
- 流式 tool-call 分片恢复错误
- 中途恢复/继续后消息序列不合法

测试输入：

- 代码生成类长链路任务
- 至少包含 `read_file -> write_file -> present_files` 的多步 tool turn

断言：

- 每个 assistant `tool_call` 后面都能看到对应 `tool_result`
- 不出现 provider `400`
- 错误文案不出现：
  - `insufficient tool messages following tool_calls message`

自动化建议：

- 单测：tool-call streaming / replay / snapshot / continuation
- live：固定 prompt 多轮执行，检查最终线程状态必须为 `success`

当前优先级：

- 最高

当前状态：

- 已覆盖

当前自动化依据：

- `pkg/agent/issue_regression_test.go`
- 已有 tool-call streaming / replay / continuation 相关回归

### Case P0-3: 长 CSV 分析不应重复执行已完成步骤

来源：

- `#2126` [CSV 数据分析时对话被截断、丢失记忆并重复执行已完成操作](https://github.com/bytedance/deer-flow/issues/2126)
- `#2116` [Loop detection false positives on legitimate retries](https://github.com/bytedance/deer-flow/issues/2116)

问题类型：

- summarization 触发过早
- 历史压缩后丢关键操作信息
- loop detection 对合法 retry 误判
- agent 重复读取/重复查询/重复生成

前置条件：

- 上传一个 100-150 行的脏数据 CSV
- 使用上下文窗口较小、容易触发长链路的模型

测试输入：

- `分析这个 CSV，给出异常值、分组统计，并生成两张图表`

断言：

- 不应过早进入重复 `read/query/chart` 循环
- 同一逻辑步骤不应无意义重复超过阈值
- 最终应生成稳定 artifact
- summarization 后仍保留“已完成哪些步骤”的可恢复状态

自动化建议：

- 当前先做 live 手工回归
- 后续演进为上传文件 + 长任务 golden case

当前优先级：

- 最高

当前状态：

- 已覆盖

当前自动化依据：

- `pkg/langgraphcompat/issue_regression_test.go`
- `pkg/langgraphcompat/live_behavior_test.go`

当前覆盖说明：

- deterministic golden 会先触发长历史 compaction，再验证下一轮只补写剩余的 `orders-summary.json`
- live 回归继续覆盖真实上传 CSV、真实线程执行、稳定 artifact 生成，以及“不重复成功写同一路径”

## P1 用例

### Case P1-1: 联网搜索工具可用

来源：

- `#2139` [为什么联网搜索能力一直无效？](https://github.com/bytedance/deer-flow/issues/2139)

问题类型：

- web search provider 不可用
- 网络配置错误
- 工具虽注册但执行失败

测试输入：

- `联网搜索今天的 OpenAI API 价格页`

断言：

- 存在搜索工具调用
- 不出现泛化的 `网络错误`
- 如果 provider 不可用，应返回明确可诊断错误

自动化建议：

- 依赖外部网络，先作为 live smoke

当前优先级：

- 中

当前状态：

- 部分覆盖

当前自动化依据：

- `pkg/langgraphcompat/issue_regression_test.go`
- `pkg/tools/builtin/web_test.go`
- `pkg/harnessruntime/tool_runtime_test.go`

当前缺口：

- 还没有加稳定 live 网络 smoke
- 当前主要覆盖：
  - 搜索工具默认必须暴露
  - 搜索失败必须保留可诊断错误

### Case P1-2: 模型映射和 provider 错误可诊断

来源：

- `#2124` [模型设置为GLM-5.1就不能用了 显示网络错误](https://github.com/bytedance/deer-flow/issues/2124)

问题类型：

- UI 模型名与 provider 实际模型名映射错误
- provider 失败被包装成模糊“网络错误”

测试输入：

- 切换到一个别名模型并发起最小对话

断言：

- `/api/models` 暴露的模型名可正常解析到 provider 模型
- 失败时返回的是 provider 可诊断错误，而不是笼统网络错误

自动化建议：

- 单测：model resolution
- live：最小 chat smoke

当前优先级：

- 中

当前状态：

- 已覆盖

当前自动化依据：

- `pkg/langgraphcompat/runtime_model_resolution_test.go`
- `pkg/agent/issue_regression_test.go`

当前覆盖点：

- alias -> provider model 解析
- `reasoning_effort` 能力裁剪
- provider 原始错误诊断不得塌成泛化“network error”

## P2 用例

### Case P2-1: 认证签名错误

来源：

- `#2136` [登录后对话或执行其它行为都报错 invalid_signature](https://github.com/bytedance/deer-flow/issues/2136)

当前判断：

- 更偏上游认证/会话集成问题
- 不是当前 `deerflow-go runtime` 的核心回归目标

处理方式：

- 记录
- 不纳入当前 runtime 主回归集

当前状态：

- 未覆盖

### Case P2-2: 切换聊天后内容为空

来源：

- `#2120` [conversation content becomes empty when navigating back from another chat](https://github.com/bytedance/deer-flow/issues/2120)

当前判断：

- 明确是前端 `useStream` + App Router 复用问题
- 不是当前后端/runtime 的直接回归目标

处理方式：

- 记录
- 不纳入当前后端主回归集

当前状态：

- 未覆盖

## 建议执行顺序

先跑：

1. P0-1 Skill 发现与触发
2. P0-2 Tool-call 连续性
3. P0-3 长 CSV 分析与重复执行

再跑：

4. P1-1 联网搜索
5. P1-2 模型映射

最后只记录不纳入主线：

6. P2-1 invalid_signature
7. P2-2 前端切换聊天空白

## 当前建议

下一步最值得做的是把 P0 用例变成我们自己的回归基线：

- `skill-trigger`
- `tool-call-continuity`
- `csv-long-run-no-repeat`

其中前两条适合先自动化，第三条适合先 live 复测，再决定是否做重型 golden。
