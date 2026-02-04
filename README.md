# Auto WeChat Article Publisher

面向公众号的文案生成与草稿发布工具，支持一键生成、修订并推送到草稿箱。

## 功能
- 需求驱动的 LLM 生成与多轮修订（OpenAI / DeepSeek 兼容）。
- 实时 Markdown 预览，可手动编辑、复制。
- 一键发布到公众号草稿箱：上传封面/正文图片并转换为微信兼容 HTML。

## 配置
- 运行配置（`config/config.json`，由 `config/config.example.json` 复制）
  - `app_id` / `app_secret`
  - `server_addr`（默认 `:8080`）
  - `llm.provider`（`openai` 或 `deepseek`），`model`，`api_key`；若 `deepseek` 必填 `base_url`
- 部署配置（`config/deploy.env`，由 `config/deploy.env.example` 复制）
  - `DOMAIN`
  - `SSL_CERT_PATH`
  - `SSL_KEY_PATH`
  - 若使用非 443 端口，设置 `HTTPS_PORT`
  - 可选 `LOG_FILE`（默认 `./logs/app.log`，部署脚本会转为绝对路径并创建）
  - 可选 `LOGROTATE_ENABLE`（默认 1，生成每周轮转、保留 8 份的 logrotate 配置）

## 使用
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

## 开发
- 前端：`cd server/web && npm install && npm run dev`；打包用 `npm run build`
- 测试：`GOCACHE=/tmp/gocache go test ./...`
- 公众号如有 IP 白名单，需将运行机公网 IP 加入。
- 日志：部署后可用 `journalctl -u auto-wechat.service -f` 查看；文件日志默认写入 `./logs/app.log`（转换为绝对路径，可在 `config/deploy.env` 中改 `LOG_FILE`），若启用 `LOGROTATE_ENABLE` 将自动生成每周轮转的 logrotate 配置。
