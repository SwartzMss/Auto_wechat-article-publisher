# Auto WeChat Article Publisher

单进程多模块：生成（可多轮评论修订）+ 发布到公众号草稿箱，附带内置 React 前端。

## 功能概览
- **发布模块 (`publisher/`)**：Markdown → HTML（含微信兼容处理）、封面/正文图片上传、草稿创建。
- **生成模块 (`generator/`)**：基于 Spec 生成/修订稿件，支持多轮 Session，使用官方 openai-go 接入大模型（支持 openai / deepseek〔需提供兼容 base_url〕）。
- **Web 前端 (`server/web`)**：React + Vite，表单收集需求、触发生成/修订、Markdown 预览、复制稿件。
- **Web 服务**：`--serve` 启动；静态资源由 Go embed，自带占位 `dist`，可用 `npm run build` 生成正式产物。

## 配置
复制 `config.example.json` 为 `config.json`，填写：
```json
{
  "app_id": "YOUR_APP_ID",
  "app_secret": "YOUR_APP_SECRET",
  "server_addr": ":8080",
  "llm": {
    "provider": "openai",          // openai 或 deepseek（OpenAI 兼容）
    "model": "gpt-4.1-mini",
    "api_key": "YOUR_API_KEY",     // 直接写在配置中
    "base_url": ""                 // deepseek 等兼容网关需填写
  }
}
```
- `server_addr`：Web 服务监听地址；命令行 `--addr` 可覆盖。
- `llm`：生成模块的必填配置。`api_key` 必填；若 `provider=deepseek`，务必填写 `base_url` 为其 OpenAI 兼容端点。

## 运行方式
### 1) CLI 发布（原有能力）
```bash
go run . \
  --config config.json \
  --md samples/article.md \
  --title "测试文章" \
  --cover samples/cover.jpg \
  --author "作者"
```
输出草稿 `media_id`。

### 2) 启动 Web 服务
```bash
go run . --serve --config config.json --addr :8080
# 或使用 config.json 中的 server_addr
```
浏览器访问 `http://localhost:8080`，使用 React 界面创建 Session、评论修订、预览 Markdown。

### 3) 前端开发/构建（可选）
```bash
cd server/web
npm install          # 需要外网或配置 npm 镜像
npm run dev          # Vite 开发模式
npm run build        # 产物输出到 server/web/dist，Go 会自动 embed 最新产物
```
如果不构建，仓库内置的占位 `dist` 也可直接使用。

## 模块结构
- `main.go`：CLI 发布或 `--serve` 启动 Web。
- `publisher/`：发布到公众号草稿箱的完整流程。
- `generator/`：Spec/Draft/Session/Agent/LLM 抽象，MockLLM 用于本地演示。
- `generator/llm_openai.go`：官方 openai-go SDK 封装，需配置并提供 API Key。
- `server/`：HTTP 路由，内存 Session 存储，静态资源嵌入。
- `server/web/`：React + Vite 前端源码与构建产物。

## 开发提示
- 测试：`GOCACHE=/tmp/gocache go test ./...`
- 真实 LLM：已接入官方 `openai-go`。  
  - OpenAI：`provider=openai`，可选 `base_url`（代理/网关）。  
  - DeepSeek：`provider=deepseek`，必须填 `base_url`（其兼容接口）。
- 发布链路：正文本地图片会自动上传并替换为微信可访问 URL；封面先上传获取 `thumb_media_id`。
- 如果公众号开启 IP 白名单，运行机器公网 IP 需加入白名单，否则无法获取 `access_token`。
