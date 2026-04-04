# deerflow-go 可用性评估报告 v2
更新时间: 2026-04-04 18:25
仓库: github.com/axeprpr/deerflow-go

---

## 一、LangGraph API 路由覆盖 ✅

| 端点 | 方法 | 状态 |
|------|------|------|
| `/runs/stream` | POST | ✅ |
| `/runs` | POST | ✅ |
| `/runs/{run_id}` | GET | ✅ |
| `/runs/{run_id}/stream` | GET | ✅ |
| `/runs/{run_id}/cancel` | POST | ✅ |
| `/threads` | GET/POST | ✅ |
| `/threads/{id}` | GET/PUT/PATCH/DELETE | ✅ |
| `/threads/search` | POST | ✅ |
| `/threads/{id}/state` | GET/PUT/PATCH | ✅ |
| `/threads/{id}/history` | GET/POST | ✅ |
| `/threads/{id}/runs` | GET/POST | ✅ |
| `/threads/{id}/clarifications` | GET/POST | ✅ |
| `/uploads` | POST/GET/DELETE | ✅ |
| `/artifacts/{path...}` | GET | ✅ |
| `/suggestions` | POST | ✅ |

**LangGraph API: ⭐⭐⭐⭐⭐ 完整**

---

## 二、Gateway API 路由覆盖 ✅

| 端点 | 方法 | 状态 |
|------|------|------|
| `/api/skills` | GET | ✅ |
| `/api/skills/{name}` | PUT | ✅ |
| `/api/skills/install` | POST | ✅ |
| `/api/agents` | GET/POST | ✅ |
| `/api/agents/{name}` | GET/PUT/DELETE | ✅ |
| `/api/channels` | GET | ✅ |
| `/api/channels/{name}/restart` | POST | ✅ |
| `/api/mcp/config` | GET/PUT | ✅ |
| `/api/memory` | GET/PUT/DELETE | ✅ |
| `/api/user-profile` | GET/PUT | ✅ |
| `/api/tts` | POST | ✅ |
| `/api/models` | GET | ✅ |

**Gateway API: ⭐⭐⭐⭐⭐ 完整**

---

## 三、持久化状态 ✅

- **Skills**: ✅ 已持久化到 `data/gateway_state.json`
- **Agents**: ✅ 已持久化到 `data/gateway_state.json`
- **MCP Config**: ✅ 已持久化到 `data/gateway_state.json`
- **Threads/Messages**: ✅ 内存 + 可选 Postgres
- **Memory**: ✅ 文件存储或 Postgres

---

## 四、认证系统 ✅ (新增)

**文件**: `pkg/langgraphcompat/auth.go`

```go
// 三种模式
YOLO 模式: DEERFLOW_YOLO=1 → 无需认证
无 Token: DEERFLOW_AUTH_TOKEN 未设置 → 跳过认证（向后兼容）
生产模式: DEERFLOW_AUTH_TOKEN=xxx → Bearer token 校验
```

**公开路径（无需认证）**:
- `GET /health`
- `GET /api/skills`（只读）
- `OPTIONS`（CORS 预检）

**行为验证**:
```
无 Authorization header    → 401 {"detail": "missing Authorization header"}
Authorization: Bearer xxx  → 401 {"detail": "invalid token"}
Authorization: Bearer secret → 200 ✅
DEERFLOW_YOLO=1            → 所有路径 200 ✅
```

---

## 五、YOLO 模式 ✅ (新增)

**启动**:
```bash
./deerflow --yolo
# 等价于:
DEERFLOW_YOLO=1 ./deerflow
```

**自动设置**:
- `DEERFLOW_YOLO=1` → 跳过认证
- `ADDR=:8080`
- `DEFAULT_LLM_MODEL=qwen/Qwen3.5-9B`
- `DEERFLOW_DATA_ROOT=./data`

**新增 flag**:
```bash
./deerflow --yolo --auth-token=customtoken --addr=:9000
```

---

## 六、GitHub Actions 发布 ✅ (新增)

**文件**: `.github/workflows/release.yml`

**触发**: Tag push (`v*.*.*`)

**构建产物**:
| 平台 | 架构 | 文件 |
|------|------|------|
| Linux | amd64 | deerflow-linux-amd64 |
| Linux | arm64 | deerflow-linux-arm64 |
| macOS | arm64 | deerflow-darwin-arm64 |

**Docker**: ghcr.io/axeprpr/deerflow-go:latest

**使用方法**:
```bash
git tag v0.1.0
git push origin v0.1.0
# 下载: https://github.com/axeprpr/deerflow-go/releases
```

---

## 七、deerflow-ui 对接评估

### 前端调用路径
- LangGraph API (`NEXT_PUBLIC_LANGGRAPH_BASE_URL`) → `/api/langgraph` ✅
- Backend API (`NEXT_PUBLIC_BACKEND_BASE_URL`) → `/api/*` ✅

### CORS
- `Access-Control-Allow-Origin: *` ✅

### 认证
- deerflow-ui 默认无登录墙 → YOLO 模式完美 ✅
- 如需认证 → `DEERFLOW_AUTH_TOKEN` + 前端传 Bearer header ✅

### 对接配置
```env
# deerflow-ui .env.local
NEXT_PUBLIC_LANGGRAPH_BASE_URL=http://go-backend:8080/api/langgraph
NEXT_PUBLIC_BACKEND_BASE_URL=http://go-backend:8080

# Go 服务
./deerflow --yolo
# 或
DEERFLOW_AUTH_TOKEN=secret ./deerflow --addr=:8080
```

---

## 八、综合评分

| 维度 | 评分 | 状态 |
|------|------|------|
| LangGraph API | ⭐⭐⭐⭐⭐ | 完整实现 |
| Gateway API | ⭐⭐⭐⭐⭐ | 完整实现 |
| 持久化 | ⭐⭐⭐⭐⭐ | gateway_state.json |
| 认证 | ⭐⭐⭐⭐ | 已实现 Bearer token |
| YOLO 模式 | ⭐⭐⭐⭐⭐ | --yolo flag |
| 自动化发布 | ⭐⭐⭐⭐ | GitHub Actions |
| deerflow-ui 对接 | ⭐⭐⭐⭐⭐ | 可直接对接 |
| **综合** | **A-** | **可用** |

---

## 九、使用建议

**立即可用**:
```bash
# 构建（本地）
cd deerflow-go && make build

# 运行 YOLO 模式
SILICONFLOW_API_KEY=sk-xxx ./bin/deerflow --yolo

# GitHub Release
git tag v0.1.0 && git push origin v0.1.0
```

**deerflow-ui 对接**:
1. 设置 `NEXT_PUBLIC_LANGGRAPH_BASE_URL` 和 `NEXT_PUBLIC_BACKEND_BASE_URL`
2. Go 服务和前端同域名或配置 CORS
3. YOLO 模式无需额外配置
