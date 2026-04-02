package langgraphcompat

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestValidateThreadIDRejectsDots(t *testing.T) {
	err := validateThreadID("thread.with.dot")
	if err == nil {
		t.Fatal("expected validateThreadID to reject dots")
	}
	if !strings.Contains(err.Error(), "invalid thread_id") {
		t.Fatalf("error=%q", err)
	}
}

func TestLangGraphRoutesRejectThreadIDsWithDots(t *testing.T) {
	_, handler := newCompatTestServer(t)

	createResp := performCompatRequest(t, handler, http.MethodPost, "/threads", strings.NewReader(`{"thread_id":"thread.with.dot"}`), map[string]string{
		"Content-Type": "application/json",
	})
	if createResp.Code != http.StatusBadRequest {
		t.Fatalf("create status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	if !strings.Contains(createResp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", createResp.Body.String())
	}
}

func TestGatewayRoutesRejectThreadIDsWithDots(t *testing.T) {
	_, handler := newCompatTestServer(t)
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
	if uploadResp.Code != http.StatusBadRequest {
		t.Fatalf("upload status=%d body=%s", uploadResp.Code, uploadResp.Body.String())
	}
	if !strings.Contains(uploadResp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", uploadResp.Body.String())
	}

	deleteResp := performCompatRequest(t, handler, http.MethodDelete, "/api/threads/"+threadID, nil, nil)
	if deleteResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("delete status=%d body=%s", deleteResp.Code, deleteResp.Body.String())
	}
	if !strings.Contains(deleteResp.Body.String(), "invalid thread_id") {
		t.Fatalf("body=%q", deleteResp.Body.String())
	}
}
