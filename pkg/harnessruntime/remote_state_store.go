package harnessruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type remoteRunSnapshotStore struct {
	client   *HTTPRemoteStateClient
	protocol RemoteStateProtocol
	endpoint string
}

func NewRemoteRunSnapshotStore(endpoint string, client *HTTPRemoteStateClient, protocol RemoteStateProtocol) RunSnapshotStore {
	return &remoteRunSnapshotStore{
		client:   defaultHTTPRemoteStateClient(client),
		protocol: defaultRemoteStateProtocol(protocol),
		endpoint: strings.TrimSpace(endpoint),
	}
}

func (s *remoteRunSnapshotStore) LoadRunSnapshot(runID string) (RunSnapshot, bool) {
	body, status, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateSnapshotURL(s.endpoint, runID), nil, "")
	if err != nil || status == http.StatusNotFound {
		return RunSnapshot{}, false
	}
	snapshot, err := s.protocol.DecodeRunSnapshot(body)
	if err != nil {
		return RunSnapshot{}, false
	}
	return snapshot, true
}

func (s *remoteRunSnapshotStore) ListRunSnapshots(threadID string) []RunSnapshot {
	body, _, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateSnapshotsListURL(s.endpoint, threadID), nil, "")
	if err != nil {
		return nil
	}
	snapshots, err := s.protocol.DecodeRunSnapshots(body)
	if err != nil {
		return nil
	}
	return snapshots
}

func (s *remoteRunSnapshotStore) SaveRunSnapshot(snapshot RunSnapshot) {
	body, err := s.protocol.EncodeRunSnapshot(snapshot)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPut, joinRemoteStateSnapshotURL(s.endpoint, snapshot.Record.RunID), body, "application/json")
}

func (s *remoteRunSnapshotStore) TryCancelStaleRun(runID string, staleBefore time.Time) (RunRecord, bool) {
	payload, err := s.protocol.EncodeAny(map[string]any{
		"stale_before": staleBefore.UTC(),
	})
	if err != nil {
		return RunRecord{}, false
	}
	body, _, err := s.client.Do(context.Background(), http.MethodPost, joinRemoteStateSnapshotCancelURL(s.endpoint, runID), payload, "application/json")
	if err != nil {
		return RunRecord{}, false
	}
	value, err := s.protocol.DecodeAny(body)
	if err != nil {
		return RunRecord{}, false
	}
	result, ok := value.(map[string]any)
	if !ok {
		return RunRecord{}, false
	}
	changed, _ := result["changed"].(bool)
	if !changed {
		return RunRecord{}, false
	}
	record, ok := decodeRemoteRunRecord(result["record"])
	if !ok {
		return RunRecord{}, false
	}
	return record, true
}

type remoteRunEventStore struct {
	client       *HTTPRemoteStateClient
	protocol     RemoteStateProtocol
	endpoint     string
	pollInterval time.Duration
}

func NewRemoteRunEventStore(endpoint string, client *HTTPRemoteStateClient, protocol RemoteStateProtocol) RunEventStore {
	return &remoteRunEventStore{
		client:       defaultHTTPRemoteStateClient(client),
		protocol:     defaultRemoteStateProtocol(protocol),
		endpoint:     strings.TrimSpace(endpoint),
		pollInterval: 250 * time.Millisecond,
	}
}

func (s *remoteRunEventStore) NextRunEventIndex(runID string) int {
	body, _, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateEventNextIndexURL(s.endpoint, runID), nil, "")
	if err != nil {
		return 1
	}
	value, err := s.protocol.DecodeInt(body)
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func (s *remoteRunEventStore) AppendRunEvent(runID string, event RunEvent) {
	body, err := s.protocol.EncodeRunEvent(event)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPost, joinRemoteStateEventsURL(s.endpoint, runID), body, "application/json")
}

func (s *remoteRunEventStore) LoadRunEvents(runID string) []RunEvent {
	body, _, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateEventsURL(s.endpoint, runID), nil, "")
	if err != nil {
		return nil
	}
	events, err := s.protocol.DecodeRunEvents(body)
	if err != nil {
		return nil
	}
	return events
}

func (s *remoteRunEventStore) ReplaceRunEvents(runID string, events []RunEvent) {
	body, err := s.protocol.EncodeRunEvents(events)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPut, joinRemoteStateEventsURL(s.endpoint, runID), body, "application/json")
}

func (s *remoteRunEventStore) SubscribeRunEvents(runID string, buffer int) (<-chan RunEvent, func()) {
	if buffer < 1 {
		buffer = 1
	}
	out := make(chan RunEvent, buffer)
	done := make(chan struct{})
	lastIndex := len(s.LoadRunEvents(runID))
	go func() {
		defer close(out)
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				events := s.LoadRunEvents(runID)
				if len(events) <= lastIndex {
					continue
				}
				for _, event := range events[lastIndex:] {
					select {
					case out <- event:
					case <-done:
						return
					}
				}
				lastIndex = len(events)
			}
		}
	}()
	return out, func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}
}

type remoteThreadStateStore struct {
	client   *HTTPRemoteStateClient
	protocol RemoteStateProtocol
	endpoint string
}

func NewRemoteThreadStateStore(endpoint string, client *HTTPRemoteStateClient, protocol RemoteStateProtocol) ThreadStateStore {
	return &remoteThreadStateStore{
		client:   defaultHTTPRemoteStateClient(client),
		protocol: defaultRemoteStateProtocol(protocol),
		endpoint: strings.TrimSpace(endpoint),
	}
}

func (s *remoteThreadStateStore) LoadThreadRuntimeState(threadID string) (ThreadRuntimeState, bool) {
	body, status, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateThreadURL(s.endpoint, threadID), nil, "")
	if err != nil || status == http.StatusNotFound {
		return ThreadRuntimeState{}, false
	}
	state, err := s.protocol.DecodeThreadRuntimeState(body)
	if err != nil {
		return ThreadRuntimeState{}, false
	}
	return state, true
}

func (s *remoteThreadStateStore) ListThreadRuntimeStates() []ThreadRuntimeState {
	body, _, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateThreadsURL(s.endpoint), nil, "")
	if err != nil {
		return nil
	}
	states, err := s.protocol.DecodeThreadRuntimeStates(body)
	if err != nil {
		return nil
	}
	return states
}

func (s *remoteThreadStateStore) SaveThreadRuntimeState(state ThreadRuntimeState) {
	body, err := s.protocol.EncodeThreadRuntimeState(state)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPut, joinRemoteStateThreadURL(s.endpoint, state.ThreadID), body, "application/json")
}

func (s *remoteThreadStateStore) HasThread(threadID string) bool {
	_, status, err := s.client.Do(context.Background(), http.MethodGet, joinRemoteStateThreadURL(s.endpoint, threadID), nil, "")
	return err == nil && status == http.StatusOK
}

func (s *remoteThreadStateStore) MarkThreadStatus(threadID string, status string) {
	body, err := s.protocol.EncodeAny(status)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPost, joinRemoteStateThreadStatusURL(s.endpoint, threadID), body, "application/json")
}

func (s *remoteThreadStateStore) SetThreadMetadata(threadID string, key string, value any) {
	body, err := s.protocol.EncodeAny(value)
	if err != nil {
		return
	}
	_, _, _ = s.client.Do(context.Background(), http.MethodPut, joinRemoteStateThreadMetadataURL(s.endpoint, threadID, key), body, "application/json")
}

func (s *remoteThreadStateStore) ClearThreadMetadata(threadID string, key string) {
	_, _, _ = s.client.Do(context.Background(), http.MethodDelete, joinRemoteStateThreadMetadataURL(s.endpoint, threadID, key), nil, "")
}

func defaultHTTPRemoteStateClient(client *HTTPRemoteStateClient) *HTTPRemoteStateClient {
	if client != nil {
		return client
	}
	return NewHTTPRemoteStateClient(nil)
}

func decodeRemoteRunRecord(value any) (RunRecord, bool) {
	if value == nil {
		return RunRecord{}, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return RunRecord{}, false
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, false
	}
	if strings.TrimSpace(record.RunID) == "" {
		return RunRecord{}, false
	}
	return record, true
}
