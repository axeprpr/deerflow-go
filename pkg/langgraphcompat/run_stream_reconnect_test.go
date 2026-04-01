package langgraphcompat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/llm"
	"github.com/axeprpr/deerflow-go/pkg/models"
)

type blockingStreamProvider struct {
	started chan llm.ChatRequest
	release chan struct{}
}

func (p *blockingStreamProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (p *blockingStreamProvider) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	select {
	case p.started <- req:
	default:
	}

	ch := make(chan llm.StreamChunk, 1)
	go func() {
		defer close(ch)
		select {
		case <-p.release:
			ch <- llm.StreamChunk{
				Done: true,
				Message: &models.Message{
					ID:        "stream-response",
					SessionID: "thread-detached",
					Role:      models.RoleAI,
					Content:   "done after reconnect",
				},
			}
		case <-ctx.Done():
			ch <- llm.StreamChunk{Err: ctx.Err()}
		}
	}()
	return ch, nil
}

func TestRunsStreamContinuesAfterClientCancellationAndSupportsJoin(t *testing.T) {
	provider := &blockingStreamProvider{
		started: make(chan llm.ChatRequest, 1),
		release: make(chan struct{}),
	}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		runStreams:   make(map[string]map[uint64]chan StreamEvent),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	body := `{"thread_id":"thread-detached","input":{"messages":[{"role":"user","content":"keep going"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(reqCtx)

	streamDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.handleRunsStream(rec, req)
		streamDone <- rec
	}()

	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run to start")
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	var activeRun *Run
	for time.Now().Before(deadline) {
		activeRun = s.getLatestActiveRunForThread("thread-detached")
		if activeRun != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if activeRun == nil {
		t.Fatal("expected active run to survive client cancellation")
	}

	bodyCh := make(chan string, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/thread-detached/stream", nil)
		mux.ServeHTTP(rec, req)
		bodyCh <- rec.Body.String()
	}()

	deadline = time.Now().Add(2 * time.Second)
	joined := false
	for time.Now().Before(deadline) {
		select {
		case body := <-bodyCh:
			t.Fatalf("join stream returned before run finished: %q", body)
		default:
		}

		s.runsMu.RLock()
		count := len(s.runStreams[activeRun.RunID])
		s.runsMu.RUnlock()
		if count > 0 {
			joined = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !joined {
		t.Fatalf("timed out waiting for join subscriber on %q", activeRun.RunID)
	}

	close(provider.release)

	select {
	case body := <-bodyCh:
		if !strings.Contains(body, `"content":"done after reconnect"`) {
			t.Fatalf("expected joined stream to receive final response, body=%q", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for joined stream response")
	}

	select {
	case rec := <-streamDone:
		resp := rec.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("stream status=%d body=%s", resp.StatusCode, string(payload))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for original stream handler to finish")
	}

	stored := s.getRun(activeRun.RunID)
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "success" {
		t.Fatalf("run status=%q want=success err=%q", stored.Status, stored.Error)
	}
}

func TestRunsStreamCancelsAbandonedRunAfterGracePeriod(t *testing.T) {
	t.Setenv("DEERFLOW_RUN_RECONNECT_GRACE", "50ms")

	provider := &blockingStreamProvider{
		started: make(chan llm.ChatRequest, 1),
		release: make(chan struct{}),
	}
	s := &Server{
		llmProvider:  provider,
		defaultModel: "default-model",
		tools:        newRuntimeToolRegistry(t),
		sessions:     make(map[string]*Session),
		runs:         make(map[string]*Run),
		runStreams:   make(map[string]map[uint64]chan StreamEvent),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}

	body := `{"thread_id":"thread-abandoned","input":{"messages":[{"role":"user","content":"stop"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	reqCtx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(reqCtx)

	streamDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.handleRunsStream(rec, req)
		streamDone <- rec
	}()

	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for run to start")
	}

	cancel()

	var rec *httptest.ResponseRecorder
	select {
	case rec = <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for abandoned run to stop")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	var stored *Run
	for time.Now().Before(deadline) {
		stored = s.getLatestActiveRunForThread("thread-abandoned")
		if stored == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	stored = s.getRun(findRunIDForThread(s, "thread-abandoned"))
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "error" {
		t.Fatalf("run status=%q want=error err=%q", stored.Status, stored.Error)
	}
	if !strings.Contains(strings.ToLower(stored.Error), "canceled") {
		t.Fatalf("run error=%q want cancellation", stored.Error)
	}
}

func findRunIDForThread(s *Server, threadID string) string {
	s.runsMu.RLock()
	defer s.runsMu.RUnlock()
	for _, run := range s.runs {
		if run.ThreadID == threadID {
			return run.RunID
		}
	}
	return ""
}
