package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/utils/maps"
)

// WebScrapeNode fetches a URL via HTTP GET and returns the response body.
type WebScrapeNode struct {
	Config WebScrapeNodeConfig
}

type WebScrapeNodeConfig struct {
	URL         string `json:"url"`
	ExtractMode string `json:"extractMode"` // "text" | "html" | "raw"
	Timeout     int    `json:"timeout"`     // seconds, default 30
}

func (n *WebScrapeNode) Type() string   { return "webScrape" }
func (n *WebScrapeNode) New() types.Node { return &WebScrapeNode{} }

func (n *WebScrapeNode) Init(_ types.Config, configuration types.Configuration) error {
	return maps.Map2Struct(configuration, &n.Config)
}

func (n *WebScrapeNode) Destroy() {}

func (n *WebScrapeNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	// Resolve URL from config (support {{.data}} substitution)
	url := strings.ReplaceAll(n.Config.URL, "{{.data}}", msg.Data.Get())
	if url == "" {
		ctx.TellFailure(msg, fmt.Errorf("webScrape: url is empty"))
		return
	}

	// HTTP GET with timeout
	timeout := 10 * time.Second
	if n.Config.Timeout > 0 {
		timeout = time.Duration(n.Config.Timeout) * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}
	defer resp.Body.Close()

	// 5MB limit to prevent unbounded memory usage
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	result := map[string]any{
		"url":          url,
		"status_code":  resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
		"body":         string(body),
		"length":       len(body),
	}
	resultJSON, _ := json.Marshal(result)
	msg.Data.Set(string(resultJSON))
	ctx.TellSuccess(msg)
}
