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
	duckDuckGoPageBaseURL   = "https://duckduckgo.com/"
	duckDuckGoImageAPIURL   = "https://duckduckgo.com/i.js"

	ddgResultAnchorRE = regexp.MustCompile(`(?is)<a[^>]+(?:class="[^"]*(?:result__a|result-link)[^"]*"|class='[^']*(?:result__a|result-link)[^']*')[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE      = regexp.MustCompile(`(?is)<(?:a|div|span)[^>]+class="[^"]*(?:result__snippet|result-snippet)[^"]*"[^>]*>(.*?)</(?:a|div|span)>`)
	ddgImageVQDREs    = []*regexp.Regexp{
		regexp.MustCompile(`vqd=([\w-]+)[&']`),
		regexp.MustCompile(`"vqd":"([^"]+)"`),
		regexp.MustCompile(`vqd='([^']+)'`),
		regexp.MustCompile(`vqd="([^"]+)"`),
	}
	titleTagRE   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	articleTagRE = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</article>`)
	mainTagRE    = regexp.MustCompile(`(?is)<main\b[^>]*>(.*?)</main>`)
	bodyTagRE    = regexp.MustCompile(`(?is)<body\b[^>]*>(.*?)</body>`)
	scriptTagRE  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleTagRE   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	noiseTagRE   = regexp.MustCompile(`(?is)<(?:aside|footer|form|nav|noscript|script|style|svg)[^>]*>.*?</(?:aside|footer|form|nav|noscript|script|style|svg)>`)
	blockTagRE   = regexp.MustCompile(`(?is)</?(?:article|aside|blockquote|br|div|h[1-6]|header|footer|li|main|nav|p|pre|section|tr|table|ul|ol)[^>]*>`)
	anyTagRE     = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRE      = regexp.MustCompile(`[ \t\r\f\v]+`)
	blankLineRE  = regexp.MustCompile(`\n{3,}`)
)

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Content string `json:"content,omitempty"`
}

type webSearchResponse struct {
	Query        string            `json:"query"`
	TotalResults int               `json:"total_results"`
	Results      []webSearchResult `json:"results"`
}

type imageSearchResult struct {
	Title        string `json:"title"`
	SourceURL    string `json:"source_url"`
	ImageURL     string `json:"image_url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

type imageSearchResponse struct {
	Query        string              `json:"query"`
	TotalResults int                 `json:"total_results"`
	Results      []imageSearchResult `json:"results"`
	UsageHint    string              `json:"usage_hint,omitempty"`
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

	body, err := json.Marshal(webSearchResponse{
		Query:        query,
		TotalResults: len(results),
		Results:      results,
	})
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

func ImageSearchHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
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

	size := optionalStringArg(call.Arguments, "size")
	imageType := optionalStringArg(call.Arguments, "type_image")
	layout := optionalStringArg(call.Arguments, "layout")

	results, err := searchDuckDuckGoImages(query, maxResults, size, imageType, layout)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("image search failed: %w", err)
	}

	body, err := json.Marshal(imageSearchResponse{
		Query:        query,
		TotalResults: len(results),
		Results:      results,
		UsageHint:    "Use the image_url values as visual references before generating images.",
	})
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("encode image results: %w", err)
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Status:   models.CallStatusCompleted,
		Content:  string(body),
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

func ImageSearchTool() models.Tool {
	return models.Tool{
		Name:        "image_search",
		Description: "Search for reference images online and return image URLs plus thumbnails.",
		Groups:      []string{"builtin", "web"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "Search keywords for the desired images"},
				"max_results": map[string]any{"type": "number", "description": "Maximum number of images to return"},
				"size":        map[string]any{"type": "string", "description": "Optional size filter such as Small, Medium, Large, or Wallpaper"},
				"type_image":  map[string]any{"type": "string", "description": "Optional image type filter such as photo, clipart, gif, transparent, or line"},
				"layout":      map[string]any{"type": "string", "description": "Optional layout filter such as Square, Tall, or Wide"},
			},
			"required": []any{"query"},
		},
		Handler: ImageSearchHandler,
	}
}

func WebTools() []models.Tool {
	return []models.Tool{
		WebSearchTool(),
		WebFetchTool(),
		ImageSearchTool(),
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
			Snippet: snippet,
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

func searchDuckDuckGoImages(query string, maxResults int, size, imageType, layout string) ([]imageSearchResult, error) {
	vqd, err := fetchDuckDuckGoImageToken(query)
	if err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(duckDuckGoImageAPIURL)
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("o", "json")
	params.Set("l", "wt-wt")
	params.Set("p", "1")
	params.Set("vqd", vqd)
	if filters := duckDuckGoImageFilters(size, imageType, layout); filters != "" {
		params.Set("f", filters)
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultWebUserAgent)
	req.Header.Set("Referer", duckDuckGoPageBaseURL)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := webClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Title     string `json:"title"`
			Image     string `json:"image"`
			Thumbnail string `json:"thumbnail"`
			URL       string `json:"url"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	results := make([]imageSearchResult, 0, min(maxResults, len(payload.Results)))
	seen := make(map[string]struct{}, len(payload.Results))
	for _, item := range payload.Results {
		imageURL := strings.TrimSpace(item.Image)
		if imageURL == "" {
			imageURL = strings.TrimSpace(item.Thumbnail)
		}
		if imageURL == "" {
			continue
		}
		if _, ok := seen[imageURL]; ok {
			continue
		}
		seen[imageURL] = struct{}{}
		results = append(results, imageSearchResult{
			Title:        cleanHTMLText(item.Title),
			SourceURL:    strings.TrimSpace(item.URL),
			ImageURL:     imageURL,
			ThumbnailURL: firstNonEmptyString(strings.TrimSpace(item.Thumbnail), imageURL),
			Width:        item.Width,
			Height:       item.Height,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func fetchDuckDuckGoImageToken(query string) (string, error) {
	endpoint, err := url.Parse(duckDuckGoPageBaseURL)
	if err != nil {
		return "", err
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("iax", "images")
	params.Set("ia", "images")
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	vqd := extractDuckDuckGoImageToken(string(body))
	if vqd == "" {
		return "", fmt.Errorf("image search token not found")
	}
	return vqd, nil
}

func extractDuckDuckGoImageToken(body string) string {
	for _, re := range ddgImageVQDREs {
		match := re.FindStringSubmatch(body)
		if len(match) >= 2 && strings.TrimSpace(match[1]) != "" {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func duckDuckGoImageFilters(size, imageType, layout string) string {
	parts := make([]string, 0, 3)
	if value := strings.TrimSpace(size); value != "" {
		parts = append(parts, "size:"+strings.ToLower(value))
	}
	if value := strings.TrimSpace(imageType); value != "" {
		parts = append(parts, "type:"+strings.ToLower(value))
	}
	if value := strings.TrimSpace(layout); value != "" {
		parts = append(parts, "layout:"+strings.ToLower(value))
	}
	return strings.Join(parts, ",")
}

func optionalStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractReadableContent(pageURL, body string, maxChars int) string {
	title := ""
	if match := titleTagRE.FindStringSubmatch(body); len(match) >= 2 {
		title = cleanHTMLText(match[1])
	}

	text := extractPrimaryContent(body)
	text = scriptTagRE.ReplaceAllString(text, " ")
	text = styleTagRE.ReplaceAllString(text, " ")
	text = noiseTagRE.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "</li>", "\n")
	text = strings.ReplaceAll(text, "<li", "\n<li")
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

func extractPrimaryContent(body string) string {
	for _, re := range []*regexp.Regexp{articleTagRE, mainTagRE, bodyTagRE} {
		if match := re.FindStringSubmatch(body); len(match) >= 2 && strings.TrimSpace(match[1]) != "" {
			return match[1]
		}
	}
	return body
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
