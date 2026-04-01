package langgraphcompat

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateThreadIDAcceptsDots(t *testing.T) {
	if err := validateThreadID("thread.with.dot"); err != nil {
		t.Fatalf("validateThreadID returned error: %v", err)
	}
}

func TestLangGraphRoutesSupportThreadIDsWithDots(t *testing.T) {
	_, handler := newCompatTestServer(t)

	createResp := performCompatRequest(t, handler, http.MethodPost, "/threads", strings.NewReader(`{"thread_id":"thread.with.dot"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}

	stateResp := performCompatRequest(t, handler, http.MethodGet, "/threads/thread.with.dot/state", nil, nil)
	if stateResp.Code != http.StatusOK {
		t.Fatalf("state status=%d body=%s", stateResp.Code, stateResp.Body.String())
	}

	var state ThreadState
	if err := json.Unmarshal(stateResp.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if got := asString(state.Metadata["thread_id"]); got != "thread.with.dot" {
		t.Fatalf("thread_id=%q want=thread.with.dot", got)
	}
}

func TestGatewayRoutesSupportThreadIDsWithDots(t *testing.T) {
	s, handler := newCompatTestServer(t)
	threadID := "thread.with.dot"

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	uploadResp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{
		"Content-Type": writer.FormDataContentType(),
	})
	if uploadResp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", uploadResp.Code, uploadResp.Body.String())
	}

	if _, err := os.Stat(filepath.Join(s.uploadsDir(threadID), "hello.txt")); err != nil {
		t.Fatalf("expected uploaded file to exist: %v", err)
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	if _, err := os.Stat(s.threadDir(threadID)); err != nil {
		t.Fatalf("expected thread dir preserved, stat err=%v", err)
	}
	if _, err := os.Stat(s.threadRoot(threadID)); !os.IsNotExist(err) {
		t.Fatalf("expected user-data removal, stat err=%v", err)
	}
}
