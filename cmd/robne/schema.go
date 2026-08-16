package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/migrations"
)

func newEmbeddedMigrate(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return m, nil
}

func embeddedHead() (uint, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("embedded migrations: %w", err)
	}
	defer src.Close()
	first, err := src.First()
	if err != nil {
		return 0, fmt.Errorf("embedded migrations first: %w", err)
	}
	last := first
	for {
		next, err := src.Next(last)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return last, nil
			}
			return 0, err
		}
		last = next
	}
}

func readSchemaStatus(m *migrate.Migrate) (schemaStatus, error) {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return schemaStatus{Empty: true}, nil
	}
	if err != nil {
		return schemaStatus{}, err
	}
	return schemaStatus{Version: v, Dirty: dirty}, nil
}

func openPostgres(ctx context.Context, dsn string, apply bool) (*pgxpool.Pool, error) {
	m, err := newEmbeddedMigrate(dsn)
	if err != nil {
		return nil, err
	}
	defer func() {
		srcErr, dbErr := m.Close()
		_ = srcErr
		_ = dbErr
	}()

	st, err := readSchemaStatus(m)
	if err != nil {
		return nil, fmt.Errorf("schema version: %w", err)
	}
	head, err := embeddedHead()
	if err != nil {
		return nil, err
	}
	needUp, err := schemaPlan(st, head, apply)
	if err != nil {
		return nil, err
	}
	if needUp {
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return pool, nil
}
