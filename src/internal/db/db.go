package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
	mu  sync.Mutex
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	d := &DB{SQL: sqlDB}
	if err := d.Migrate(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.SQL.Close()
}

func (d *DB) WithLock(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fn()
}

func (d *DB) Ping() error {
	return d.SQL.Ping()
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}
