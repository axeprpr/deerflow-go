# deerflow-go 架构设计文档

> 版本：v0.1.0 | 状态：Draft | 作者：axe3

---

## 1. 背景与目标

### 1.1 为什么重写

DeerFlow（字节跳动）是目前最完整的开源 Agent 工作站，但它存在以下问题：

| 问题 | 影响 |
|------|------|
| 强依赖 LangGraph | LangGraph Platform 是闭源商业版，多实例部署需要付费授权 |
| Python 性能 | 单次 LLM 调用开销大，ReAct 循环延迟明显 |
| LangChain 复杂性 | 框架过于通用，定制成本高，调试困难 |
| 部署门槛 | 需要 LangGraph API Server 或 LangGraph Platform |

### 1.2 重写目标

1. **零 LangChain 依赖** —— 完全用 Go 重写
2. **性能优先** —— LLM 调用异步化，P95 延迟降低 5-10x
3. **可私有部署** —— 单二进制 + Postgres，无需特殊基础设施
4. **完全兼容 DeerFlow 协议** —— 复用 DeerFlow 的 Frontend、ACP（Agent Client Protocol）、MCP 工具

### 1.3 放弃的部分

以下 DeerFlow 特性被评估为「非必要」，暂不实现：

| 特性 | 原因 |
|------|------|
| LangGraph 状态机 | 用更简单的 Session + Message 结构替代 |
| LangSmith 可观测性 | 用 OpenTelemetry 替代 |
| Kubernetes 部署支持 | 聚焦单实例和 Docker 部署 |
| 多租户隔离 | 目标用户为个人/小团队 |

---

## 2. 整体架构

```
┌──────────────────────────────────────────────────────────────┐
│                      Frontend (复用 DeerFlow)                  │
│         Next.js Web + IM 集成 (Slack/Discord/TG/QQ)          │
├──────────────────────────────────────────────────────────────┤
│                  ACP Gateway (Go)                            │
│         HTTP API + SSE 流式响应 + IM Webhook 接收            │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│   ┌──────────────┐      ┌──────────────┐                    │
│   │ Lead Agent   │      │ Subagents    │                    │
│   │ (ReAct Loop) │◄────►│ Pool         │                    │
│   └──────┬───────┘      └──────┬───────┘                    │
│          │                     │                             │
│   ┌──────▼───────┐      ┌──────▼───────┐                    │
│   │  Tool Router │      │  Tool Router │                    │
│   └──────┬───────┘      └──────┬───────┘                    │
│          │                     │                             │
│   ┌──────▼───────┐      ┌──────▼───────┐                    │
│   │   Sandbox    │      │  MCP Client  │                    │
│   │  (Landlock)  │      │              │                    │
│   └──────────────┘      └──────────────┘                    │
│                                                              │
│   ┌──────────────┐      ┌──────────────┐                    │
│   │   Memory     │      │  Checkpoint  │                    │
│   │   (LLM-based)│      │  (Postgres)  │                    │
│   └──────────────┘      └──────────────┘                    │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### 2.1 核心组件

| 组件 | 职责 | 语言 |
|------|------|------|
| ACP Gateway | HTTP API、认证、会话路由 | Go |
| Lead Agent | ReAct 主循环、任务分解、子 Agent 调用 | Go |
| Subagent Pool | 并行执行子任务、超时控制 | Go |
| Tool Router | 工具注册、参数校验、结果归一化 | Go |
| Sandbox | Landlock 进程隔离、文件系统限制、网络白名单 | Go + C (Landlock绑定) |
| MCP Client | MCP 协议客户端，连接外部工具服务 | Go |
| Memory | LLM-based 记忆提取与存储 | Go + LLM API |
| Checkpointer | Postgres 会话状态持久化 | Go + pg |

---

## 3. 数据模型

### 3.1 Session（会话）

```go
type Session struct {
    ID          string            // 唯一会话 ID (UUID)
    UserID      string            // 用户标识
    CreatedAt   time.Time
    UpdatedAt   time.Time
    State       SessionState      // active / completed / archived
    Metadata    map[string]string // 自由元数据
}

type SessionState int
const (
    SessionActive    SessionState = iota
    SessionCompleted
    SessionArchived
)
```

### 3.2 Message（消息）

```go
type Message struct {
    ID        string
    SessionID string
    Role      Role        // human / ai / system / tool
    Content   string
    ToolCalls []ToolCall  `json:",omitempty"`
    ToolResult *ToolResult `json:",omitempty"`
    Metadata  MessageMetadata
    CreatedAt time.Time
}

type Role string
const (
    RoleHuman Role = "human"
    RoleAI    Role = "ai"
    RoleSystem Role = "system"
    RoleTool   Role = "tool"
)
```

### 3.3 ToolCall（工具调用）

```go
type ToolCall struct {
    ID       string                 // 唯一 ID
    Name     string                 // 工具名
    Args     map[string]interface{} // 参数
    Result   string                 // 执行结果
    Status   CallStatus            // pending / running / completed / failed
    Duration time.Duration          // 执行耗时
}

type CallStatus int
const (
    CallPending   CallStatus = iota
    CallRunning
    CallCompleted
    CallFailed
)
```

### 3.4 Tool（工具定义）

```go
type Tool struct {
    Name        string
    Description string
    Schema      *jsonschema.Schema   // JSON Schema 参数定义
    Handler     ToolHandler
    Groups      []string             // 工具分组 (researcher / file_ops / code)
}

type ToolHandler func(ctx context.Context, args map[string]interface{}, sandbox *Sandbox) (string, error)
```

---

## 4. Sandbox 设计（重点）

### 4.1 隔离模型

采用 **Linux Landlock**（kernel 5.13+）实现进程级隔离，比 Docker 轻量一个量级。

```
每条对话 = 1 个 Session 目录 + 1 个受限进程组
    └── Landlock 规则绑定该进程组
        ├── 文件系统：限制在 Session 目录内
        └── 网络：基于白名单（可选）
```

### 4.2 实现原理

Landlock 允许非 root 用户限制自己后代进程的权限。通过 `seccomp` + `landlock` syscall 实现：

```go
// 伪代码
func (s *Sandbox) restrictDir(root string) error {
    ruleset := landlock.NewRuleset().
        AddPathBeneath(root, landlock.Read|landlock.Write).
        Create()
    return landlock.Run(ruleset, func() {
        exec.Command("bash", "-c", s.cmd).Run()
    })
}
```

### 4.3 Sandbox API

```go
type Sandbox struct {
    sessionDir string
    processes  []*os.Process
    netPolicy  *NetworkPolicy
}

// 创建会话 Sandbox
func NewSandbox(sessionID string) (*Sandbox, error)

// 执行命令
func (s *Sandbox) Exec(ctx context.Context, cmd string) (*ExecResult, error)

// 上传文件
func (s *Sandbox) WriteFile(path string, data []byte) error

// 下载文件
func (s *Sandbox) ReadFile(path string) ([]byte, error)

// 清理
func (s *Sandbox) Close() error
```

### 4.4 网络白名单

```go
type NetworkPolicy struct {
    AllowTCP []*netaddr.IPPort  // 允许的 TCP 目标
    AllowUDP []*netaddr.IPPort
    DenyAll  bool               // 默认拒绝所有出站
}

func (s *Sandbox) SetNetPolicy(policy NetworkPolicy) error
```

### 4.5 回退方案

如果 Landlock 不可用（内核 < 5.13 或环境限制），使用 **Bubblewrap** 作为回退：

```bash
bwrap --dev /dev/null \
      --ro-bind /usr /usr \
      --bind /tmp/session-xxx /workspace \
      --die-with-parent \
      bash -c "..."
```

---

## 5. Agent 系统

### 5.1 Lead Agent（ReAct 主循环）

```
while tokens_remaining > 0:
    1. 构建 Prompt（含 Memory Context + Tools Schema）
    2. 调用 LLM（流式）
    3. LLM 返回：
       - 文本 → 输出给用户
       - ToolCall → 执行工具 → 返回结果 → 继续
       - 结束信号 → 退出循环
```

```go
type LeadAgent struct {
    llm        LLMClient
    tools      *ToolRegistry
    memory     *MemoryService
    sandbox    *Sandbox
    maxTurns   int
}

func (a *LeadAgent) Run(ctx context.Context, sessionID string, input string) (*RunResult, error)
```

### 5.2 Subagent Pool（子 Agent 池）

```go
type SubagentPool struct {
    maxConcurrent int
    timeout       time.Duration
    registry      *ToolRegistry
}

func (p *SubagentPool) Execute(ctx context.Context, task string, cfg SubagentConfig) (*SubagentResult, error)
```

Subagent 和 Lead Agent 共用同一个 Sandbox，但有独立的工具限制：

```go
type SubagentConfig struct {
    Name        string
    SystemPrompt string
    Tools       []string        // 允许的工具列表（白名单）
    Disallowed  []string        // 禁止的工具列表（黑名单）
    MaxTurns    int
    Timeout     time.Duration
}
```

### 5.3 工具组（Tool Groups）

```go
const (
    GroupResearcher  = "researcher"   // 搜索、网页抓取
    GroupFileOps     = "file_ops"     // 文件读写
    GroupCode        = "code"         // 代码执行、Git
    GroupSystem      = "system"       // 系统信息
    GroupMCP         = "mcp"          // MCP 工具
)
```

---

## 6. 工具系统

### 6.1 内置工具

| 工具 | 组 | 说明 |
|------|-----|------|
| `bash` | code | Sandbox 内执行 shell 命令 |
| `read_file` | file_ops | 读取文件 |
| `write_file` | file_ops | 写入文件 |
| `glob` | file_ops | 按模式匹配文件 |
| `grep` | file_ops | 搜索文件内容 |
| `task` | researcher | 启动 Subagent 执行子任务 |
| `clarification` | researcher | 向用户提问澄清 |
| `todo_write` | researcher | 写入 TODO 列表 |

### 6.2 MCP 工具

```go
type MCPClient struct {
    servers map[string]*MCPClient
}

func (c *MCPClient) CallTool(ctx context.Context, server, tool string, args map[string]interface{}) (string, error)
```

### 6.3 工具注册

```go
registry := NewToolRegistry()
registry.Register(Tool{
    Name: "bash",
    Description: "Execute shell commands in the sandbox",
    Schema: loadSchema("bash"),
    Handler: builtin.BashHandler,
    Groups: []string{GroupCode},
})
```

---

## 7. Memory 系统

### 7.1 设计思路

沿用 DeerFlow 的结构，但完全用 Go 实现：

```
Memory 更新时机：
    └── Lead Agent 每轮对话结束后异步触发

Memory 数据结构：
    ├── user.workContext          // 用户工作上下文摘要
    ├── user.personalContext     // 用户个人信息摘要
    ├── user.topOfMind           // 用户当前最关心的事
    ├── history.recentMonths     // 最近几月重要事件
    ├── history.earlierContext   // 早期背景
    ├── facts[]                  // 结构化事实 (id, content, category, confidence)
    └── source                    // 来源 session
```

### 7.2 LLM 更新流程

```go
func (m *Memory) Update(ctx context.Context, sessionID string, messages []Message) error {
    // 1. 构建更新 Prompt
    prompt := buildMemoryUpdatePrompt(messages, m.currentMemory)

    // 2. 调用 LLM（轻量模型）
    resp, err := m.llm.Call(ctx, prompt)
    if err != nil {
        return err
    }

    // 3. 解析 JSON 更新
    update := parseUpdate(resp)

    // 4. 合并到 Memory
    m.storage.Save(update)
    return nil
}
```

### 7.3 注入到 System Prompt

```go
func (m *Memory) Inject(sessionID string) string {
    // Memory 内容注入 System Prompt
    return fmt.Sprintf("## User Memory\n%s\n## Current Session\n", m.storage.Load())
}
```

---

## 8. Checkpoint / 会话持久化

### 8.1 Postgres Schema

```sql
CREATE TABLE sessions (
    id          UUID PRIMARY KEY,
    user_id     VARCHAR(255) NOT NULL,
    state       VARCHAR(50) NOT NULL DEFAULT 'active',
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE messages (
    id          UUID PRIMARY KEY,
    session_id  UUID REFERENCES sessions(id) ON DELETE CASCADE,
    role        VARCHAR(50) NOT NULL,
    content     TEXT NOT NULL,
    tool_calls  JSONB,
    tool_result TEXT,
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_messages_session ON messages(session_id, created_at);
```

### 8.2 实现

```go
type Checkpointer struct {
    db *pgxpool.Pool
}

func (c *Checkpointer) SaveMessage(ctx context.Context, msg Message) error
func (c *Checkpointer) LoadSession(ctx context.Context, sessionID string) ([]Message, error)
func (c *Checkpointer) UpdateSessionState(ctx context.Context, sessionID, state string) error
```

---

## 9. LLM 接口抽象

### 9.1 Provider 接口

```go
type LLMProvider interface {
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    Stream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

type ChatRequest struct {
    Model         string
    Messages      []Message
    Tools         []Tool          // 可选，工具定义
    Thinking      *ThinkingConfig  // 可选，推理参数
    Temperature   float64
    MaxTokens    int
}

type StreamChunk struct {
    Delta      string
    FinishReason string
    Usage      *Usage
}
```

### 9.2 已支持 Provider

| Provider | 状态 | 说明 |
|----------|------|------|
| OpenAI | ✅ | GPT-4o / o3 |
| Anthropic | ✅ | Claude 3.5/3.7 Sonnet |
| SiliconFlow | ✅ | Qwen 等国产模型 |
| Ollama | ✅ | 本地模型 |
| DeepSeek | 计划 | |

---

## 10. ACP 协议

沿用 DeerFlow 的 ACP（Agent Client Protocol）：

### 10.1 核心端点

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/chat/runs` | 创建对话流 |
| `GET` | `/v1/chat/runs/{thread_id}` | 查询运行状态 |
| `GET` | `/v1/memory` | 获取记忆 |
| `PUT` | `/v1/memory` | 更新记忆 |
| `GET` | `/v1/models` | 列出可用模型 |
| `GET` | `/v1/skills` | 列出可用 Skills |

### 10.2 SSE 流式响应

沿用 LangGraph SSE 协议：

```
event: values
data: {"title":"...","messages":[...],"artifacts":[]}

event: messages-tuple
data: {"type":"ai","content":"...","id":"..."}

event: messages-tuple
data: {"type":"tool","name":"bash","content":"...","tool_call_id":"..."}

event: end
data: {"usage":{"input_tokens":...,"output_tokens":...}}
```

---

## 11. 实现计划

### Phase 1：核心骨架（Week 1-2）
- [ ] 项目初始化（Go Module、Docker、Makefile）
- [ ] 数据模型（Session、Message、Tool）
- [ ] LLM Provider 抽象（OpenAI + SiliconFlow）
- [ ] 基本 ReAct 循环（无工具）

### Phase 2：Sandbox + 工具（Week 3-4）
- [ ] Landlock Sandbox 实现
- [ ] 内置工具（Bash、FileOps）
- [ ] 工具注册系统
- [ ] Tool Router

### Phase 3：多 Agent（Week 5-6）
- [ ] Subagent Pool
- [ ] Task 工具
- [ ] 子 Agent 工具限制

### Phase 4：持久化 + Memory（Week 7-8）
- [ ] Postgres Checkpointer
- [ ] Memory Service
- [ ] ACP Gateway

### Phase 5：MCP + 完善（Week 9-10）
- [ ] MCP Client
- [ ] 完善工具集
- [ ] Frontend 集成测试
- [ ] 性能调优

---

## 12. 参考资料

- [DeerFlow (ByteDance)](https://github.com/bytedance/deer-flow)
- [Claude Code Sandboxing](https://code.claude.com/docs/en/sandboxing)
- [Landlock LSM](https://www.kernel.org/doc/html/latest/security/landlock.html)
- [Agent Client Protocol](https://github.com/bytedance/deer-flow/tree/main/protocol)
- [LangGraph Architecture](https://langchain-ai.github.io/langgraph/concepts/)

---

## 13. 多层隔离架构

### 13.1 三层隔离模型

按代码可信度从低到高分配隔离层级：

```
Layer 1: gVisor（进程级沙箱）
└── 用途：跑 agent 生成的用户代码（Shell、Bash 命令）
└── 资源：CPU 限制、内存限制、文件系统只读/可写分区
└── 工具：google/gvisor runsc

Layer 2: Pod（容器级隔离）
└── 用途：独立会话环境，跨会话隔离
└── 资源：独立网络命名空间、进程命名空间
└── 工具：Kubernetes Pod 或 Docker

Layer 3: VM（虚拟机隔离）
└── 用途：跑完全不可信的外部代码
└── 资源：独立内核、完全隔离
└── 工具：预热 VM 池（QEMU/Libvirt）
```

### 13.2 VM Pool 架构（推荐）

在已有物理机上部署：

```
物理机池（r.axe3.cn、f.axe3.cn）
    ├── 预启动 3-5 个 VM
    ├── 每个 VM 运行 gVisor ready
    └── 每会话分配一个 VM

对话结束 → VM 归还池 → 重置 → 等待下一个
```

### 13.3 Kubernetes 部署

```
┌─────────────────────────────────────┐
│  K8s Cluster                        │
│                                     │
│  Deployment: deerflow-go API Server │
│  Service: LoadBalancer               │
│                                     │
│  DaemonSet: gVisor runsc            │
│  └── 每节点预装 runsc               │
│                                     │
│  Pod per Session:                   │
│  └── Landlock/Bubblewrap           │
│  └── 或 gVisor runsc               │
│                                     │
│  （重度隔离场景）                    │
│  └── VM Pod（KubeVirt）             │
└─────────────────────────────────────┘
```

## 14. ACP 协议（规划中）

### 14.1 定位

ACP 用于将外部 Agent（Claude Code、Codex 等）作为子进程接入 deerflow-go 的执行管道。

### 14.2 协议格式

```
主进程（deerflow-go）
    └── spawn: Claude Code / Codex
              └── stdin: JSON-RPC 命令
              └── stdout: JSON-RPC 响应
```

### 14.3 依赖

- 主协议：`agentclientprotocol/agent-client-protocol`
- Go SDK：`coder/acp-go-sdk`（生态成熟后接入）
- 优先级：Phase 4 之后
