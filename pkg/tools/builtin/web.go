package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

const (
	defaultWebSearchMaxResults = 5
	defaultWebFetchMaxChars    = 4096
	defaultWebUserAgent        = "deerflow-go/0.1 (+https://github.com/axeprpr/deerflow-go)"
)

var (
	webClient               = &http.Client{Timeout: 20 * time.Second}
	duckDuckGoSearchBaseURL = "https://html.duckduckgo.com/html/"

	ddgResultAnchorRE = regexp.MustCompile(`(?is)<a[^>]+(?:class="[^"]*(?:result__a|result-link)[^"]*"|class='[^']*(?:result__a|result-link)[^']*')[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE      = regexp.MustCompile(`(?is)<(?:a|div|span)[^>]+class="[^"]*(?:result__snippet|result-snippet)[^"]*"[^>]*>(.*?)</(?:a|div|span)>`)
	titleTagRE        = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	scriptTagRE       = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleTagRE        = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	blockTagRE        = regexp.MustCompile(`(?is)</?(?:article|aside|blockquote|br|div|h[1-6]|header|footer|li|main|nav|p|pre|section|tr|table|ul|ol)[^>]*>`)
	anyTagRE          = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRE           = regexp.MustCompile(`[ \t\r\f\v]+`)
	blankLineRE       = regexp.MustCompile(`\n{3,}`)
)

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content,omitempty"`
}

func WebSearchHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	_ = ctx

	query, ok := call.Arguments["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("query is required")
	}
	query = strings.TrimSpace(query)

	maxResults := defaultWebSearchMaxResults
	if raw, ok := call.Arguments["max_results"].(float64); ok && raw > 0 {
		maxResults = int(raw)
	}
	if maxResults <= 0 {
		maxResults = defaultWebSearchMaxResults
	}
	if maxResults > 10 {
		maxResults = 10
	}

	results, err := searchDuckDuckGo(query, maxResults)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("web search failed: %w", err)
	}

	body, err := json.Marshal(results)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("encode search results: %w", err)
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  string(body),
	}, nil
}

func WebFetchHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	_ = ctx

	rawURL, ok := call.Arguments["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("url is required")
	}
	rawURL = strings.TrimSpace(rawURL)

	maxChars := defaultWebFetchMaxChars
	if raw, ok := call.Arguments["max_chars"].(float64); ok && raw > 0 {
		maxChars = int(raw)
	}
	if maxChars <= 0 {
		maxChars = defaultWebFetchMaxChars
	}

	content, err := fetchWebPage(rawURL, maxChars)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("web fetch failed: %w", err)
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  content,
	}, nil
}

func WebSearchTool() models.Tool {
	return models.Tool{
		Name:        "web_search",
		Description: "Search the web for current information and return relevant results.",
		Groups:      []string{"builtin", "web"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "Search query"},
				"max_results": map[string]any{"type": "number", "description": "Maximum number of results to return"},
			},
			"required": []any{"query"},
		},
		Handler: WebSearchHandler,
	}
}

func WebFetchTool() models.Tool {
	return models.Tool{
		Name:        "web_fetch",
		Description: "Fetch the contents of a web page URL and return a readable text summary.",
		Groups:      []string{"builtin", "web"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":       map[string]any{"type": "string", "description": "Exact URL to fetch"},
				"max_chars": map[string]any{"type": "number", "description": "Maximum characters to return"},
			},
			"required": []any{"url"},
		},
		Handler: WebFetchHandler,
	}
}

func WebTools() []models.Tool {
	return []models.Tool{
		WebSearchTool(),
		WebFetchTool(),
	}
}

func searchDuckDuckGo(query string, maxResults int) ([]webSearchResult, error) {
	endpoint := duckDuckGoSearchBaseURL + "?q=" + url.QueryEscape(query)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultWebUserAgent)

	resp, err := webClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGoResults(string(body), maxResults), nil
}

func parseDuckDuckGoResults(body string, maxResults int) []webSearchResult {
	anchors := ddgResultAnchorRE.FindAllStringSubmatch(body, -1)
	snippets := ddgSnippetRE.FindAllStringSubmatch(body, -1)
	results := make([]webSearchResult, 0, min(maxResults, len(anchors)))
	seen := make(map[string]struct{}, len(anchors))

	for idx, match := range anchors {
		if len(match) < 3 {
			continue
		}
		link := normalizeDuckDuckGoURL(match[1])
		title := cleanHTMLText(match[2])
		if link == "" || title == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}

		var snippet string
		if idx < len(snippets) && len(snippets[idx]) >= 2 {
			snippet = cleanHTMLText(snippets[idx][1])
		}
		results = append(results, webSearchResult{
			Title:   title,
			URL:     link,
			Content: snippet,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results
}

func fetchWebPage(rawURL string, maxChars int) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}

	req, err := http.NewRequest(http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultWebUserAgent)

	resp, err := webClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return extractReadableContent(parsed.String(), string(body), maxChars), nil
}

func extractReadableContent(pageURL, body string, maxChars int) string {
	title := ""
	if match := titleTagRE.FindStringSubmatch(body); len(match) >= 2 {
		title = cleanHTMLText(match[1])
	}

	text := scriptTagRE.ReplaceAllString(body, " ")
	text = styleTagRE.ReplaceAllString(text, " ")
	text = blockTagRE.ReplaceAllString(text, "\n")
	text = anyTagRE.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)

	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(spaceRE.ReplaceAllString(line, " "))
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
	}
	text = strings.Join(filtered, "\n\n")
	text = blankLineRE.ReplaceAllString(text, "\n\n")

	var b strings.Builder
	if title != "" {
		b.WriteString("# ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	b.WriteString("Source: ")
	b.WriteString(pageURL)
	if text != "" {
		b.WriteString("\n\n")
		b.WriteString(text)
	}

	content := strings.TrimSpace(b.String())
	if maxChars > 0 && len(content) > maxChars {
		content = strings.TrimSpace(content[:maxChars])
	}
	return content
}

func normalizeDuckDuckGoURL(raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if parsed.Path == "/l/" || parsed.Path == "/l" {
		if uddg := parsed.Query().Get("uddg"); uddg != "" {
			decoded, err := url.QueryUnescape(uddg)
			if err == nil {
				return decoded
			}
			return uddg
		}
	}
	return raw
}

func cleanHTMLText(value string) string {
	value = anyTagRE.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.TrimSpace(spaceRE.ReplaceAllString(value, " "))
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
