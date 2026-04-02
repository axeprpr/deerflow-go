package langgraphcompat

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestUploadPlainTextDocumentCreatesMarkdownCompanion(t *testing.T) {
	_, handler := newCompatTestServer(t)
	threadID := "thread-gateway-text"

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", "notes.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("first line\nsecond line\n")); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	resp := performCompatRequest(t, handler, http.MethodPost, "/api/threads/"+threadID+"/uploads", &body, map[string]string{"Content-Type": w.FormDataContentType()})
	if resp.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
	}

	var uploaded struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if len(uploaded.Files) != 1 {
		t.Fatalf("files=%d want=1", len(uploaded.Files))
	}
	if got := asString(uploaded.Files[0]["markdown_file"]); got != "notes.md" {
		t.Fatalf("markdown_file=%q want=notes.md", got)
	}

	mdResp := performCompatRequest(t, handler, http.MethodGet, "/api/threads/"+threadID+"/artifacts/mnt/user-data/uploads/notes.md", nil, nil)
	if mdResp.Code != http.StatusOK {
		t.Fatalf("markdown artifact status=%d", mdResp.Code)
	}
	if body := mdResp.Body.String(); !strings.Contains(body, "first line") || !strings.Contains(body, "second line") {
		t.Fatalf("markdown body=%q missing uploaded text", body)
	}
}
