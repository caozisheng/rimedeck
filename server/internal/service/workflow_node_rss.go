package service

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rulego/rulego/api/types"
	"github.com/rulego/rulego/utils/maps"
)

// RssFetchNode fetches and parses an RSS or Atom feed, returning entries as JSON.
type RssFetchNode struct {
	Config RssFetchNodeConfig
}

type RssFetchNodeConfig struct {
	URL     string `json:"url"`
	Limit   int    `json:"limit"`   // max entries to return, 0 = all
	Timeout int    `json:"timeout"` // seconds, default 30
}

func (n *RssFetchNode) Type() string   { return "rssFetch" }
func (n *RssFetchNode) New() types.Node { return &RssFetchNode{} }

func (n *RssFetchNode) Init(_ types.Config, configuration types.Configuration) error {
	return maps.Map2Struct(configuration, &n.Config)
}

func (n *RssFetchNode) Destroy() {}

// feedEntry is the unified output for both RSS <item> and Atom <entry> elements.
type feedEntry struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	PubDate     string `json:"pubDate"`
}

// rssFeed models a subset of RSS 2.0 XML.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

// atomFeed models a subset of Atom XML.
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string   `xml:"title"`
	Link    atomLink `xml:"link"`
	Summary string   `xml:"summary"`
	Updated string   `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

func (n *RssFetchNode) OnMsg(ctx types.RuleContext, msg types.RuleMsg) {
	url := strings.ReplaceAll(n.Config.URL, "{{.data}}", msg.Data.Get())
	slog.Info("rssFetch: starting", "url", url, "config_url", n.Config.URL, "limit", n.Config.Limit)
	if url == "" {
		ctx.TellFailure(msg, fmt.Errorf("rssFetch: url is empty"))
		return
	}

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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		ctx.TellFailure(msg, err)
		return
	}

	entries := n.parse(body)

	if n.Config.Limit > 0 && len(entries) > n.Config.Limit {
		entries = entries[:n.Config.Limit]
	}

	result := map[string]any{
		"url":     url,
		"count":   len(entries),
		"entries": entries,
	}
	resultJSON, _ := json.Marshal(result)
	msg.Data.Set(string(resultJSON))
	ctx.TellSuccess(msg)
}

// parse tries RSS first, then Atom.
func (n *RssFetchNode) parse(data []byte) []feedEntry {
	// Try RSS 2.0
	var rss rssFeed
	if err := xml.Unmarshal(data, &rss); err == nil && len(rss.Channel.Items) > 0 {
		entries := make([]feedEntry, 0, len(rss.Channel.Items))
		for _, item := range rss.Channel.Items {
			entries = append(entries, feedEntry{
				Title:       item.Title,
				Link:        item.Link,
				Description: item.Description,
				PubDate:     item.PubDate,
			})
		}
		return entries
	}

	// Try Atom
	var atom atomFeed
	if err := xml.Unmarshal(data, &atom); err == nil && len(atom.Entries) > 0 {
		entries := make([]feedEntry, 0, len(atom.Entries))
		for _, entry := range atom.Entries {
			entries = append(entries, feedEntry{
				Title:       entry.Title,
				Link:        entry.Link.Href,
				Description: entry.Summary,
				PubDate:     entry.Updated,
			})
		}
		return entries
	}

	return nil
}
