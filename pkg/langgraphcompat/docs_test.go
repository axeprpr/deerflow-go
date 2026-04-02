package langgraphcompat

import (
	"net/http"
	"strings"
	"testing"
)

func TestOfflineDocsPagesRenderWithoutCDN(t *testing.T) {
	_, handler := newCompatTestServer(t)

	docsResp := performCompatRequest(t, handler, http.MethodGet, "/docs", nil, nil)
	if docsResp.Code != http.StatusOK {
		t.Fatalf("docs status=%d", docsResp.Code)
	}
	if got := docsResp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("docs content-type=%q want html", got)
	}
	if !strings.Contains(docsResp.Body.String(), "Open raw OpenAPI schema") {
		t.Fatalf("docs body missing offline docs link: %q", docsResp.Body.String())
	}
	if !strings.Contains(docsResp.Body.String(), "/api/threads/{thread_id}/uploads") {
		t.Fatalf("docs body missing route listing: %q", docsResp.Body.String())
	}
	if strings.Contains(docsResp.Body.String(), "unpkg.com") {
		t.Fatalf("docs body unexpectedly depends on CDN: %q", docsResp.Body.String())
	}

	redocResp := performCompatRequest(t, handler, http.MethodGet, "/redoc", nil, nil)
	if redocResp.Code != http.StatusOK {
		t.Fatalf("redoc status=%d", redocResp.Code)
	}
	if got := redocResp.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("redoc content-type=%q want html", got)
	}
	if !strings.Contains(redocResp.Body.String(), "Offline route index") {
		t.Fatalf("redoc body missing offline description: %q", redocResp.Body.String())
	}
	if strings.Contains(redocResp.Body.String(), "unpkg.com") {
		t.Fatalf("redoc body unexpectedly depends on CDN: %q", redocResp.Body.String())
	}
}
