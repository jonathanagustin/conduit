// Package application manages durable, identity-scoped event delivery.
// It is not composed into a runtime until committed-source adapters exist.
package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
)

var opaque = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$`)
var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

type Config struct {
	Journal    ports.Journal
	Authorizer ports.Authorizer
	Validator  ports.Validator
	Readiness  ports.Readiness
	Now        func() time.Time
	Token      func(string) (string, error)
	MaxStreams int
}
type Service struct {
	failed   bool
	mu       sync.Mutex
	cfg      Config
	sessions map[string]*connection
}
type connection struct {
	id, stream        string
	identity          domain.Identity
	ctx               context.Context
	openedAt          time.Time
	next, replayUpper uint64
	inflight          *domain.Frame
	err               error
	changed           chan struct{}
}

// Ready is a queue watermark, not a claim that native read models are current.
type Ready struct {
	StreamID string
	Sequence uint64
	Cursor   string
}
type Subscription struct {
	service *Service
	id      string
	Ready   Ready
}

func New(cfg Config) (*Service, error) {
	if cfg.Journal == nil || cfg.Authorizer == nil || cfg.Validator == nil || cfg.Readiness == nil {
		return nil, domain.ErrInvalid
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Token == nil {
		cfg.Token = func(prefix string) (string, error) {
			var body [32]byte
			if _, err := rand.Read(body[:]); err != nil {
				return "", err
			}
			return prefix + base64.RawURLEncoding.EncodeToString(body[:]), nil
		}
	}
	if cfg.MaxStreams == 0 {
		cfg.MaxStreams = 128
	}
	if cfg.MaxStreams < 1 {
		return nil, domain.ErrInvalid
	}
	return &Service{cfg: cfg, sessions: map[string]*connection{}}, nil
}

func (service *Service) identity(ctx context.Context) (domain.Identity, error) {
	identity, err := service.cfg.Authorizer.Snapshot(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) || errors.Is(err, domain.ErrForbidden) {
			return domain.Identity{}, err
		}
		return domain.Identity{}, domain.ErrUnavailable
	}
	if !identity.Valid() {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	return identity, nil
}
func (service *Service) check(ctx context.Context, expected domain.Identity) error {
	current, err := service.identity(ctx)
	if err != nil {
		return err
	}
	if current != expected {
		return domain.ErrReset
	}
	if service.failed || service.cfg.Readiness.Check(ctx) != nil {
		return domain.ErrUnavailable
	}
	return nil
}
func (service *Service) token(prefix string, used map[string]bool) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		value, err := service.cfg.Token(prefix)
		if err != nil {
			return "", domain.ErrUnavailable
		}
		if !cursorPattern.MatchString(value) || len(value) > 200 {
			return "", domain.ErrInvalid
		}
		if !used[value] {
			used[value] = true
			return value, nil
		}
	}
	return "", domain.ErrUnavailable
}
func usedTokens(state domain.State) map[string]bool {
	used := map[string]bool{}
	for _, stream := range state.Streams {
		used[stream.ID] = true
		used[stream.RootCursor] = true
		for _, frame := range stream.Frames {
			used[frame.Envelope.Cursor] = true
		}
	}
	return used
}
func (service *Service) active(streamID string) bool {
	for _, connection := range service.sessions {
		if connection.stream == streamID && connection.err == nil {
			return true
		}
	}
	return false
}
func trimFrames(stream domain.Stream, now time.Time) domain.Stream {
	first := 0
	for first < len(stream.Frames) && !stream.Frames[first].StoredAt.After(now.Add(-domain.ReplayRetention)) {
		first++
	}
	if len(stream.Frames)-first > domain.MaxReplayEvents {
		first = len(stream.Frames) - domain.MaxReplayEvents
	}
	stream.Frames = append([]domain.Frame(nil), stream.Frames[first:]...)
	return stream
}
func (service *Service) prune(state *domain.State, now time.Time) {
	first := 0
	for first < len(state.Sources) && !state.Sources[first].StoredAt.After(now.Add(-domain.ReplayRetention)) {
		first++
	}
	state.Sources = append([]domain.SourceRow(nil), state.Sources[first:]...)
	for id, stream := range state.Streams {
		stream = trimFrames(stream, now)
		last := stream.CreatedAt
		if len(stream.Frames) > 0 {
			last = stream.Frames[len(stream.Frames)-1].StoredAt
		}
		if !service.active(id) && !last.After(now.Add(-domain.ReplayRetention)) {
			delete(state.Streams, id)
			continue
		}
		state.Streams[id] = stream
	}
}
func validState(state domain.State) bool {
	return state.Version == domain.Version && state.Streams != nil && state.Producers != nil
}

// Open authenticates and checks readiness before allocating any stream state.
// A cursor is an opaque stored position, never authority to read its stream.
func (service *Service) Open(ctx context.Context, cursor string) (*Subscription, error) {
	identity, err := service.identity(ctx)
	if err != nil {
		return nil, err
	}
	if service.cfg.Readiness.Check(ctx) != nil {
		return nil, domain.ErrUnavailable
	}
	if cursor != "" && !cursorPattern.MatchString(cursor) {
		return nil, domain.ErrReset
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.failed {
		return nil, domain.ErrUnavailable
	}
	if len(service.sessions) >= service.cfg.MaxStreams {
		return nil, domain.ErrUnavailable
	}
	now := service.cfg.Now().UTC()
	var ready Ready
	err = service.cfg.Journal.Update(ctx, func(state *domain.State) error {
		if !validState(*state) {
			return domain.ErrUnavailable
		}
		service.prune(state, now)
		used := usedTokens(*state)
		if cursor == "" {
			if len(state.Streams) >= service.cfg.MaxStreams {
				return domain.ErrUnavailable
			}
			id, err := service.token("s_", used)
			if err != nil {
				return err
			}
			root, err := service.token("c_", used)
			if err != nil {
				return err
			}
			stream := domain.Stream{ID: id, Identity: identity, CreatedAt: now, RootCursor: root, SourcePosition: state.SourcePosition}
			state.Streams[id] = stream
			ready = Ready{StreamID: id, Cursor: root}
			return nil
		}
		for id, stream := range state.Streams {
			if stream.Identity != identity {
				continue
			}
			sequence, found := uint64(0), false
			if cursor == stream.RootCursor && stream.CreatedAt.After(now.Add(-domain.ReplayRetention)) {
				found = true
			}
			for _, frame := range stream.Frames {
				if frame.Envelope.Cursor == cursor {
					sequence = frame.Envelope.Sequence
					found = true
					break
				}
			}
			if !found {
				continue
			}
			updated, err := service.materialize(ctx, stream, *state, now, used)
			if err != nil {
				return err
			}
			if err := service.check(ctx, identity); err != nil {
				return err
			}
			for _, frame := range updated.Frames {
				if frame.Envelope.Sequence <= sequence {
					continue
				}
				if service.validateFrame(frame, id) != nil {
					return domain.ErrReset
				}
				visible, err := service.cfg.Authorizer.Visible(ctx, identity, frame.Subject)
				if err != nil {
					return domain.ErrUnavailable
				}
				if !visible {
					return domain.ErrReset
				}
			}
			if sequence < updated.LastSequence && !hasFrame(updated, sequence+1) {
				return domain.ErrReset
			}
			state.Streams[id] = updated
			ready = Ready{StreamID: id, Sequence: sequence, Cursor: cursor}
			return nil
		}
		return domain.ErrReset
	})
	if err != nil {
		return nil, err
	}
	if err := service.check(ctx, identity); err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for id := range service.sessions {
		used[id] = true
	}
	id, err := service.token("q_", used)
	if err != nil {
		return nil, err
	}
	snapshot, err := service.cfg.Journal.Snapshot(ctx)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	stream, ok := snapshot.Streams[ready.StreamID]
	if !ok {
		return nil, domain.ErrReset
	}
	service.sessions[id] = &connection{id: id, stream: ready.StreamID, identity: identity, ctx: ctx, openedAt: now, next: ready.Sequence + 1, replayUpper: stream.LastSequence, changed: make(chan struct{}, 1)}
	return &Subscription{service: service, id: id, Ready: ready}, nil
}
func hasFrame(stream domain.Stream, sequence uint64) bool {
	for _, frame := range stream.Frames {
		if frame.Envelope.Sequence == sequence {
			return true
		}
	}
	return false
}
func sourceDigest(record domain.Record) (string, error) {
	body, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func (service *Service) canonical(record domain.Record) (domain.Record, error) {
	record.Content.Data = append(json.RawMessage(nil), record.Content.Data...)
	record.Content.OccurredAt = record.Content.OccurredAt.UTC()
	if record.Content.OccurredAt.IsZero() || !opaque.MatchString(record.Subject.ID) || record.Position == 0 {
		return domain.Record{}, domain.ErrInvalid
	}
	switch record.Subject.Type {
	case "plan", "environment", "packaging-run", "assistant-run", "change-request":
	default:
		return domain.Record{}, domain.ErrInvalid
	}
	if record.Producer == "" {
		record.Producer = domain.ProducerID(record.Subject)
	}
	if record.ID == "" {
		record.ID = domain.RecordID(record.Producer, record.Position)
	}
	if record.Producer != domain.ProducerID(record.Subject) || record.ID != domain.RecordID(record.Producer, record.Position) {
		return domain.Record{}, domain.ErrInvalid
	}
	if !binding(record.Subject, record.Content.Resource) {
		return domain.Record{}, domain.ErrInvalid
	}
	probe := domain.Envelope{Version: domain.Version, StreamID: strings.Repeat("s", 200), Sequence: 1, Cursor: strings.Repeat("c", 200), Content: record.Content}
	if service.cfg.Validator.Envelope(probe) != nil {
		return domain.Record{}, domain.ErrInvalid
	}
	return record, nil
}
func binding(subject domain.Subject, resource domain.Resource) bool {
	if resource.Type == "index" {
		return resource.ID == domain.IndexID(subject)
	}
	return resource.Type == subject.Type && resource.ID == subject.ID
}
func (service *Service) validateFrame(frame domain.Frame, streamID string) error {
	if frame.Envelope.StreamID != streamID || !binding(frame.Subject, frame.Envelope.Resource) {
		return domain.ErrInvalid
	}
	return service.cfg.Validator.Envelope(frame.Envelope)
}
func (service *Service) materialize(ctx context.Context, stream domain.Stream, state domain.State, now time.Time, used map[string]bool) (domain.Stream, error) {
	if stream.SourcePosition > state.SourcePosition {
		return domain.Stream{}, domain.ErrReset
	}
	if stream.SourcePosition < state.SourcePosition && (len(state.Sources) == 0 || state.Sources[0].Position > stream.SourcePosition+1) {
		return domain.Stream{}, domain.ErrReset
	}
	stream.Frames = append([]domain.Frame(nil), stream.Frames...)
	for _, row := range state.Sources {
		if row.Position <= stream.SourcePosition {
			continue
		}
		if row.Position != stream.SourcePosition+1 {
			return domain.Stream{}, domain.ErrReset
		}
		visible, err := service.cfg.Authorizer.Visible(ctx, stream.Identity, row.Record.Subject)
		if err != nil {
			return domain.Stream{}, domain.ErrUnavailable
		}
		if visible {
			if stream.LastSequence >= domain.MaxSequence {
				return domain.Stream{}, domain.ErrReset
			}
			cursor, err := service.token("c_", used)
			if err != nil {
				return domain.Stream{}, err
			}
			frame := domain.Frame{Envelope: domain.Envelope{Version: domain.Version, StreamID: stream.ID, Sequence: stream.LastSequence + 1, Cursor: cursor, Content: row.Record.Content}, Subject: row.Record.Subject, StoredAt: now}
			if service.validateFrame(frame, stream.ID) != nil {
				return domain.Stream{}, domain.ErrInvalid
			}
			stream.Frames = append(stream.Frames, frame)
			stream.LastSequence++
		}
		stream.SourcePosition = row.Position
	}
	return trimFrames(stream, now), nil
}

// PublishCommittedBatch is an internal producer port. Adapters must supply only
// durably committed native records, ordered by their persisted source position.
// Success acknowledges durable fan-out; no public emit endpoint is provided.
func (service *Service) PublishCommittedBatch(ctx context.Context, records []domain.Record) error {
	if len(records) > domain.MaxQueueEvents {
		service.publicationFailed()
		return domain.ErrInvalid
	}
	if len(records) == 0 {
		return nil
	}
	canonical := make([]domain.Record, len(records))
	for index, record := range records {
		value, err := service.canonical(record)
		if err != nil {
			service.publicationFailed()
			return err
		}
		canonical[index] = value
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.cfg.Now().UTC()
	closed := map[string]error{}
	err := service.cfg.Journal.Update(ctx, func(state *domain.State) error {
		if !validState(*state) {
			return domain.ErrUnavailable
		}
		service.prune(state, now)
		for _, record := range canonical {
			digest, err := sourceDigest(record)
			if err != nil {
				return domain.ErrInvalid
			}
			previous, seen := state.Producers[record.Producer]
			if seen && record.Position <= previous.Position {
				if record.Position == previous.Position && digest != previous.Digest {
					return domain.ErrConflict
				}
				continue
			}
			if seen && (previous.Position == math.MaxUint64 || record.Position != previous.Position+1) {
				return domain.ErrReset
			}
			if state.SourcePosition == math.MaxUint64 {
				return domain.ErrUnavailable
			}
			state.SourcePosition++
			state.Sources = append(state.Sources, domain.SourceRow{Position: state.SourcePosition, StoredAt: now, Record: record})
			state.Producers[record.Producer] = domain.ProducerCheckpoint{Position: record.Position, Digest: digest}
		}
		used := usedTokens(*state)
		processed := map[string]bool{}
		for id, connection := range service.sessions {
			if connection.err != nil {
				continue
			}
			if connection.ctx.Err() != nil {
				closed[id] = domain.ErrClosed
				continue
			}
			if err := service.check(connection.ctx, connection.identity); err != nil {
				closed[id] = err
				continue
			}
			if !processed[connection.stream] {
				stream, ok := state.Streams[connection.stream]
				if !ok {
					closed[id] = domain.ErrReset
					continue
				}
				updated, err := service.materialize(connection.ctx, stream, *state, now, used)
				if err != nil {
					closed[id] = err
					continue
				}
				if err := service.check(connection.ctx, connection.identity); err != nil {
					closed[id] = err
					continue
				}
				state.Streams[connection.stream] = updated
				processed[connection.stream] = true
			}
			if err := queueBudget(connection, state.Streams[connection.stream]); err != nil {
				closed[id] = err
			}
		}
		return nil
	})
	if err != nil {
		service.failed = true
		for _, connection := range service.sessions {
			if connection.err == nil {
				connection.err = domain.ErrUnavailable
			}
			signal(connection)
		}
		return err
	}
	service.failed = false
	for id, connection := range service.sessions {
		if reason, ok := closed[id]; ok {
			connection.err = reason
		}
		signal(connection)
	}
	return nil
}
func signal(connection *connection) {
	select {
	case connection.changed <- struct{}{}:
	default:
	}
}
func queueBudget(connection *connection, stream domain.Stream) error {
	count, bytes := 0, 0
	if connection.inflight != nil && connection.inflight.Envelope.Sequence <= connection.replayUpper {
		count++
		bytes += connection.inflight.WireSize()
	}
	for _, frame := range stream.Frames {
		if frame.Envelope.Sequence <= connection.replayUpper || frame.Envelope.Sequence < connection.next {
			continue
		}
		count++
		bytes += frame.WireSize()
	}
	if count > domain.MaxQueueEvents || bytes > domain.MaxQueueBytes {
		return domain.ErrReset
	}
	if connection.next <= stream.LastSequence && !hasFrame(stream, connection.next) {
		return domain.ErrReset
	}
	return nil
}

// Next exposes one durable envelope. Delivered must acknowledge its exact cursor
// after the transport writes it; an in-flight frame remains inside queue bounds.
func (subscription *Subscription) Next(ctx context.Context) (*domain.Envelope, error) {
	service := subscription.service
	service.mu.Lock()
	defer service.mu.Unlock()
	connection, ok := service.sessions[subscription.id]
	if !ok {
		return nil, domain.ErrClosed
	}
	if connection.err != nil {
		return nil, connection.err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if connection.ctx.Err() != nil {
		connection.err = domain.ErrClosed
		return nil, connection.err
	}
	if !service.cfg.Now().Before(connection.openedAt.Add(domain.ConnectionLifetime)) {
		connection.err = domain.ErrClosed
		return nil, connection.err
	}
	if err := service.check(connection.ctx, connection.identity); err != nil {
		connection.err = err
		return nil, err
	}
	if connection.inflight != nil {
		return nil, domain.ErrBusy
	}
	state, err := service.cfg.Journal.Snapshot(ctx)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	stream, ok := state.Streams[connection.stream]
	if !ok || !validState(state) {
		connection.err = domain.ErrReset
		return nil, connection.err
	}
	if err := queueBudget(connection, stream); err != nil {
		connection.err = err
		return nil, err
	}
	if connection.next > stream.LastSequence {
		return nil, nil
	}
	for _, frame := range stream.Frames {
		if frame.Envelope.Sequence != connection.next {
			continue
		}
		if service.validateFrame(frame, stream.ID) != nil {
			connection.err = domain.ErrReset
			return nil, connection.err
		}
		visible, err := service.cfg.Authorizer.Visible(connection.ctx, connection.identity, frame.Subject)
		if err != nil {
			connection.err = domain.ErrUnavailable
			return nil, connection.err
		}
		if !visible {
			connection.err = domain.ErrReset
			return nil, connection.err
		}
		if err := service.check(connection.ctx, connection.identity); err != nil {
			connection.err = err
			return nil, err
		}
		frame.Envelope.Data = append(json.RawMessage(nil), frame.Envelope.Data...)
		connection.inflight = &frame
		if err := queueBudget(connection, stream); err != nil {
			connection.inflight = nil
			connection.err = err
			return nil, err
		}
		envelope := frame.Envelope
		envelope.Data = append(json.RawMessage(nil), envelope.Data...)
		return &envelope, nil
	}
	connection.err = domain.ErrReset
	return nil, connection.err
}
func (subscription *Subscription) Delivered(cursor string) error {
	service := subscription.service
	service.mu.Lock()
	defer service.mu.Unlock()
	connection, ok := service.sessions[subscription.id]
	if !ok {
		return domain.ErrClosed
	}
	if connection.err != nil {
		return connection.err
	}
	if connection.inflight == nil || connection.inflight.Envelope.Cursor != cursor {
		return domain.ErrConflict
	}
	connection.next++
	connection.inflight = nil
	return nil
}
func (subscription *Subscription) Changed() <-chan struct{} {
	service := subscription.service
	service.mu.Lock()
	defer service.mu.Unlock()
	if connection, ok := service.sessions[subscription.id]; ok {
		return connection.changed
	}
	closed := make(chan struct{})
	close(closed)
	return closed
}
func (subscription *Subscription) Close() {
	service := subscription.service
	service.mu.Lock()
	defer service.mu.Unlock()
	if connection, ok := service.sessions[subscription.id]; ok {
		connection.err = domain.ErrClosed
		signal(connection)
		delete(service.sessions, subscription.id)
	}
}

func (service *Service) publicationFailed() {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.failed = true
	for _, connection := range service.sessions {
		if connection.err == nil {
			connection.err = domain.ErrUnavailable
		}
		signal(connection)
	}
}
