# 拾种 Workseed

记录工作过程中出现的灵感、功能、事项与 Bug。

## 开发

```bash
# 前端
cd web
npm install
npm run dev

# 后端（另一个终端）
go run ./cmd/workseed --data ./data
```

Vite 开发服务器会将 `/api` 代理到 `http://127.0.0.1:8080`。

## 构建

```bash
cd web
npm install
npm run build
cd ..
go build -o workseed ./cmd/workseed
```

前端构建产物会写入 `internal/webui/dist` 并嵌入 Go 可执行文件。

## 运行

```bash
./workseed --host 127.0.0.1 --port 8080 --data ./data
```

