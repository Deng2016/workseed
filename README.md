# 拾种 Workseed

拾种（Workseed）是一款面向个人的轻量记录工具，用来快速捕捉突然出现的待办事项或灵感，避免它们在片刻之后被遗忘。

## 项目背景与定位

在使用 Workseed 之前，这些零散的念头通常记录在本地记事本中。随着内容不断积累，纯文本记录逐渐变得难以浏览、筛选和查找。Workseed 因此而生：它保留记事本随手记录的轻便，同时通过项目、类型、状态和优先级，让个人记录更有条理，也更容易回顾。

Workseed 只专注于个人使用，不提供多人协作、权限管理、任务指派、进度跟踪等团队能力。团队协作或完整的项目管理并不属于本工具的适用范围，请选择专业的项目管理工具。

项目采用 Go + Vue 3 + TypeScript + Vite + SQLite。前端构建产物会嵌入 Go 二进制，部署时只需要一个可执行文件和一个数据目录，直接双击运行。

## 功能

- 创建和切换项目，项目名称全局唯一（忽略大小写）
- 新增、编辑、查看和删除种子
- 在列表中直接修改种子的类型、状态和优先级
- 按类型、状态和优先级组合过滤，并实时显示结果数量

## 字段约定

### 类型

| 中文 | 英文值 | 用途 |
| --- | --- | --- |
| 灵感 | `idea` | 尚未确定是否实施的想法 |
| 功能 | `feature` | 明确的功能需求或改进 |
| 事项 | `todo` | 需要处理的具体工作，默认值 |
| 缺陷 | `bug` | 需要修复的问题 |

### 状态

| 中文 | 英文值 | 说明 |
| --- | --- | --- |
| 待实现 | `inbox` | 尚未完成，默认值 |
| 已完成 | `done` | 已完成；首次进入该状态时记录完成时间 |

### 优先级

| 中文 | 英文值 |
| --- | --- |
| 高 | `high` |
| 中 | `middle` |
| 低 | `low` |

优先级默认为“中”。列表默认先显示待实现的种子，再按高、中、低排序；同级按最近更新时间倒序排列。页面首次打开时，类型默认选择“全部类型”，状态默认选择“待实现”，优先级默认选择“全部优先级”。

## 环境要求

- Go 1.24 或更高版本（`go.mod` 当前指定 Go 1.24.0、工具链 1.24.13）
- Node.js 20.19+ 或 22.12+
- npm

Node.js 只在前端开发和构建时需要。运行已构建的 Workseed 不需要安装 Node.js。

## 开发运行

首次安装前端依赖：

```bash
cd web
npm install
```

启动后端：

```bash
go run ./cmd/workseed
```

在另一个终端启动前端开发服务器：

```bash
cd web
npm run dev
```

浏览器访问 <http://127.0.0.1:5173>。Vite 会把 `/api` 请求代理到 <http://127.0.0.1:8080>。

## 构建与运行

在项目根目录执行：

```bash
cd web
npm install
npm run build
cd ..
go build -o workseed ./cmd/workseed
```

`npm run build` 会将前端产物写入 `internal/webui/dist`，随后由 Go 的 `embed` 机制打包进可执行文件。

启动生产构建：

```bash
./workseed
```

程序会从 `8080` 开始自动查找可用端口，并在启动成功后打开 `http://127.0.0.1:{port}`。如果浏览器未能自动打开，可根据终端输出的地址手动访问。

### 默认启动行为

- 监听地址固定为 `127.0.0.1`，仅允许本机访问
- 从 `8080` 开始递增查找第一个可用端口
- 数据固定存储在当前工作目录的 `./data`
- 服务监听成功后自动打开系统默认浏览器
- 无需传入 `--host`、`--port` 或 `--data` 参数

## 数据存储与备份

数据库位于当前工作目录的 `./data/workseed.db`。SQLite 使用 WAL 模式，运行时还可能出现 `workseed.db-wal` 和 `workseed.db-shm`。

建议停止 Workseed 后备份整个数据目录：

```bash
cp -a ./data ./data-backup
```

恢复时同样应先停止服务，再用备份替换数据目录。请不要把本地数据库提交到 Git。

## API

所有接口使用 JSON。错误响应格式为：

```json
{ "error": "错误信息" }
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/projects` | 获取项目列表 |
| `POST` | `/api/projects` | 创建项目 |
| `GET` | `/api/seeds?projectId=1&type=all&status=inbox&priority=all` | 获取并过滤种子 |
| `POST` | `/api/seeds` | 创建种子 |
| `PATCH` | `/api/seeds/{id}` | 更新种子 |
| `DELETE` | `/api/seeds/{id}` | 删除种子 |

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

## 项目结构

```text
.
├── cmd/workseed/          # 程序入口与自动启动逻辑
├── internal/api/          # HTTP JSON API
├── internal/store/        # SQLite 初始化与迁移
├── internal/webui/        # 嵌入式前端及构建产物
├── web/                   # Vue 3 + TypeScript 前端源码
├── data/                  # 默认本地数据目录（运行后生成）
├── go.mod
└── README.md
```

## 检查与测试

```bash
# 前端类型检查
cd web
npm run typecheck

# 前端生产构建
npm run build

# Go 测试与构建
cd ..
go test ./...
go build -o workseed ./cmd/workseed
```

## 使用建议

- 标题写结论或动作，详细内容记录背景、约束和验收方式。
- 尚不明确的念头使用“灵感”，明确要做的工作再调整为“功能”或“事项”。
- “下世纪”不再作为状态；无限延后的种子可设为低优先级，确认无价值后直接删除。
- 当前服务固定监听本机地址，没有内置账号和鉴权。暴露到局域网或公网前，请自行增加访问控制与 HTTPS。
