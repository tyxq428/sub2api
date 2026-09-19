package codexidentity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type ObservedField struct {
	Carrier Carrier       `json:"carrier"`
	Name    string        `json:"name"`
	Class   IdentityClass `json:"class"`
	Digest  string        `json:"digest"`
}

// Event contains only pseudonymized values. Raw identifiers, request content,
// credentials and complete user-agent suffixes never enter a Sink.
type Event struct {
	AccountID        int64           `json:"-"`
	ObservedAtUnixMS int64           `json:"observed_at_unix_ms"`
	Stage            Stage           `json:"stage"`
	Purpose          Purpose         `json:"purpose"`
	Profile          string          `json:"profile,omitempty"`
	Result           string          `json:"result,omitempty"`
	CoverageComplete bool            `json:"coverage_complete"`
	CoverageReasons  []string        `json:"coverage_reasons,omitempty"`
	Fields           []ObservedField `json:"fields,omitempty"`
}

type BatchSink interface {
	WriteBatch(context.Context, []Event) error
}

type ObserverOptions struct {
	Mode          Mode
	Secret        []byte
	QueueCapacity int
	MaxEventBytes int
	BatchSize     int
	FlushInterval time.Duration
	Sink          BatchSink
	Now           func() time.Time
}

type ObserverStats struct {
	Submitted       uint64
	Enqueued        uint64
	IgnoredOff      uint64
	DroppedFull     uint64
	DroppedOversize uint64
	DroppedStopped  uint64
	Written         uint64
	WriteErrors     uint64
}

type RecordResult string

const (
	RecordIgnoredOff      RecordResult = "ignored_off"
	RecordEnqueued        RecordResult = "enqueued"
	RecordDroppedFull     RecordResult = "dropped_full"
	RecordDroppedOversize RecordResult = "dropped_oversize"
	RecordDroppedStopped  RecordResult = "dropped_stopped"
)

type Observer struct {
	mode            Mode
	secret          []byte
	queue           chan Event
	maxEventBytes   int
	batchSize       int
	flushInterval   time.Duration
	sink            BatchSink
	now             func() time.Time
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	stopOnce        sync.Once
	acceptMu        sync.RWMutex
	stopped         atomic.Bool
	submitted       atomic.Uint64
	enqueued        atomic.Uint64
	ignoredOff      atomic.Uint64
	droppedFull     atomic.Uint64
	droppedOversize atomic.Uint64
	droppedStopped  atomic.Uint64
	written         atomic.Uint64
	writeErrors     atomic.Uint64
}

func NewObserver(options ObserverOptions) (*Observer, error) {
	mode := ParseMode(string(options.Mode))
	o := &Observer{mode: mode}
	if mode == ModeOff {
		return o, nil
	}
	if len(options.Secret) < 32 {
		return nil, errors.New("codex identity observer secret must contain at least 32 bytes")
	}
	if options.Sink == nil {
		return nil, errors.New("codex identity observer sink is required")
	}
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = 4096
	}
	if options.MaxEventBytes <= 0 {
		options.MaxEventBytes = 4096
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 128
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = 5 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.secret = append([]byte(nil), options.Secret...)
	o.queue = make(chan Event, options.QueueCapacity)
	o.maxEventBytes = options.MaxEventBytes
	o.batchSize = options.BatchSize
	o.flushInterval = options.FlushInterval
	o.sink = options.Sink
	o.now = options.Now
	o.cancel = cancel
	o.wg.Add(1)
	go o.run(ctx)
	return o, nil
}

func (o *Observer) Record(snapshot RawSnapshot, stage Stage, purpose Purpose, profile, result string) RecordResult {
	return o.RecordForAccount(0, snapshot, stage, purpose, profile, result)
}

// RecordForAccount attaches the local account owner outside the serialized
// diagnostic payload. Raw identity values still never enter a Sink.
func (o *Observer) RecordForAccount(accountID int64, snapshot RawSnapshot, stage Stage, purpose Purpose, profile, result string) RecordResult {
	if o == nil || o.mode == ModeOff {
		if o != nil {
			o.ignoredOff.Add(1)
		}
		return RecordIgnoredOff
	}
	o.submitted.Add(1)
	o.acceptMu.RLock()
	defer o.acceptMu.RUnlock()
	if o.stopped.Load() {
		o.droppedStopped.Add(1)
		return RecordDroppedStopped
	}
	event := o.buildEvent(snapshot, stage, purpose, profile, result)
	event.AccountID = accountID
	encoded, err := json.Marshal(event)
	if err != nil || len(encoded) > o.maxEventBytes {
		o.droppedOversize.Add(1)
		return RecordDroppedOversize
	}
	select {
	case o.queue <- event:
		o.enqueued.Add(1)
		return RecordEnqueued
	default:
		o.droppedFull.Add(1)
		return RecordDroppedFull
	}
}

func (o *Observer) buildEvent(snapshot RawSnapshot, stage Stage, purpose Purpose, profile, result string) Event {
	coverage := snapshot.Coverage()
	event := Event{
		ObservedAtUnixMS: o.now().UnixMilli(),
		Stage:            stage,
		Purpose:          purpose,
		Profile:          profile,
		Result:           result,
		CoverageComplete: coverage.Complete,
		CoverageReasons:  append([]string(nil), coverage.Reasons...),
	}
	for _, field := range snapshot.Fields() {
		event.Fields = append(event.Fields, ObservedField{
			Carrier: field.Carrier,
			Name:    field.Name,
			Class:   field.Class,
			Digest:  o.digest(field.Class, field.Value),
		})
	}
	sort.SliceStable(event.Fields, func(i, j int) bool {
		if event.Fields[i].Class != event.Fields[j].Class {
			return event.Fields[i].Class < event.Fields[j].Class
		}
		if event.Fields[i].Carrier != event.Fields[j].Carrier {
			return event.Fields[i].Carrier < event.Fields[j].Carrier
		}
		return event.Fields[i].Name < event.Fields[j].Name
	})
	return event
}

func (o *Observer) digest(class IdentityClass, value string) string {
	mac := hmac.New(sha256.New, o.secret)
	_, _ = mac.Write([]byte("codex-r2-shadow-v1\x00"))
	_, _ = mac.Write([]byte(class))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

func (o *Observer) Stats() ObserverStats {
	if o == nil {
		return ObserverStats{}
	}
	return ObserverStats{
		Submitted:       o.submitted.Load(),
		Enqueued:        o.enqueued.Load(),
		IgnoredOff:      o.ignoredOff.Load(),
		DroppedFull:     o.droppedFull.Load(),
		DroppedOversize: o.droppedOversize.Load(),
		DroppedStopped:  o.droppedStopped.Load(),
		Written:         o.written.Load(),
		WriteErrors:     o.writeErrors.Load(),
	}
}

func (o *Observer) Close() {
	if o == nil || o.mode == ModeOff {
		return
	}
	o.stopOnce.Do(func() {
		o.acceptMu.Lock()
		o.stopped.Store(true)
		o.cancel()
		o.acceptMu.Unlock()
		o.wg.Wait()
	})
}

func (o *Observer) run(ctx context.Context) {
	defer o.wg.Done()
	ticker := time.NewTicker(o.flushInterval)
	defer ticker.Stop()
	batch := make([]Event, 0, o.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		out := append([]Event(nil), batch...)
		batch = batch[:0]
		if err := o.sink.WriteBatch(context.Background(), out); err != nil {
			o.writeErrors.Add(1)
			return
		}
		o.written.Add(uint64(len(out)))
	}
	for {
		select {
		case event := <-o.queue:
			batch = append(batch, event)
			if len(batch) >= o.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			for {
				select {
				case event := <-o.queue:
					batch = append(batch, event)
					if len(batch) >= o.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// EvaluateShadow gives the evaluator a private copy. It never receives request
// headers/body and cannot alter the bytes used by the actual sender.
func EvaluateShadow(mode Mode, snapshot RawSnapshot, evaluator func(RawSnapshot) error) error {
	if ParseMode(string(mode)) != ModeShadow || evaluator == nil {
		return nil
	}
	return evaluator(snapshot.Clone())
}
