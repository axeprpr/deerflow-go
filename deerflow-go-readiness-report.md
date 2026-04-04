# deerflow-go 可用性评估报告
生成时间: 2026-04-04
仓库: github.com/axeprpr/deerflow-go

---

## 一、LangGraph API 路由覆盖

deerflow-go 在 `/` 和 `/api/langgraph` 两个路径注册了 LangGraph 兼容路由：

| 端点 | 方法 | 状态 | 备注 |
|------|------|------|------|
| `/runs/stream` | POST | ✅ | SSE 流式执行 |
| `/runs` | POST | ✅ | 同步执行 |
| `/runs/{run_id}` | GET | ✅ | 获取 Run 状态 |
| `/runs/{run_id}/stream` | GET | ✅ | Run 事件流 |
| `/runs/{run_id}/cancel` | POST | ✅ | 取消执行 |
| `/threads` | GET/POST | ✅ | 线程列表/创建 |
| `/threads/{id}` | GET/PUT/PATCH/DELETE | ✅ | 完整 CRUD |
| `/threads/search` | POST | ✅ | 搜索线程 |
| `/threads/{id}/state` | GET/PUT/PATCH | ✅ | 状态读写 |
| `/threads/{id}/history` | GET/POST | ✅ | 对话历史 |
| `/threads/{id}/runs` | GET/POST | ✅ | 线程内 Run |
| `/threads/{id}/runs/stream` | POST | ✅ | 线程内流执行 |
| `/threads/{id}/runs/{run_id}/stream` | GET | ✅ | Run 事件流 |
| `/threads/{id}/clarifications` | GET/POST | ✅ | 澄清机制 |
| `/threads/{id}/clarifications/{id}/resolve` | POST | ✅ | 解决澄清 |
| `/uploads` | POST/GET/DELETE | ✅ | 文件上传 |
| `/artifacts/{path...}` | GET | ✅ | Artifact 获取 |
| `/suggestions` | POST | ✅ | 建议生成 |

**LangGraph API 结论: ✅ 完整**

---

## 二、Gateway API 路由覆盖

| 端点 | 方法 | 状态 | 备注 |
|------|------|------|------|
| `/api/skills` | GET | ✅ | 返回 defaultGatewaySkills() |
| `/api/skills/{name}` | GET/PUT | ✅ | 启用/禁用 skills |
| `/api/skills/install` | POST | ✅ | 动态安装 skill |
| `/api/agents` | GET/POST | ⚠️ | 仅返回默认配置，无持久化 |
| `/api/agents/{name}` | GET/PUT/DELETE | ⚠️ | 仅内存操作 |
| `/api/channels` | GET | ✅ | 仅返回 gatewayChannelService |
| `/api/channels/{name}/restart` | POST | ✅ | 重启通道 |
| `/api/mcp/config` | GET/PUT | ✅ | MCP 配置读写 |
| `/api/memory` | GET/PUT/DELETE | ✅ | 记忆配置 |
| `/api/user-profile` | GET/PUT | ✅ | 用户配置 |
| `/api/tts` | POST | ✅ | TTS 服务 |
| `/api/models` | GET | ✅ | 模型列表 |

**Gateway API 结论: ⚠️ 路由完整，但部分无持久化**

---

## 三、deerflow-ui 前端对接评估

### 前端调用分析

前端（Next.js）使用两套接口：

1. **LangGraph SDK** (`@langchain/langgraph-sdk/client`)
   - 路径: `getLangGraphBaseURL()` → `/api/langgraph`
   - 用途: Run 执行、线程管理、消息流
   - ✅ **deerflow-go 完整兼容**（LangGraph API 是核心）

2. **Backend API** (`getBackendBaseURL()`)
   - 路径: `/api/skills`, `/api/agents`, `/api/channels` 等
   - 用途: Skills 管理、Agent 管理、Channels 配置
   - ⚠️ **部分可用**（无认证，无持久化）

### 对接条件

```
deerflow-ui 需要配置:
NEXT_PUBLIC_BACKEND_BASE_URL=http://go-backend:8080   # gateway API
NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://go-backend:8080   # langgraph API
```

### 关键问题

#### 1. 认证缺失（🔴 阻塞）
- Python deerflow: 使用 BetterAuth 做用户认证
- Go deerflow-go: **无任何认证中间件**
- 影响: 前端所有需要登录的操作会失败（如果前端开启了登录墙）

#### 2. 持久化缺失（⚠️ 需确认）
- Skills: 写操作仅在内存中，重启丢失
- Agents: 创建的 agent 无持久化
- Channels: 通道配置无持久化

#### 3. CORS 配置（✅ OK）
- Go 服务使用 `Access-Control-Allow-Origin: *`
- 前端运行在 `localhost:3000`，Go 在 `localhost:8080`
- ✅ 跨域没问题

### 结论
**deerflow-ui 可以对接 deerflow-go**，前提：
1. 前端关闭登录验证（或在 YOLO 模式下）
2. 接受 skills/agents/channels 无持久化（重启重置）
3. 主要功能（Run 执行、消息流、线程管理）✅ 完整可用

---

## 四、YOLO 模式构建方案

### YOLO 模式定义
- ✅ 无需登录
- ✅ 内置默认配置（无需 .env）
- ✅ SQLite 可选（无 DB 时用文件存储）
- ✅ MCP servers 默认禁用
- ✅ 文件存储使用 `./data` 目录
- ✅ 所有配置通过环境变量或 flag 注入

### 当前状态
- ✅ `cmd/langgraph/main.go` 是 YOLO 入口
- ✅ 支持 `-addr`, `-db`, `-model` flags
- ✅ 默认 LLM: `qwen/Qwen3.5-9B`
- ✅ 文件存储回退: `os.TempDir()/deerflow-go-data`
- ⚠️ 无 `--yolo` 单一 flag

### 建议改进
1. 添加 `--yolo` flag: 一次性设置所有默认值
2. 添加 `config.yaml` 内置默认值
3. 添加 GitHub Actions release workflow

---

## 五、GitHub Actions 发布配置

### 现状
- ✅ Dockerfile 已有（`Dockerfile.alpine`）
- ❌ 无 `.github/workflows/release.yml`

### 目标
- Tag push (`v*.*.*`) 触发构建
- 构建产物: `deerflow-gateway-linux-amd64`, `deerflow-gateway-darwin-arm64`
- 直接发布到 GitHub Releases（无需 draft）
- 支持 Docker build + push

---

## 六、总体结论

| 维度 | 评分 | 说明 |
|------|------|------|
| LangGraph API | ⭐⭐⭐⭐⭐ | 完整实现，核心功能可用 |
| Gateway API | ⭐⭐⭐ | 路由全，持久化弱 |
| 前端对接 | ⭐⭐⭐ | 可用，需关闭登录 |
| YOLO 模式 | ⭐⭐⭐ | 基础 OK，缺单一 flag |
| 自动化发布 | ⭐⭐ | Dockerfile 有，CI/CD 无 |
| **综合** | **B+** | **可使用，不适合生产** |

### 建议优先级

**P0（可直接集成）:**
- [ ] 创建 GitHub Actions release workflow
- [ ] 添加 `--yolo` 单一入口 flag

**P1（提升完成度）:**
- [ ] 添加 Skills 持久化（JSON 文件）
- [ ] 确认认证方案（Bearer token 或禁用）

**P2（可选）:**
- [ ] 添加 Agents 持久化
- [ ] 添加 Channels 动态管理
