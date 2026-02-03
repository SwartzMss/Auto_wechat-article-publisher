package main

import (
    "bytes"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "io"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/yuin/goldmark"
)

const (
    accessTokenURL = "https://api.weixin.qq.com/cgi-bin/token"
    uploadImageURL = "https://api.weixin.qq.com/cgi-bin/material/add_material"
    addDraftURL    = "https://api.weixin.qq.com/cgi-bin/draft/add"
)

type config struct {
    AppID     string `json:"app_id"`
    AppSecret string `json:"app_secret"`
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

func loadConfig(path string) (config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return config{}, err
    }
    var cfg config
    if err := json.Unmarshal(data, &cfg); err != nil {
        return config{}, err
    }
    if cfg.AppID == "" || cfg.AppSecret == "" {
        return config{}, errors.New("config must include app_id and app_secret")
    }
    return cfg, nil
}

func getAccessToken(client *http.Client, cfg config) (string, error) {
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

func uploadImage(client *http.Client, accessToken, imagePath string) (string, error) {
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

    req, err := http.NewRequest("POST", uploadImageURL, &body)
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

func mdToHTML(md string) (string, error) {
    var buf bytes.Buffer
    if err := goldmark.Convert([]byte(md), &buf); err != nil {
        return "", err
    }
    return buf.String(), nil
}

func defaultDigest(md string, limit int) string {
    compact := strings.Fields(md)
    joined := strings.Join(compact, " ")
    if len(joined) <= limit {
        return joined
    }
    return joined[:limit]
}

func addDraft(client *http.Client, accessToken string, art article) (string, error) {
    payload := addDraftPayload{Articles: []article{art}}
    body, err := json.Marshal(payload)
    if err != nil {
        return "", err
    }

    req, err := http.NewRequest("POST", addDraftURL, bytes.NewReader(body))
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

func main() {
    configPath := flag.String("config", "config.json", "path to config.json")
    mdPath := flag.String("md", "", "path to markdown file")
    title := flag.String("title", "", "article title")
    cover := flag.String("cover", "", "path to cover image")
    author := flag.String("author", "", "author name")
    digest := flag.String("digest", "", "article digest")
    flag.Parse()

    if *mdPath == "" || *title == "" || *cover == "" {
        fmt.Fprintln(os.Stderr, "--md, --title, and --cover are required")
        os.Exit(1)
    }

    cfg, err := loadConfig(*configPath)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    mdBytes, err := os.ReadFile(*mdPath)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    contentHTML, err := mdToHTML(string(mdBytes))
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    finalDigest := *digest
    if finalDigest == "" {
        finalDigest = defaultDigest(string(mdBytes), 120)
    }

    client := &http.Client{Timeout: 60 * time.Second}
    accessToken, err := getAccessToken(client, cfg)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    thumbMediaID, err := uploadImage(client, accessToken, *cover)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    art := article{
        Title:              *title,
        Author:             *author,
        Digest:             finalDigest,
        Content:            contentHTML,
        ThumbMediaID:       thumbMediaID,
        NeedOpenComment:    0,
        OnlyFansCanComment: 0,
    }

    mediaID, err := addDraft(client, accessToken, art)
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    fmt.Println(mediaID)
}