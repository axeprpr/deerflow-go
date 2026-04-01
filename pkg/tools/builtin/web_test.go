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
