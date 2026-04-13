package harnessruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRemoteWorkerClientSubmit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	client := NewHTTPRemoteWorkerClient(nil)
	body, err := client.Submit(context.Background(), server.URL, []byte(`{"envelope":{}}`))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if strings.TrimSpace(string(body)) != `{"result":"ok"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestHTTPRemoteWorkerClientReturnsBodyOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "worker rejected request", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewHTTPRemoteWorkerClient(nil)
	_, err := client.Submit(context.Background(), server.URL, []byte(`{}`))
	if err == nil || err.Error() != "worker rejected request" {
		t.Fatalf("Submit() error = %v", err)
	}
}
