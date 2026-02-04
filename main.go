package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"auto_wechat_article_publisher/generator"
	"auto_wechat_article_publisher/publisher"
	"auto_wechat_article_publisher/server"
)

var verbose bool

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	configPath := flag.String("config", "config/config.json", "path to config.json")
	mdPath := flag.String("md", "", "path to markdown file")
	title := flag.String("title", "", "article title")
	cover := flag.String("cover", "", "path to cover image")
	author := flag.String("author", "", "author name")
	digest := flag.String("digest", "", "article digest")
	serve := flag.Bool("serve", false, "start web server")
	addr := flag.String("addr", "", "http listen address when --serve (overrides config.server_addr)")
	flag.BoolVar(&verbose, "v", false, "enable info logs")
	flag.Parse()

	// Web server mode
	if *serve {
		cfg, err := publisher.LoadConfig(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		llm, err := buildLLM(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		agent, err := generator.NewAgent(llm)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		srv, err := server.New(agent, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		listen := cfg.ServerAddr
		if *addr != "" {
			listen = *addr
		}
		if listen == "" {
			listen = ":8080"
		}
		log.Printf("Starting web server on %s", listen)
		if err := http.ListenAndServe(listen, srv.Routes()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if *mdPath == "" || *title == "" || *cover == "" {
		fmt.Fprintln(os.Stderr, "--md, --title, and --cover are required")
		os.Exit(1)
	}

	cfg, err := publisher.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	p, err := publisher.New(cfg, nil, verbose, log.Default())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	params := publisher.PublishParams{
		MarkdownPath: *mdPath,
		Title:        *title,
		CoverPath:    *cover,
		Author:       *author,
		Digest:       *digest,
	}

	ctx := context.Background()
	log.Printf("[cli] publishing title=%q md=%s cover=%s", params.Title, params.MarkdownPath, params.CoverPath)
	mediaID, err := p.PublishDraft(ctx, params)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	log.Printf("[cli] publish done media_id=%s", mediaID)
	fmt.Println(mediaID)
}

func buildLLM(cfg publisher.Config) (generator.LLMClient, error) {
	if cfg.LLM == nil || cfg.LLM.Provider == "" {
		return nil, fmt.Errorf("llm config missing; please set llm.provider/model/api_key_env in config")
	}
	switch cfg.LLM.Provider {
	case "openai":
		return generator.NewOpenAILLMFromConfig(&generator.LLMSettings{
			Provider: cfg.LLM.Provider,
			Model:    cfg.LLM.Model,
			APIKey:   cfg.LLM.APIKey,
			BaseURL:  cfg.LLM.BaseURL,
		})
	case "deepseek":
		// DeepSeek 提供 OpenAI 兼容接口，需填写 base_url（例如官方/网关地址）。
		if cfg.LLM.BaseURL == "" {
			return nil, fmt.Errorf("llm provider deepseek requires base_url (OpenAI-compatible endpoint)")
		}
		return generator.NewOpenAILLMFromConfig(&generator.LLMSettings{
			Provider: cfg.LLM.Provider,
			Model:    cfg.LLM.Model,
			APIKey:   cfg.LLM.APIKey,
			BaseURL:  cfg.LLM.BaseURL,
		})
	default:
		return nil, fmt.Errorf("llm provider %s not supported", cfg.LLM.Provider)
	}
}
