# Auto WeChat Article Publisher

面向公众号的文案生成与草稿发布工具，支持一键生成、修订并推送到草稿箱。

## 功能
- Markdown → 微信兼容 HTML，自动上传封面与正文图片，创建草稿。
- LLM 生成/修订稿件（OpenAI 或 DeepSeek 兼容接口）。
- 内置前端：填写需求、查看/复制 Markdown，触发生成与发布。

## 配置
1. 复制 `config/config.example.json` 到 `config/config.json` 并填写：
   - `app_id` / `app_secret`
   - `server_addr`（默认 `:8080`）
   - `llm.provider`（`openai` 或 `deepseek`），`model`，`api_key`；若 `deepseek` 必填 `base_url`
2. 复制 `config/deploy.env.example` 到 `config/deploy.env`，至少填写：
   - `DOMAIN`
   - `SSL_CERT_PATH`
   - `SSL_KEY_PATH`
   - 需要非 443 端口时改 `HTTPS_PORT`

## 使用
### CLI 发布
```bash
go run . --config config/config.json \
  --md samples/article.md \
  --title "测试文章" \
  --cover samples/cover.jpg \
  --author "作者"
```

### 启动 Web 服务
```bash
go run . --serve --config config/config.json --addr :8080
# 可省略 --addr 使用配置中的 server_addr
```
访问 `http://localhost:8080` 使用前端。

## 脚本
- `scripts/build.sh`：构建后端并默认打包前端。可用环境变量：
  - `OUTPUT=./bin/auto-wechat-article-publisher` 自定义二进制
  - `SKIP_WEB=1` 跳过前端打包
  - `GOFLAGS="-trimpath"` 透传 go 参数
- `scripts/deploy.sh`（sudo）：
  - 读取 `config/deploy.env` + `config/config.json`
  - 同步前端 dist，生成 nginx + systemd 配置并启动
  - 必填：`DOMAIN`、`SSL_CERT_PATH`、`SSL_KEY_PATH`
  - 未显式设置 `BIND_ADDR` 时自动取 `config.json` 的 `server_addr`

## 开发
- 前端：`cd server/web && npm install && npm run dev`；打包用 `npm run build`
- 测试：`GOCACHE=/tmp/gocache go test ./...`
- 公众号如有 IP 白名单，需将运行机公网 IP 加入。
