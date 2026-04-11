package langgraphcompat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axeprpr/deerflow-go/pkg/harnessruntime"
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
		runRegistry:  newRunRegistry(),
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
		record, ok := harnessruntime.NewQueryService(s.runtimeQueryAdapter()).LatestActiveRun("thread-detached")
		if ok {
			activeRun = runFromRecord(record)
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

		if s.runSubscriberCount(activeRun.RunID) > 0 {
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

func TestRunsStreamCancelsAbandonedRunWhenDisconnectModeIsCancel(t *testing.T) {
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
		runRegistry:  newRunRegistry(),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}

	body := `{"thread_id":"thread-abandoned","on_disconnect":"cancel","input":{"messages":[{"role":"user","content":"stop"}]}}`
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

	var stored *Run
	stored = s.getRun(findRunIDForThread(s, "thread-abandoned"))
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "interrupted" {
		t.Fatalf("run status=%q want=interrupted err=%q", stored.Status, stored.Error)
	}
	if stored.Error != "" {
		t.Fatalf("run error=%q want empty", stored.Error)
	}
}

func TestRunsStreamResumableRequestsSurviveClientCancellationWithoutJoin(t *testing.T) {
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
		runRegistry:  newRunRegistry(),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}

	body := `{"thread_id":"thread-resumable","stream_resumable":true,"input":{"messages":[{"role":"user","content":"keep going"}]}}`
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
		t.Fatal("timed out waiting for resumable run to start")
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
	close(provider.release)

	var rec *httptest.ResponseRecorder
	select {
	case rec = <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for resumable run to finish")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", rec.Code, rec.Body.String())
	}

	stored := s.getRun(findRunIDForThread(s, "thread-resumable"))
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "success" {
		t.Fatalf("run status=%q want=success err=%q", stored.Status, stored.Error)
	}
}

func TestThreadRunJoinWaitsForCompletion(t *testing.T) {
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
		runRegistry:  newRunRegistry(),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	body := `{"thread_id":"thread-join-wait","input":{"messages":[{"role":"user","content":"keep going"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	runID := findRunIDForThread(s, "thread-join-wait")
	if runID == "" {
		t.Fatal("run id missing")
	}

	joinDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/threads/thread-join-wait/runs/"+runID+"/join", nil)
		mux.ServeHTTP(rec, req)
		joinDone <- rec
	}()

	select {
	case rec := <-joinDone:
		t.Fatalf("join returned before run finished: %s", rec.Body.String())
	case <-time.After(150 * time.Millisecond):
	}

	close(provider.release)

	select {
	case rec := <-joinDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("join status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"status":"success"`) {
			t.Fatalf("join body=%s", rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for join response")
	}

	select {
	case <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream completion")
	}
}

func TestThreadRunJoinCanCancelRunOnDisconnect(t *testing.T) {
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
		runRegistry:  newRunRegistry(),
		dataRoot:     t.TempDir(),
		agents:       map[string]gatewayAgent{},
	}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	body := `{"thread_id":"thread-join-cancel","input":{"messages":[{"role":"user","content":"keep going"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

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

	runID := findRunIDForThread(s, "thread-join-cancel")
	if runID == "" {
		t.Fatal("run id missing")
	}

	joinCtx, cancelJoin := context.WithCancel(context.Background())
	joinReq := httptest.NewRequest(http.MethodGet, "/threads/thread-join-cancel/runs/"+runID+"/join?cancel_on_disconnect=true", nil).WithContext(joinCtx)
	joinDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, joinReq)
		joinDone <- rec
	}()

	cancelJoin()

	select {
	case <-joinDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for join disconnect")
	}

	select {
	case <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for canceled run to stop")
	}

	stored := s.getRun(runID)
	if stored == nil {
		t.Fatal("stored run missing")
	}
	if stored.Status != "interrupted" {
		t.Fatalf("run status=%q want=interrupted err=%q", stored.Status, stored.Error)
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
