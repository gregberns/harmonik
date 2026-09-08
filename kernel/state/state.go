// Package state is the kernel's box-local, durable append-only journal.
//
// A journal is identified by (namespace, journal name). Namespace comes from
// the caller's kernel-registered plugin identity — every exported method
// takes it as a Go parameter, never as a field a request can set, so a
// plugin cannot name its way into another plugin's journal. Sequence numbers
// are monotonic per (namespace, journal name) and survive a restart.
// JournalAppendRequest.sync controls whether a call fsyncs its records
// before returning; JournalAppendResponse.synced reports whether that
// request was honoured.
//
// The store is modernc.org/sqlite in WAL mode — pure Go, no cgo, so this
// package builds with CGO_ENABLED=0. KV lives in a later slice: nothing here
// reads or writes a key/value pair.
package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"google.golang.org/protobuf/types/known/timestamppb"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// ErrEmptyNamespace is returned when Append or Read is called with an empty
// namespace. The kernel derives namespace from plugin registration; an empty
// value here means the caller skipped that step.
var ErrEmptyNamespace = errors.New("state: empty namespace")

// ErrEmptyJournal is returned when a request names no journal.
var ErrEmptyJournal = errors.New("state: empty journal name")

const schemaDDL = `
CREATE TABLE IF NOT EXISTS journal_record (
	namespace   TEXT    NOT NULL,
	journal     TEXT    NOT NULL,
	seq         INTEGER NOT NULL,
	record      BLOB    NOT NULL,
	appended_at INTEGER NOT NULL,
	PRIMARY KEY (namespace, journal, seq)
);`

// State is the box-local SQLite-backed journal store. The zero value is not
// usable; construct one with Open.
type State struct {
	db *sql.DB

	mu       sync.Mutex // serializes Append and guards watchers
	watchers map[journalKey][]chan struct{}
}

type journalKey struct {
	namespace string
	journal   string
}

// Open opens (creating if absent) a SQLite database at path in WAL mode and
// prepares its schema. path must name a real file — an empty path defeats
// the restart-durability guarantee this package exists for.
func Open(path string) (*State, error) {
	if path == "" {
		return nil, errors.New("state: empty database path")
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("state: open %q: %w", path, err)
	}
	// One connection, reused for every call: database/sql then serializes
	// callers for us, so PRAGMA synchronous set ahead of a write always
	// applies to that same write's transaction on the same session.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), schemaDDL); err != nil {
		return nil, errors.Join(fmt.Errorf("state: create schema: %w", err), db.Close())
	}

	return &State{db: db, watchers: make(map[journalKey][]chan struct{})}, nil
}

// Close releases the underlying database handle.
func (s *State) Close() error {
	return s.db.Close()
}

// Append writes req.GetRecords() to namespace's copy of req.GetJournal(),
// in order, each getting the next sequence number after whatever that
// journal already holds — including across a restart. When req.GetSync() is
// true, Append does not return until the write is fsynced.
func (s *State) Append(ctx context.Context, namespace string, req *kernelv1.JournalAppendRequest) (resp *kernelv1.JournalAppendResponse, err error) {
	if namespace == "" {
		return nil, ErrEmptyNamespace
	}
	if req.GetJournal() == "" {
		return nil, ErrEmptyJournal
	}
	records := req.GetRecords()
	if len(records) == 0 {
		return &kernelv1.JournalAppendResponse{Synced: true}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	wantSync := req.GetSync()
	pragma := "NORMAL"
	if wantSync {
		pragma = "FULL"
	}
	if _, err = s.db.ExecContext(ctx, "PRAGMA synchronous="+pragma); err != nil {
		return nil, fmt.Errorf("state: set synchronous=%s: %w", pragma, err)
	}

	seqs, err := s.appendTx(ctx, namespace, req.GetJournal(), records)
	if err != nil {
		return nil, err
	}

	s.wakeLocked(journalKey{namespace: namespace, journal: req.GetJournal()})

	return &kernelv1.JournalAppendResponse{Seqs: seqs, Synced: wantSync}, nil
}

// appendTx runs the read-max-seq-then-insert sequence in one transaction so
// two concurrent Append calls can never hand out the same seq. Callers must
// hold s.mu.
func (s *State) appendTx(ctx context.Context, namespace, journal string, records [][]byte) (seqs []uint64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("state: begin append: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = fmt.Errorf("%w (rollback also failed: %w)", err, rbErr)
			}
		}
	}()

	var maxSeq uint64
	row := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM journal_record WHERE namespace = ? AND journal = ?`, namespace, journal)
	if err = row.Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("state: read max seq: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO journal_record (namespace, journal, seq, record, appended_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("state: prepare insert: %w", err)
	}
	defer func() {
		if closeErr := stmt.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("state: close insert statement: %w", closeErr)
		}
	}()

	now := time.Now().UnixNano()
	seqs = make([]uint64, len(records))
	for i, rec := range records {
		maxSeq++
		if _, err = stmt.ExecContext(ctx, namespace, journal, maxSeq, rec, now); err != nil {
			return nil, fmt.Errorf("state: insert record: %w", err)
		}
		seqs[i] = maxSeq
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("state: commit append: %w", err)
	}
	return seqs, nil
}

// Read emits every record namespace's copy of req.GetJournal() holds after
// req.GetAfterSeq(), in sequence order, up to req.GetLimit() records (0
// means unlimited). When req.GetFollow() is true, Read then blocks and
// keeps emitting records appended after it started, until ctx is done.
func (s *State) Read(ctx context.Context, namespace string, req *kernelv1.JournalReadRequest, emit func(*kernelv1.JournalRecord) error) error {
	if namespace == "" {
		return ErrEmptyNamespace
	}
	if req.GetJournal() == "" {
		return ErrEmptyJournal
	}

	key := journalKey{namespace: namespace, journal: req.GetJournal()}

	var watchCh chan struct{}
	if req.GetFollow() {
		var cancelWatch func()
		watchCh, cancelWatch = s.watch(key)
		defer cancelWatch()
	}

	afterSeq, err := s.emitFrom(ctx, namespace, req.GetJournal(), req.GetAfterSeq(), int(req.GetLimit()), emit)
	if err != nil || !req.GetFollow() {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-watchCh:
		}
		if afterSeq, err = s.emitFrom(ctx, namespace, req.GetJournal(), afterSeq, 0, emit); err != nil {
			return err
		}
	}
}

// emitFrom queries every record after afterSeq (capped at limit when > 0)
// and calls emit for each, in seq order. It returns the highest seq emitted,
// or afterSeq unchanged when nothing matched.
func (s *State) emitFrom(ctx context.Context, namespace, journal string, afterSeq uint64, limit int, emit func(*kernelv1.JournalRecord) error) (newAfter uint64, err error) {
	query := `SELECT seq, record, appended_at FROM journal_record WHERE namespace = ? AND journal = ? AND seq > ? ORDER BY seq ASC`
	args := []any{namespace, journal, afterSeq}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return afterSeq, fmt.Errorf("state: read %q: %w", journal, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("state: close read cursor: %w", closeErr)
		}
	}()

	newAfter = afterSeq
	for rows.Next() {
		var seq uint64
		var record []byte
		var appendedAtNanos int64
		if err = rows.Scan(&seq, &record, &appendedAtNanos); err != nil {
			return newAfter, fmt.Errorf("state: scan record: %w", err)
		}
		if err := emit(&kernelv1.JournalRecord{
			Seq:        seq,
			Record:     record,
			AppendedAt: timestamppb.New(time.Unix(0, appendedAtNanos)),
		}); err != nil {
			return newAfter, err
		}
		newAfter = seq
	}
	if err = rows.Err(); err != nil {
		return newAfter, fmt.Errorf("state: iterate records: %w", err)
	}
	return newAfter, nil
}

// watch registers a wake channel for key. The returned cancel func removes
// it; callers must call cancel once done watching.
func (s *State) watch(key journalKey) (ch chan struct{}, cancel func()) {
	ch = make(chan struct{}, 1)

	s.mu.Lock()
	s.watchers[key] = append(s.watchers[key], ch)
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		chs := s.watchers[key]
		for i, c := range chs {
			if c == ch {
				s.watchers[key] = append(chs[:i], chs[i+1:]...)
				break
			}
		}
	}
}

// wakeLocked notifies every watcher registered for key. Callers must hold
// s.mu.
func (s *State) wakeLocked(key journalKey) {
	for _, ch := range s.watchers[key] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
