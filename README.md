# Auto WeChat Article Publisher

单进程应用：生成稿件（可多轮修订）并发布到公众号草稿箱，附带 React 前端与部署脚本。

## 能力
- Markdown → 微信兼容 HTML，上传封面/正文图片，创建草稿。
- 基于 LLM（openai / deepseek 兼容接口）生成与修订稿件。
- 内置前端（Vite + React）用于填写需求、预览、复制。

## 配置（必填）
复制 `config/config.example.json` 为 `config/config.json`，填写：
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
- `server_addr`：Web 服务监听地址；`--addr` 可覆盖。
- `llm.api_key` 必填；`provider=deepseek` 时必须填 `base_url` 为其兼容端点。

## 快速使用
### CLI 发布
```bash
go run . \
  --config config/config.json \
  --md samples/article.md \
  --title "测试文章" \
  --cover samples/cover.jpg \
  --author "作者"
```
输出草稿 `media_id`。

### 启动 Web 服务
```bash
go run . --serve --config config/config.json --addr :8080
# 或使用 config/config.json 中的 server_addr
```
浏览器访问 `http://localhost:8080`，使用 React 界面创建 Session、评论修订、预览 Markdown。

### 前端开发/构建（可选）
```bash
cd server/web
npm install          # 需要外网或配置 npm 镜像
npm run dev          # Vite 开发模式
npm run build        # 产物输出到 server/web/dist，Go 会自动 embed 最新产物
```
如果不构建，仓库内置的占位 `dist` 也可直接使用。

## 脚本
- `scripts/build.sh`：后端编译 +（默认）前端打包。可选变量：`OUTPUT` 自定义二进制路径；`SKIP_WEB=1` 跳过前端；`GOFLAGS` 透传 go 参数。
- `scripts/deploy.sh`：需要 sudo。读取 `config/deploy.env`（可由 `config/deploy.env.example` 复制）和 `config/config.json`，同步前端 dist、写入 nginx + systemd 并启动服务。必填：`DOMAIN`、`SSL_CERT_PATH`、`SSL_KEY_PATH`；可选 `HTTPS_PORT`（默认 443）。若未设置 `BIND_ADDR`，自动使用 `config.json` 的 `server_addr`。

## 其他提示
- 测试：`GOCACHE=/tmp/gocache go test ./...`
- 图片上传会自动替换为微信可访问 URL；封面先上传获取 `thumb_media_id`。
- 公众号如开通 IP 白名单，需将运行机公网 IP 加入。
