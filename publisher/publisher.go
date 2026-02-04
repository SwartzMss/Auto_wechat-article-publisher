package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
)

const (
	accessTokenURL = "https://api.weixin.qq.com/cgi-bin/token"
	uploadImageURL = "https://api.weixin.qq.com/cgi-bin/material/add_material"
	uploadImgURL   = "https://api.weixin.qq.com/cgi-bin/media/uploadimg"
	addDraftURL    = "https://api.weixin.qq.com/cgi-bin/draft/add"
)

// Config holds the WeChat app credentials.
type Config struct {
	AppID      string     `json:"app_id"`
	AppSecret  string     `json:"app_secret"`
	LLM        *LLMConfig `json:"llm,omitempty"`
	ServerAddr string     `json:"server_addr,omitempty"`
}

// LLMConfig 预留给生成模块的模型配置（可选，不影响发布流程）。
type LLMConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

// PublishParams describes the content to be published.
type PublishParams struct {
	MarkdownPath string
	Title        string
	CoverPath    string
	Author       string
	Digest       string
}

type accessTokenResp struct {
	AccessToken string `json:"access_token"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

type uploadImageResp struct {
	MediaID string `json:"media_id"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type uploadImgResp struct {
	URL     string `json:"url"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type addDraftResp struct {
	MediaID string `json:"media_id"`
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

type article struct {
	Title              string `json:"title"`
	Author             string `json:"author"`
	Digest             string `json:"digest"`
	Content            string `json:"content"`
	ThumbMediaID       string `json:"thumb_media_id"`
	NeedOpenComment    int    `json:"need_open_comment"`
	OnlyFansCanComment int    `json:"only_fans_can_comment"`
}

type addDraftPayload struct {
	Articles []article `json:"articles"`
}

// Publisher orchestrates conversion and upload to WeChat.
type Publisher struct {
	cfg         Config
	client      *http.Client
	accessToken string
	verbose     bool
	logger      *log.Logger
}

// New creates a Publisher and fetches the access token immediately so it can be reused.
func New(cfg Config, client *http.Client, verbose bool, logger *log.Logger) (*Publisher, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, errors.New("config must include app_id and app_secret")
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if logger == nil {
		logger = log.Default()
	}

	accessToken, err := getAccessToken(client, cfg)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		cfg:         cfg,
		client:      client,
		accessToken: accessToken,
		verbose:     verbose,
		logger:      logger,
	}, nil
}

func (p *Publisher) infof(format string, args ...interface{}) {
	if !p.verbose {
		return
	}
	p.logger.Printf("[INFO] "+format, args...)
}

// LoadConfig reads JSON config from disk.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return Config{}, errors.New("config must include app_id and app_secret")
	}
	return cfg, nil
}

// PublishDraft converts markdown to WeChat-friendly HTML, uploads resources, and creates a draft.
func (p *Publisher) PublishDraft(ctx context.Context, params PublishParams) (string, error) {
	if params.MarkdownPath == "" || params.Title == "" || params.CoverPath == "" {
		return "", errors.New("markdown path, title, and cover path are required")
	}

	mdBytes, err := os.ReadFile(params.MarkdownPath)
	if err != nil {
		return "", err
	}

	finalDigest := params.Digest
	if finalDigest == "" {
		finalDigest = defaultDigest(string(mdBytes), 120)
	}

	mdWithImages, err := replaceMarkdownImages(ctx, p.client, p.accessToken, string(mdBytes), params.MarkdownPath)
	if err != nil {
		return "", err
	}
	p.infof("Processed markdown and uploaded inline images if any")

	contentHTML, err := mdToHTML(mdWithImages)
	if err != nil {
		return "", err
	}
	p.infof("Converted Markdown to HTML")

	contentHTML = normalizeForWeChat(contentHTML)
	p.infof("Normalized HTML for WeChat compatibility")

	thumbMediaID, err := uploadImage(ctx, p.client, p.accessToken, params.CoverPath)
	if err != nil {
		return "", err
	}
	p.infof("Uploaded cover image %s -> media_id=%s", params.CoverPath, thumbMediaID)

	art := article{
		Title:              params.Title,
		Author:             params.Author,
		Digest:             finalDigest,
		Content:            contentHTML,
		ThumbMediaID:       thumbMediaID,
		NeedOpenComment:    0,
		OnlyFansCanComment: 0,
	}

	mediaID, err := addDraft(ctx, p.client, p.accessToken, art)
	if err != nil {
		return "", err
	}
	p.infof("Draft created successfully: media_id=%s", mediaID)

	return mediaID, nil
}

func getAccessToken(client *http.Client, cfg Config) (string, error) {
	req, err := http.NewRequest("GET", accessTokenURL, nil)
	if err != nil {
		return "", err
	}
	q := req.URL.Query()
	q.Set("grant_type", "client_credential")
	q.Set("appid", cfg.AppID)
	q.Set("secret", cfg.AppSecret)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data accessTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.AccessToken == "" {
		return "", fmt.Errorf("failed to get access_token: %d %s", data.ErrCode, data.ErrMsg)
	}
	return data.AccessToken, nil
}

func uploadImage(ctx context.Context, client *http.Client, accessToken, imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", uploadImageURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	q := req.URL.Query()
	q.Set("access_token", accessToken)
	q.Set("type", "image")
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data uploadImageResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.MediaID == "" {
		return "", fmt.Errorf("failed to upload image: %d %s", data.ErrCode, data.ErrMsg)
	}
	return data.MediaID, nil
}

func uploadContentImage(ctx context.Context, client *http.Client, accessToken, imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", uploadImgURL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	q := req.URL.Query()
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data uploadImgResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.URL == "" {
		return "", fmt.Errorf("failed to upload content image: %d %s", data.ErrCode, data.ErrMsg)
	}
	return data.URL, nil
}

func mdToHTML(md string) (string, error) {
	var buf bytes.Buffer
	if err := goldmark.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// WeChat 会弱化部分列表和标题标签，导致有序列表合并、标题样式丢失。
// 这里在上传前把列表展开、把标题转成带字号的段落，让排版更稳定。
func flattenListsForWeChat(html string) string {
	olRe := regexp.MustCompile(`(?s)<ol[^>]*>(.*?)</ol>`)
	liRe := regexp.MustCompile(`(?s)<li[^>]*>(.*?)</li>`)

	html = olRe.ReplaceAllStringFunc(html, func(block string) string {
		items := liRe.FindAllStringSubmatch(block, -1)
		if len(items) == 0 {
			return block
		}
		var b strings.Builder
		for i, item := range items {
			text := strings.TrimSpace(item[1])
			b.WriteString("<p>")
			b.WriteString(fmt.Sprintf("%d. %s", i+1, text))
			b.WriteString("</p>")
		}
		return b.String()
	})

	ulRe := regexp.MustCompile(`(?s)<ul[^>]*>(.*?)</ul>`)
	html = ulRe.ReplaceAllStringFunc(html, func(block string) string {
		items := liRe.FindAllStringSubmatch(block, -1)
		if len(items) == 0 {
			return block
		}
		var b strings.Builder
		for _, item := range items {
			text := strings.TrimSpace(item[1])
			b.WriteString("<p>• ")
			b.WriteString(text)
			b.WriteString("</p>")
		}
		return b.String()
	})

	return html
}

func convertHeadingsForWeChat(html string) string {
	hRe := regexp.MustCompile(`(?s)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	sizes := map[string]string{
		"1": "24px",
		"2": "22px",
		"3": "20px",
		"4": "18px",
		"5": "16px",
		"6": "15px",
	}

	return hRe.ReplaceAllStringFunc(html, func(block string) string {
		parts := hRe.FindStringSubmatch(block)
		if len(parts) != 3 {
			return block
		}
		size := sizes[parts[1]]
		if size == "" {
			size = "18px"
		}
		text := strings.TrimSpace(parts[2])
		return fmt.Sprintf(`<p style="font-size:%s;font-weight:700;margin:1em 0 0.6em;">%s</p>`, size, text)
	})
}

func normalizeForWeChat(html string) string {
	html = convertHeadingsForWeChat(html)
	html = flattenListsForWeChat(html)
	return html
}

func replaceMarkdownImages(ctx context.Context, client *http.Client, accessToken, md string, mdPath string) (string, error) {
	imgPattern := regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	matches := imgPattern.FindAllStringSubmatchIndex(md, -1)
	if len(matches) == 0 {
		return md, nil
	}

	baseDir := filepath.Dir(mdPath)
	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[2]
		end := match[3]
		builder.WriteString(md[last:start])
		imgRef := strings.TrimSpace(md[start:end])
		if strings.HasPrefix(imgRef, "http://") || strings.HasPrefix(imgRef, "https://") {
			builder.WriteString(imgRef)
			last = end
			continue
		}
		if strings.HasPrefix(imgRef, "data:") {
			builder.WriteString(imgRef)
			last = end
			continue
		}
		localPath := imgRef
		if !filepath.IsAbs(localPath) {
			if _, statErr := os.Stat(localPath); statErr != nil {
				localPath = filepath.Join(baseDir, imgRef)
			}
		}
		uploadedURL, err := uploadContentImage(ctx, client, accessToken, localPath)
		if err != nil {
			return "", err
		}
		builder.WriteString(uploadedURL)
		last = end
	}
	builder.WriteString(md[last:])
	return builder.String(), nil
}

func defaultDigest(md string, limit int) string {
	compact := strings.Fields(md)
	joined := strings.Join(compact, " ")
	if len(joined) <= limit {
		return joined
	}
	return joined[:limit]
}

func addDraft(ctx context.Context, client *http.Client, accessToken string, art article) (string, error) {
	payload := addDraftPayload{Articles: []article{art}}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", addDraftURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	q := req.URL.Query()
	q.Set("access_token", accessToken)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data addDraftResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.MediaID == "" {
		return "", fmt.Errorf("failed to add draft: %d %s", data.ErrCode, data.ErrMsg)
	}
	return data.MediaID, nil
}
