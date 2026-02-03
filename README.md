# Auto_wechat-article-publisher

自动进行微信公众号文章的发布（当前支持：Markdown 上传到草稿箱）。

## 语言建议
优先使用 Python 或 Go。当前实现为 Go CLI。

## 快速开始

1) 初始化依赖

```bash
go mod tidy
```

2) 准备配置

复制 `config.example.json` 为 `config.json`，填写公众号的 `app_id` 和 `app_secret`。

3) 上传草稿

```bash
go run . \
  --config config.json \
  --md path\to\article.md \
  --title "文章标题" \
  --cover path\to\cover.jpg \
  --author "作者" \
  --digest "摘要"
```

示例（使用 samples 目录）：

```bash
go run . \
  --config config.json \
  --md samples/article.md \
  --title "测试文章" \
  --cover samples/cover.jpg
```

成功后会输出草稿 `media_id`。

## 注意事项
- 草稿接口要求 `thumb_media_id`，此处会先上传封面图片再创建草稿。
- 公众号接口权限与调用次数受微信平台限制。
- 如果开启了 IP 白名单，运行机器的公网 IP 必须加入公众号平台白名单，否则无法获取 `access_token`。
- 文章中的图片需能被微信端访问，后续可扩展为自动上传内容图片并替换链接。
