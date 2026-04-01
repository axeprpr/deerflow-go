package builtin

import (
	"context"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func stubHTTPClient(t *testing.T, fn roundTripFunc) func() {
	t.Helper()
	oldClient := webClient
	webClient = &http.Client{Transport: fn}
	return func() {
		webClient = oldClient
	}
}

func newHTTPResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header(textproto.MIMEHeader{"Content-Type": {"text/html; charset=utf-8"}}),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestParseDuckDuckGoResults(t *testing.T) {
	body := `
<html><body>
  <a class="result__a" href="https://example.com/alpha">Alpha Result</a>
  <a class="result__snippet">First snippet</a>
  <a class="result-link" href="https://example.com/beta">Beta Result</a>
  <div class="result-snippet">Second snippet</div>
</body></html>`

	results := parseDuckDuckGoResults(body, 5)
	if len(results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(results))
	}
	if results[0].Title != "Alpha Result" || results[0].URL != "https://example.com/alpha" {
		t.Fatalf("first result=%#v", results[0])
	}
	if results[0].Content != "First snippet" {
		t.Fatalf("first snippet=%q want %q", results[0].Content, "First snippet")
	}
	if results[1].Title != "Beta Result" || results[1].Content != "Second snippet" {
		t.Fatalf("second result=%#v", results[1])
	}
}

func TestWebSearchHandler(t *testing.T) {
	restore := stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("q"); got != "golang" {
			t.Fatalf("query=%q want %q", got, "golang")
		}
		return newHTTPResponse(r, `
<a class="result__a" href="https://example.com/one">Result One</a>
<a class="result__snippet">Snippet One</a>
<a class="result__a" href="https://example.com/two">Result Two</a>
<a class="result__snippet">Snippet Two</a>`), nil
	})
	defer restore()

	oldBaseURL := duckDuckGoSearchBaseURL
	duckDuckGoSearchBaseURL = "https://search.local/html/"
	defer func() { duckDuckGoSearchBaseURL = oldBaseURL }()

	result, err := WebSearchHandler(context.Background(), models.ToolCall{
		ID:   "call-web-search-1",
		Name: "web_search",
		Arguments: map[string]any{
			"query":       "golang",
			"max_results": float64(2),
		},
	})
	if err != nil {
		t.Fatalf("WebSearchHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, `"title":"Result One"`) || !strings.Contains(result.Content, `"title":"Result Two"`) {
		t.Fatalf("content=%q missing result", result.Content)
	}
}

func TestWebFetchHandler(t *testing.T) {
	restore := stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return newHTTPResponse(r, `<!doctype html>
<html>
  <head>
    <title>Sample Page</title>
    <style>.hidden { display: none; }</style>
  </head>
  <body>
    <article>
      <h1>Headline</h1>
      <p>Important content.</p>
      <script>console.log("ignore");</script>
    </article>
  </body>
</html>`), nil
	})
	defer restore()

	result, err := WebFetchHandler(context.Background(), models.ToolCall{
		ID:   "call-web-fetch-1",
		Name: "web_fetch",
		Arguments: map[string]any{
			"url":       "https://example.com/article",
			"max_chars": float64(200),
		},
	})
	if err != nil {
		t.Fatalf("WebFetchHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, "# Sample Page") {
		t.Fatalf("content=%q missing title", result.Content)
	}
	if !strings.Contains(result.Content, "Important content.") {
		t.Fatalf("content=%q missing article text", result.Content)
	}
	if strings.Contains(result.Content, "console.log") {
		t.Fatalf("content=%q should not include script", result.Content)
	}
}

func TestSearchDuckDuckGoReturnsParsedResults(t *testing.T) {
	restore := stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		return newHTTPResponse(r, `<a class="result__a" href="https://example.com/article">Example Article</a><a class="result__snippet">Useful summary</a>`), nil
	})
	defer restore()

	oldBaseURL := duckDuckGoSearchBaseURL
	duckDuckGoSearchBaseURL = "https://search.local/html/"
	defer func() { duckDuckGoSearchBaseURL = oldBaseURL }()

	results, err := searchDuckDuckGo("test", 5)
	if err != nil {
		t.Fatalf("searchDuckDuckGo() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].URL != "https://example.com/article" {
		t.Fatalf("url=%q want %q", results[0].URL, "https://example.com/article")
	}
}

func TestWebSearchUsesUserAgent(t *testing.T) {
	restore := stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("User-Agent"); got != defaultWebUserAgent {
			t.Fatalf("user-agent=%q want %q", got, defaultWebUserAgent)
		}
		return newHTTPResponse(r, `<a class="result__a" href="https://example.com/x">X</a>`), nil
	})
	defer restore()

	oldBaseURL := duckDuckGoSearchBaseURL
	duckDuckGoSearchBaseURL = "https://search.local/html/"
	defer func() { duckDuckGoSearchBaseURL = oldBaseURL }()

	if _, err := searchDuckDuckGo("query", 1); err != nil {
		t.Fatalf("searchDuckDuckGo() error = %v", err)
	}
}

func TestExtractDuckDuckGoImageToken(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "json field",
			body: `{"vqd":"3-12345678901234567890123456789012"}`,
			want: "3-12345678901234567890123456789012",
		},
		{
			name: "query parameter",
			body: `https://duckduckgo.com/i.js?vqd=4-abcdef&foo=bar`,
			want: "4-abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractDuckDuckGoImageToken(tt.body); got != tt.want {
				t.Fatalf("extractDuckDuckGoImageToken()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestImageSearchHandler(t *testing.T) {
	restore := stubHTTPClient(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(r.URL.String(), duckDuckGoImageAPIURL):
			if got := r.URL.Query().Get("vqd"); got != "3-abc123" {
				t.Fatalf("image token=%q want %q", got, "3-abc123")
			}
			if got := r.URL.Query().Get("f"); got != "size:large,type:photo,layout:wide" {
				t.Fatalf("filters=%q want %q", got, "size:large,type:photo,layout:wide")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header(textproto.MIMEHeader{"Content-Type": {"application/json"}}),
				Body: io.NopCloser(strings.NewReader(`{
					"results": [
						{
							"title": "Retro Robot",
							"image": "https://img.example.com/full.jpg",
							"thumbnail": "https://img.example.com/thumb.jpg",
							"url": "https://source.example.com/robot",
							"width": 1024,
							"height": 768
						}
					]
				}`)),
				Request: r,
			}, nil
		case strings.HasPrefix(r.URL.String(), duckDuckGoPageBaseURL):
			if got := r.URL.Query().Get("q"); got != "retro robot illustration" {
				t.Fatalf("page query=%q want %q", got, "retro robot illustration")
			}
			return newHTTPResponse(r, `<html><script>var cfg={"vqd":"3-abc123"}</script></html>`), nil
		default:
			t.Fatalf("unexpected request URL %q", r.URL.String())
			return nil, nil
		}
	})
	defer restore()

	result, err := ImageSearchHandler(context.Background(), models.ToolCall{
		ID:   "call-image-search-1",
		Name: "image_search",
		Arguments: map[string]any{
			"query":       "retro robot illustration",
			"max_results": float64(1),
			"size":        "Large",
			"type_image":  "photo",
			"layout":      "Wide",
		},
	})
	if err != nil {
		t.Fatalf("ImageSearchHandler() error = %v", err)
	}
	if !strings.Contains(result.Content, `"query":"retro robot illustration"`) {
		t.Fatalf("content=%q missing query", result.Content)
	}
	if !strings.Contains(result.Content, `"image_url":"https://img.example.com/full.jpg"`) {
		t.Fatalf("content=%q missing image_url", result.Content)
	}
	if !strings.Contains(result.Content, `"thumbnail_url":"https://img.example.com/thumb.jpg"`) {
		t.Fatalf("content=%q missing thumbnail_url", result.Content)
	}
	if !strings.Contains(result.Content, `"source_url":"https://source.example.com/robot"`) {
		t.Fatalf("content=%q missing source_url", result.Content)
	}
}
