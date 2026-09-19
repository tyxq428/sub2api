package codexidentity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memorySink struct {
	mu      sync.Mutex
	batches [][]Event
	err     error
	block   chan struct{}
	started chan struct{}
}

func (s *memorySink) WriteBatch(_ context.Context, batch []Event) error {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, append([]Event(nil), batch...))
	return s.err
}

func (s *memorySink) events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	for _, batch := range s.batches {
		out = append(out, batch...)
	}
	return out
}

func TestObserverOffDoesNotRequireSecretOrSink(t *testing.T) {
	observer, err := NewObserver(ObserverOptions{Mode: ModeOff})
	require.NoError(t, err)
	result := observer.Record(RawSnapshot{}, StageInput, PurposeInference, "", "")
	require.Equal(t, RecordIgnoredOff, result)
	require.Equal(t, uint64(1), observer.Stats().IgnoredOff)
}

func TestObserverPseudonymizesAndNeverPersistsRawIdentity(t *testing.T) {
	sink := &memorySink{}
	observer, err := NewObserver(ObserverOptions{
		Mode:          ModeShadow,
		Secret:        []byte("01234567890123456789012345678901"),
		QueueCapacity: 8,
		MaxEventBytes: 4096,
		BatchSize:     1,
		FlushInterval: time.Hour,
		Sink:          sink,
		Now:           func() time.Time { return time.UnixMilli(1234) },
	})
	require.NoError(t, err)
	snapshot := Capture(http.Header{"Session-Id": {"raw-session-secret"}, "Thread-Id": {"raw-thread-secret"}}, nil, DefaultLimits())
	require.Equal(t, RecordEnqueued, observer.Record(snapshot, StageInput, PurposeInference, "codex-0.154-profile-r1", "observed"))
	observer.Close()
	events := sink.events()
	require.Len(t, events, 1)
	encoded, err := json.Marshal(events[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "raw-session-secret")
	require.NotContains(t, string(encoded), "raw-thread-secret")
	require.Contains(t, string(encoded), "codex-0.154-profile-r1")
	require.Equal(t, int64(1234), events[0].ObservedAtUnixMS)
}

func TestObserverQueueFullDropsWithoutBlocking(t *testing.T) {
	sink := &memorySink{block: make(chan struct{}), started: make(chan struct{}, 1)}
	observer, err := NewObserver(ObserverOptions{
		Mode: ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 1, MaxEventBytes: 4096, BatchSize: 1, FlushInterval: time.Hour, Sink: sink,
	})
	require.NoError(t, err)
	snapshot := Capture(http.Header{"Thread-Id": {"thread"}}, nil, DefaultLimits())
	require.Equal(t, RecordEnqueued, observer.Record(snapshot, StageInput, PurposeInference, "", ""))
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("observer worker did not start sink write")
	}
	require.Equal(t, RecordEnqueued, observer.Record(snapshot, StageActual, PurposeInference, "", ""))
	start := time.Now()
	require.Equal(t, RecordDroppedFull, observer.Record(snapshot, StageProposed, PurposeInference, "", ""))
	require.Less(t, time.Since(start), 100*time.Millisecond)
	close(sink.block)
	observer.Close()
	require.Equal(t, uint64(1), observer.Stats().DroppedFull)
}

func TestObserverCountsOversizeEvents(t *testing.T) {
	sink := &memorySink{}
	observer, err := NewObserver(ObserverOptions{
		Mode: ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 2, MaxEventBytes: 40, BatchSize: 1, FlushInterval: time.Hour, Sink: sink,
	})
	require.NoError(t, err)
	snapshot := Capture(http.Header{"Thread-Id": {"thread"}}, nil, DefaultLimits())
	require.Equal(t, RecordDroppedOversize, observer.Record(snapshot, StageInput, PurposeInference, "profile", "result"))
	observer.Close()
	require.Equal(t, uint64(1), observer.Stats().DroppedOversize)
}

func TestObserverCountsSinkErrors(t *testing.T) {
	sink := &memorySink{err: errors.New("synthetic sink failure")}
	observer, err := NewObserver(ObserverOptions{
		Mode: ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 2, MaxEventBytes: 4096, BatchSize: 1, FlushInterval: time.Hour, Sink: sink,
	})
	require.NoError(t, err)
	snapshot := Capture(http.Header{"Thread-Id": {"thread"}}, nil, DefaultLimits())
	require.Equal(t, RecordEnqueued, observer.Record(snapshot, StageInput, PurposeInference, "", ""))
	observer.Close()
	require.Equal(t, uint64(1), observer.Stats().WriteErrors)
	require.Zero(t, observer.Stats().Written)
}

func TestObserverRecordAfterCloseIsRejected(t *testing.T) {
	observer, err := NewObserver(ObserverOptions{
		Mode: ModeShadow, Secret: []byte("01234567890123456789012345678901"),
		QueueCapacity: 2, MaxEventBytes: 4096, BatchSize: 1, FlushInterval: time.Hour, Sink: &memorySink{},
	})
	require.NoError(t, err)
	observer.Close()
	snapshot := Capture(http.Header{"Thread-Id": {"thread"}}, nil, DefaultLimits())
	require.Equal(t, RecordDroppedStopped, observer.Record(snapshot, StageInput, PurposeInference, "", ""))
	require.Equal(t, uint64(1), observer.Stats().DroppedStopped)
}

func TestEvaluateShadowUsesPrivateSnapshotAndDoesNotDuplicateSend(t *testing.T) {
	headers := http.Header{"Session-Id": {"session-a"}}
	body := []byte(`{"input":"hello","client_metadata":{"session_id":"session-a"}}`)
	originalHeader := headers.Get("Session-Id")
	originalBody := append([]byte(nil), body...)
	snapshot := Capture(headers, body, DefaultLimits())
	plannerCalls := 0
	err := EvaluateShadow(ModeShadow, snapshot, func(candidate RawSnapshot) error {
		plannerCalls++
		fields := candidate.Fields()
		fields[0].Value = "proposed-only"
		return nil
	})
	require.NoError(t, err)
	sendCalls := 0
	send := func() {
		sendCalls++
		require.Equal(t, originalHeader, headers.Get("Session-Id"))
		require.Equal(t, originalBody, body)
	}
	send()
	require.Equal(t, 1, plannerCalls)
	require.Equal(t, 1, sendCalls)
	require.Equal(t, "session-a", snapshot.Fields()[0].Value)
}

func TestEvaluateShadowIsInactiveInOffMode(t *testing.T) {
	calls := 0
	require.NoError(t, EvaluateShadow(ModeOff, RawSnapshot{}, func(RawSnapshot) error { calls++; return nil }))
	require.Zero(t, calls)
}
