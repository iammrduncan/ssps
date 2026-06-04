package storage

/*
#cgo LDFLAGS: -lsqlite3
#include <stdlib.h>
#include <sqlite3.h>

static int ssps_bind_text(sqlite3_stmt *stmt, int idx, char *value) {
	return sqlite3_bind_text(stmt, idx, value, -1, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unsafe"
)

type Store struct {
	mu sync.Mutex
	db *C.sqlite3
}

type CheckpointMode string

const (
	CheckpointPassive  CheckpointMode = "PASSIVE"
	CheckpointFull     CheckpointMode = "FULL"
	CheckpointRestart  CheckpointMode = "RESTART"
	CheckpointTruncate CheckpointMode = "TRUNCATE"
)

type CheckpointStats struct {
	Busy               bool
	LogFrames          int64
	CheckpointedFrames int64
}

type VisitBatch struct {
	SiteID     int64
	Hits       int64
	VisitorIDs []string
}

type SiteStats struct {
	SiteID         int64
	TotalHits      int64
	UniqueVisitors int64
}

type StoredStats struct {
	IDsCreated  int64
	TotalVisits int64
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("storage path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var db *C.sqlite3
	flags := C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX
	if rc := C.sqlite3_open_v2(cpath, &db, C.int(flags), nil); rc != C.SQLITE_OK {
		err := sqliteError(db, "open sqlite")
		if db != nil {
			C.sqlite3_close(db)
		}
		return nil, err
	}

	store := &Store{db: db}
	if err := store.configure(context.Background()); err != nil {
		store.Close()
		return nil, err
	}
	if err := store.migrate(context.Background()); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if rc := C.sqlite3_close(s.db); rc != C.SQLITE_OK {
		return sqliteError(s.db, "close sqlite")
	}
	s.db = nil
	return nil
}

func (s *Store) CreateSite(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	stmt, err := s.prepare(`INSERT INTO sites (created_at) VALUES (?)`)
	if err != nil {
		return 0, err
	}
	defer C.sqlite3_finalize(stmt)

	if err := bindText(stmt, 1, now); err != nil {
		return 0, err
	}
	if err := stepDone(stmt, "insert site"); err != nil {
		return 0, err
	}
	id := int64(C.sqlite3_last_insert_rowid(s.db))
	return id, nil
}

func (s *Store) ApplyVisitBatch(ctx context.Context, batches []VisitBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(batches) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.exec(`BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.exec(`ROLLBACK`)
		}
	}()

	ensureCounterStmt, err := s.prepare(`
		INSERT INTO site_counters (site_id, total_hits, unique_visitors, updated_at)
		VALUES (?, 0, 0, ?)
		ON CONFLICT(site_id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(ensureCounterStmt)

	insertVisitorStmt, err := s.prepare(`
		INSERT OR IGNORE INTO site_visitors (site_id, visitor_id, first_seen_at)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(insertVisitorStmt)

	updateCounterStmt, err := s.prepare(`
		UPDATE site_counters
		SET total_hits = total_hits + ?,
		    unique_visitors = unique_visitors + ?,
		    updated_at = ?
		WHERE site_id = ?
	`)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(updateCounterStmt)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, batch := range batches {
		if batch.SiteID < 0 || batch.Hits <= 0 {
			continue
		}

		if err := s.execStmt(ensureCounterStmt, "ensure counter", bindInt64Arg(batch.SiteID), bindTextArg(now)); err != nil {
			return fmt.Errorf("ensure counter for site %d: %w", batch.SiteID, err)
		}

		insertedVisitors := int64(0)
		for _, visitorID := range uniqueStrings(batch.VisitorIDs) {
			if err := s.execStmt(insertVisitorStmt, "insert visitor", bindInt64Arg(batch.SiteID), bindTextArg(visitorID), bindTextArg(now)); err != nil {
				return fmt.Errorf("insert visitor for site %d: %w", batch.SiteID, err)
			}
			insertedVisitors += int64(C.sqlite3_changes(s.db))
		}

		if err := s.execStmt(updateCounterStmt, "update counter", bindInt64Arg(batch.Hits), bindInt64Arg(insertedVisitors), bindTextArg(now), bindInt64Arg(batch.SiteID)); err != nil {
			return fmt.Errorf("update counter for site %d: %w", batch.SiteID, err)
		}
	}

	if err := s.exec(`COMMIT`); err != nil {
		return fmt.Errorf("commit visit batch: %w", err)
	}
	committed = true
	return nil
}

func (s *Store) SiteStats(ctx context.Context, siteID int64) (SiteStats, error) {
	if err := ctx.Err(); err != nil {
		return SiteStats{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stats := SiteStats{SiteID: siteID}
	stmt, err := s.prepare(`
		SELECT total_hits, unique_visitors
		FROM site_counters
		WHERE site_id = ?
	`)
	if err != nil {
		return SiteStats{}, err
	}
	defer C.sqlite3_finalize(stmt)

	if err := bindInt64(stmt, 1, siteID); err != nil {
		return SiteStats{}, err
	}
	rc := C.sqlite3_step(stmt)
	if rc == C.SQLITE_DONE {
		return stats, nil
	}
	if rc != C.SQLITE_ROW {
		return SiteStats{}, sqliteError(s.db, "read site stats")
	}
	stats.TotalHits = int64(C.sqlite3_column_int64(stmt, 0))
	stats.UniqueVisitors = int64(C.sqlite3_column_int64(stmt, 1))
	return stats, nil
}

func (s *Store) StoredStats(ctx context.Context) (StoredStats, error) {
	if err := ctx.Err(); err != nil {
		return StoredStats{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var stats StoredStats
	idsCreated, err := s.queryInt64(`SELECT COUNT(*) FROM sites`)
	if err != nil {
		return StoredStats{}, fmt.Errorf("count sites: %w", err)
	}
	totalVisits, err := s.queryInt64(`SELECT COALESCE(SUM(total_hits), 0) FROM site_counters`)
	if err != nil {
		return StoredStats{}, fmt.Errorf("sum visits: %w", err)
	}
	stats.IDsCreated = idsCreated
	stats.TotalVisits = totalVisits
	return stats, nil
}

func (s *Store) Checkpoint(ctx context.Context, mode CheckpointMode) (CheckpointStats, error) {
	if err := ctx.Err(); err != nil {
		return CheckpointStats{}, err
	}
	if mode == "" {
		mode = CheckpointPassive
	}
	if !validCheckpointMode(mode) {
		return CheckpointStats{}, fmt.Errorf("unknown checkpoint mode %q", mode)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stmt, err := s.prepare(fmt.Sprintf(`PRAGMA wal_checkpoint(%s)`, mode))
	if err != nil {
		return CheckpointStats{}, err
	}
	defer C.sqlite3_finalize(stmt)

	rc := C.sqlite3_step(stmt)
	if rc == C.SQLITE_DONE {
		return CheckpointStats{}, nil
	}
	if rc != C.SQLITE_ROW {
		return CheckpointStats{}, sqliteError(s.db, "checkpoint wal")
	}
	return CheckpointStats{
		Busy:               C.sqlite3_column_int64(stmt, 0) != 0,
		LogFrames:          int64(C.sqlite3_column_int64(stmt, 1)),
		CheckpointedFrames: int64(C.sqlite3_column_int64(stmt, 2)),
	}, nil
}

func (s *Store) Compact(ctx context.Context, pages int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pages <= 0 {
		pages = 1000
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureIncrementalVacuum(); err != nil {
		return err
	}
	if err := s.exec(fmt.Sprintf(`PRAGMA incremental_vacuum(%d)`, pages)); err != nil {
		return fmt.Errorf("incremental vacuum: %w", err)
	}
	stmt, err := s.prepare(`PRAGMA wal_checkpoint(TRUNCATE)`)
	if err != nil {
		return err
	}
	rc := C.sqlite3_step(stmt)
	finalizeRC := C.sqlite3_finalize(stmt)
	if rc != C.SQLITE_ROW && rc != C.SQLITE_DONE {
		return sqliteError(s.db, "truncate wal")
	}
	if finalizeRC != C.SQLITE_OK {
		return sqliteError(s.db, "finalize truncate wal")
	}
	if err := s.exec(`PRAGMA optimize`); err != nil {
		return fmt.Errorf("optimize sqlite: %w", err)
	}
	return nil
}

func (s *Store) ensureIncrementalVacuum() error {
	autoVacuum, err := s.queryInt64(`PRAGMA auto_vacuum`)
	if err != nil {
		return fmt.Errorf("read auto vacuum: %w", err)
	}
	if autoVacuum == 2 {
		return nil
	}
	if err := s.exec(`PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
		return fmt.Errorf("enable incremental vacuum: %w", err)
	}
	if err := s.exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum sqlite for incremental vacuum: %w", err)
	}
	return nil
}

func (s *Store) RunMaintenance(ctx context.Context, checkpointInterval time.Duration, compactInterval time.Duration) {
	if checkpointInterval <= 0 {
		checkpointInterval = 5 * time.Minute
	}
	if compactInterval <= 0 {
		compactInterval = 24 * time.Hour
	}

	checkpointTicker := time.NewTicker(checkpointInterval)
	compactTicker := time.NewTicker(compactInterval)
	defer checkpointTicker.Stop()
	defer compactTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if _, err := s.Checkpoint(shutdownCtx, CheckpointTruncate); err != nil {
				slog.Error("checkpoint sqlite during shutdown", "error", err)
			}
			cancel()
			return
		case <-checkpointTicker.C:
			stats, err := s.Checkpoint(ctx, CheckpointPassive)
			if err != nil {
				slog.Error("checkpoint sqlite", "error", err)
				continue
			}
			if stats.Busy {
				slog.Debug("sqlite checkpoint busy", "logFrames", stats.LogFrames, "checkpointedFrames", stats.CheckpointedFrames)
			}
		case <-compactTicker.C:
			if err := s.Compact(ctx, 1000); err != nil {
				slog.Error("compact sqlite", "error", err)
			}
		}
	}
}

func (s *Store) configure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pragmas := []string{
		`PRAGMA page_size = 4096`,
		`PRAGMA auto_vacuum = INCREMENTAL`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`PRAGMA wal_autocheckpoint = 0`,
		`PRAGMA journal_size_limit = 67108864`,
		`PRAGMA temp_store = MEMORY`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	}
	for _, pragma := range pragmas {
		if err := s.exec(pragma); err != nil {
			return fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS sites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS site_counters (
			site_id INTEGER PRIMARY KEY,
			total_hits INTEGER NOT NULL DEFAULT 0,
			unique_visitors INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS site_visitors (
			site_id INTEGER NOT NULL,
			visitor_id TEXT NOT NULL,
			first_seen_at TEXT NOT NULL,
			PRIMARY KEY (site_id, visitor_id)
		)`,
	}
	for _, statement := range statements {
		if err := s.exec(statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

type bindArg struct {
	int64Value int64
	textValue  string
	kind       string
}

func bindInt64Arg(value int64) bindArg {
	return bindArg{kind: "int64", int64Value: value}
}

func bindTextArg(value string) bindArg {
	return bindArg{kind: "text", textValue: value}
}

func (s *Store) exec(sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var errmsg *C.char
	rc := C.sqlite3_exec(s.db, csql, nil, nil, &errmsg)
	if rc != C.SQLITE_OK {
		if errmsg != nil {
			defer C.sqlite3_free(unsafe.Pointer(errmsg))
			return fmt.Errorf("%s: %s", sql, C.GoString(errmsg))
		}
		return sqliteError(s.db, sql)
	}
	return nil
}

func (s *Store) execPrepared(sql string, args ...bindArg) error {
	stmt, err := s.prepare(sql)
	if err != nil {
		return err
	}
	defer C.sqlite3_finalize(stmt)

	for idx, arg := range args {
		if err := bind(stmt, idx+1, arg); err != nil {
			return err
		}
	}
	return stepDone(stmt, sql)
}

func (s *Store) execStmt(stmt *C.sqlite3_stmt, action string, args ...bindArg) error {
	for idx, arg := range args {
		if err := bind(stmt, idx+1, arg); err != nil {
			_ = resetStmt(stmt)
			return err
		}
	}

	stepErr := stepDone(stmt, action)
	resetErr := resetStmt(stmt)
	if stepErr != nil {
		return stepErr
	}
	return resetErr
}

func resetStmt(stmt *C.sqlite3_stmt) error {
	resetRC := C.sqlite3_reset(stmt)
	clearRC := C.sqlite3_clear_bindings(stmt)
	if resetRC != C.SQLITE_OK {
		return fmt.Errorf("reset sqlite statement: sqlite code %d", int(resetRC))
	}
	if clearRC != C.SQLITE_OK {
		return fmt.Errorf("clear sqlite bindings: sqlite code %d", int(clearRC))
	}
	return nil
}

func (s *Store) prepare(sql string) (*C.sqlite3_stmt, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var stmt *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(s.db, csql, -1, &stmt, nil); rc != C.SQLITE_OK {
		return nil, sqliteError(s.db, "prepare")
	}
	return stmt, nil
}

func (s *Store) queryInt64(sql string) (int64, error) {
	stmt, err := s.prepare(sql)
	if err != nil {
		return 0, err
	}
	defer C.sqlite3_finalize(stmt)

	rc := C.sqlite3_step(stmt)
	if rc == C.SQLITE_DONE {
		return 0, nil
	}
	if rc != C.SQLITE_ROW {
		return 0, sqliteError(s.db, sql)
	}
	return int64(C.sqlite3_column_int64(stmt, 0)), nil
}

func (s *Store) queryText(sql string) (string, error) {
	stmt, err := s.prepare(sql)
	if err != nil {
		return "", err
	}
	defer C.sqlite3_finalize(stmt)

	rc := C.sqlite3_step(stmt)
	if rc == C.SQLITE_DONE {
		return "", nil
	}
	if rc != C.SQLITE_ROW {
		return "", sqliteError(s.db, sql)
	}
	text := C.sqlite3_column_text(stmt, 0)
	if text == nil {
		return "", nil
	}
	return C.GoString((*C.char)(unsafe.Pointer(text))), nil
}

func bind(stmt *C.sqlite3_stmt, idx int, arg bindArg) error {
	switch arg.kind {
	case "int64":
		return bindInt64(stmt, idx, arg.int64Value)
	case "text":
		return bindText(stmt, idx, arg.textValue)
	default:
		return errors.New("unknown sqlite bind argument")
	}
}

func bindInt64(stmt *C.sqlite3_stmt, idx int, value int64) error {
	if rc := C.sqlite3_bind_int64(stmt, C.int(idx), C.sqlite3_int64(value)); rc != C.SQLITE_OK {
		return fmt.Errorf("bind int64: sqlite code %d", int(rc))
	}
	return nil
}

func bindText(stmt *C.sqlite3_stmt, idx int, value string) error {
	cvalue := C.CString(value)
	defer C.free(unsafe.Pointer(cvalue))
	if rc := C.ssps_bind_text(stmt, C.int(idx), cvalue); rc != C.SQLITE_OK {
		return fmt.Errorf("bind text: sqlite code %d", int(rc))
	}
	return nil
}

func stepDone(stmt *C.sqlite3_stmt, action string) error {
	if rc := C.sqlite3_step(stmt); rc != C.SQLITE_DONE {
		return fmt.Errorf("%s: sqlite code %d", action, int(rc))
	}
	return nil
}

func sqliteError(db *C.sqlite3, action string) error {
	if db == nil {
		return fmt.Errorf("%s: sqlite unavailable", action)
	}
	return fmt.Errorf("%s: %s", action, C.GoString(C.sqlite3_errmsg(db)))
}

func validCheckpointMode(mode CheckpointMode) bool {
	switch mode {
	case CheckpointPassive, CheckpointFull, CheckpointRestart, CheckpointTruncate:
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
