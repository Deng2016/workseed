# Workseed 开发指南

本文档面向需要在本地开发、构建或发布 Workseed 的开发者。产品背景、功能和字段约定请先阅读[项目 README](../README.md)。

## 技术栈

- 后端：Go、`net/http`
- 前端：Vue 3、TypeScript、Vite
- 数据库：SQLite（`modernc.org/sqlite`，无需 CGO）
- 部署方式：Vue SPA 嵌入 Go 可执行文件

## 环境要求

- Go 1.24 或更高版本（`go.mod` 当前指定 Go 1.24.0、工具链 1.24.13）
- Node.js 20.19+ 或 22.12+
- npm

Node.js 只在前端开发和构建时需要。运行已构建的 Workseed 不需要安装 Node.js。

## 本地开发

首次安装前端依赖：

```bash
cd web
npm install
```

在项目根目录启动后端：

```bash
go run ./cmd/workseed
```

在另一个终端启动前端开发服务器：

```bash
cd web
npm run dev
```

浏览器访问 <http://127.0.0.1:5173>。Vite 会把 `/api` 请求代理到 <http://127.0.0.1:8866>。

开发后端会从 `8866` 开始自动寻找可用端口。如果 8866 已被占用，Vite 的代理地址不会自动变化；此时应先停止占用 8866 的进程，再启动后端开发服务。

## 生产构建

在项目根目录执行：

```bash
cd web
npm install
npm run build
cd ..
go build -o workseed ./cmd/workseed
```

`npm run build` 会将前端产物写入 `internal/webui/dist`，随后由 Go 的 `embed` 机制打包进可执行文件。

运行构建结果：

```bash
./workseed
```

默认启动行为：

- 监听地址固定为 `127.0.0.1`，仅允许本机访问
- 从 `8866` 开始递增查找第一个可用端口
- 数据固定存储在当前工作目录的 `./data`
- 服务监听成功后自动打开系统默认浏览器
- 同一用户会话中只允许一个 Workseed 实例运行，重复启动会直接退出
- 无需传入 `--host`、`--port` 或 `--data` 参数

如果浏览器未能自动打开，可根据终端输出的地址手动访问。

## 多平台发布

执行发布脚本可一次生成 Linux、Windows 10 和 Windows 11 的 amd64 发布包：

```bash
./release.sh 0.1.0
```

版本号参数可省略；省略时使用当前 Git 标签或提交号。产物写入 `release/`：

```text
workseed-0.1.0-linux-amd64.tar.gz
workseed-0.1.0-windows10-amd64.zip
workseed-0.1.0-windows11-amd64.zip
SHA256SUMS
```

脚本会依次构建前端、运行 Go 测试、交叉编译三个目标，并生成 SHA-256 校验文件。Windows 10 与 Windows 11 使用相同的 Go 目标平台，但分别输出对应名称的发布包。

Windows ZIP 压缩优先使用 `zip`；没有安装 `zip` 时，脚本会自动使用 Python 3 标准库。

## 数据存储与备份

数据库位于当前工作目录的 `./data/workseed.db`。SQLite 使用 WAL 模式，运行时还可能出现 `workseed.db-wal` 和 `workseed.db-shm`。

建议停止 Workseed 后备份整个数据目录：

```bash
cp -a ./data ./data-backup
```

恢复时同样应先停止服务，再用备份替换数据目录。不要把本地数据库提交到 Git。

## HTTP API

所有接口使用 JSON。错误响应格式为：

```json
{ "error": "错误信息" }
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/projects` | 获取项目列表 |
| `POST` | `/api/projects` | 创建项目 |
| `GET` | `/api/seeds?projectId=1&type=idea&type=todo&status=inbox&status=doing&priority=high&keyword=README&page=1&pageSize=20` | 分页获取并过滤种子（项目可省略，默认每页 20 条） |
| `POST` | `/api/seeds` | 创建种子 |
| `PATCH` | `/api/seeds/{id}` | 更新种子 |
| `DELETE` | `/api/seeds/{id}` | 删除种子 |
| `GET` | `/api/worklogs?startTime=...&endTime=...` | 按完成时间获取工作日志 |
| `GET` | `/api/version` | 获取当前程序版本 |
| MCP | `/mcp` | Agent 使用的 Streamable HTTP MCP 端点 |

种子列表接口的 `projectId` 可省略；省略时返回所有项目中的种子。`page` 默认为 `1`，`pageSize` 默认为 `20`、最大为 `100`。响应体仍为种子数组，分页信息通过 `X-Seed-Page`、`X-Seed-Page-Size`、`X-Seed-Filtered-Total` 和 `X-Seed-Has-More` 响应头返回。

创建项目示例：

```json
{
  "name": "Workseed",
  "description": "拾种项目本身的工作记录"
}
```

创建种子示例：

```json
{
  "projectId": 1,
  "type": "todo",
  "status": "inbox",
  "priority": "middle",
  "title": "完善 README",
  "content": "补充开发、构建和数据备份说明。"
}
```

种子字段的可选值参见 README 中的[字段约定](../README.md#字段约定)。列表接口的 `type`、`status`、`priority` 参数可重复传递以实现多选，也兼容逗号分隔值；参数值为空表示该组不选择任何值。`keyword` 会使用 `LIKE` 同时模糊匹配标题和详细内容，并与其他筛选条件组合。接口还会通过 `X-Seed-Count-*` 响应头返回各类型、状态和优先级的总数，供前端在单次请求中展示筛选统计。

种子列表按 `createdAt` 倒序返回。种子响应中的 `startedAt`、`completedAt` 和 `durationSeconds` 分别表示开始时间、完成时间和耗时（秒）。进入 `doing` 时记录开始时间；进入 `done` 时记录完成时间，且仅在已有开始时间时计算耗时。状态可在 `inbox`、`doing`、`paused`、`skipped`、`done` 之间自由切换。

工作日志接口按 `completedAt` 倒序返回所有项目中已记录完成时间的种子。`startTime` 和 `endTime` 使用 RFC 3339 格式；开始时间包含在结果中，结束时间作为不包含的上界。两个参数均可省略。

## Agent MCP 接入

Workseed 启动后会在同一个本地 HTTP 服务上提供 Streamable HTTP MCP 端点：

```text
http://127.0.0.1:8866/mcp
```

如果 `8866` 已被占用，请使用启动日志中显示的实际端口。服务只监听 `127.0.0.1`，默认没有额外身份验证，适合本机 Agent 使用，不应通过反向代理直接暴露到公网。

在支持远程 MCP 的 Agent 中添加名为 `workseed` 的服务，并将 URL 指向上述端点。常见配置形态如下；具体字段以所用 Agent 的配置格式为准：

```json
{
  "mcpServers": {
    "workseed": {
      "url": "http://127.0.0.1:8866/mcp"
    }
  }
}
```

MCP 服务提供以下工具：

| 工具 | 作用 |
| --- | --- |
| `list_seeds` | 按高、中、低优先级列出事种；默认只返回 `inbox`，可传 `projectId`、`status` 和 `limit`，状态支持 `inbox`、`doing`、`paused`、`skipped`、`done`、`all` |
| `get_seed` | 按 `seedId` 获取最新信息；可传 `claimToken`，通过 `claimedByCaller` 确认所有权，但不会回显服务端保存的令牌 |
| `start_seed` | 使用唯一 `claimToken` 原子领取 `inbox` 事种并改为 `doing`；相同令牌重复调用保持成功，其他令牌不能接管 |
| `complete_seed` | 使用领取时的 `claimToken` 将 `doing` 事种改为 `done`；相同令牌重复调用保持成功 |
| `skip_seed` | 仅当当前状态与 `expectedStatus` 原子匹配时改为 `skipped`；跳过 `doing` 时必须提供领取时的 `claimToken` |

推荐 Agent 工作流：

1. 调用 `list_seeds` 获取事种，选择返回列表中的第一条，并为它生成本次运行唯一且不可复用的 `claimToken`。
2. 条件不完整时调用 `skip_seed`，传入 `expectedStatus: "inbox"`；否则使用 `claimToken` 调用 `start_seed`。
3. 工具调用结果不确定时，将同一 `claimToken` 传给 `get_seed`，同时确认状态和 `claimedByCaller`。
4. 完成工作后使用同一 `claimToken` 调用 `complete_seed`；多次尝试仍失败时调用 `skip_seed`，传入 `expectedStatus: "doing"` 和同一令牌。
5. 重复以上步骤，直到 `list_seeds` 返回空列表。不要记录、输出或跨事种复用 `claimToken`。

版本号由 Go 构建时自动写入的最后一次 Git 提交 ID 和提交时间生成，格式为 `<7位提交ID>_yyyyMMddHHmm`，其中时间使用 UTC，例如 `07b9a39_202607210344`。无法获取 Git 构建信息时版本为 `dev`。

## 项目结构

```text
.
├── cmd/workseed/          # 程序入口与自动启动逻辑
├── docs/                  # 项目文档
├── images/                # README 截图
├── internal/api/          # HTTP JSON API
├── internal/mcpserver/    # Agent MCP 服务与工具
├── internal/store/        # SQLite 初始化与迁移
├── internal/webui/        # 嵌入式前端及构建产物
├── web/                   # Vue 3 + TypeScript 前端源码
├── data/                  # 默认本地数据目录（运行后生成）
├── release.sh             # 多平台发布脚本
├── go.mod
└── README.md
```

## 检查与测试

前端类型检查与生产构建：

```bash
cd web
npm run typecheck
npm run build
```

Go 测试与构建：

```bash
go test ./...
go build -o workseed ./cmd/workseed
```

提交代码前建议运行：

```bash
git diff --check
```
