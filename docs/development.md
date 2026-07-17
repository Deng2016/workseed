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

浏览器访问 <http://127.0.0.1:5173>。Vite 会把 `/api` 请求代理到 <http://127.0.0.1:8080>。

开发后端会从 `8080` 开始自动寻找可用端口。如果 8080 已被占用，Vite 的代理地址不会自动变化；此时应先停止占用 8080 的进程，再启动后端开发服务。

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
- 从 `8080` 开始递增查找第一个可用端口
- 数据固定存储在当前工作目录的 `./data`
- 服务监听成功后自动打开系统默认浏览器
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

种子字段的可选值参见 README 中的[字段约定](../README.md#字段约定)。种子列表接口还会通过 `X-Seed-Count-*` 响应头返回各类型、状态和优先级的总数，供前端在单次请求中展示筛选统计。

## 项目结构

```text
.
├── cmd/workseed/          # 程序入口与自动启动逻辑
├── docs/                  # 项目文档
├── images/                # README 截图
├── internal/api/          # HTTP JSON API
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
