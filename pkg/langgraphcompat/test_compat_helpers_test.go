package langgraphcompat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axeprpr/deerflow-go/pkg/tools"
)

func performCompatRequest(t *testing.T, handler any, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	switch h := any(handler).(type) {
	case *httptest.Server:
		req, err := http.NewRequest(method, h.URL+target, body)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := h.Client().Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		rec := httptest.NewRecorder()
		rec.Code = resp.StatusCode
		for key, values := range resp.Header {
			for _, value := range values {
				rec.Header().Add(key, value)
			}
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		_, _ = rec.Body.Write(data)
		return rec
	case http.Handler:
		req := httptest.NewRequest(method, target, body)
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	default:
		t.Fatalf("unsupported handler type %T", handler)
		return nil
	}
}

func (s *Server) sessionFiles(session *Session) []tools.PresentFile {
	if s == nil || session == nil {
		return nil
	}
	return s.collectSessionFiles(session)
}
