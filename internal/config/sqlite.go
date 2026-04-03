//go:build linux

package config

import (
	"database/sql"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	_ "modernc.org/sqlite"
)

func NewSQLite(v *viper.Viper, log *logrus.Logger) *sql.DB {
	dbPath := v.GetString("database.path")
	if dbPath == "" {
		dbPath = "moxy.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.WithError(err).Fatal("failed to open SQLite database")
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.WithError(err).Warn("failed to enable WAL mode")
	}

	// Create tables (idempotent — safe to run every startup)
	if err := createTables(db); err != nil {
		log.WithError(err).Fatal("failed to create database tables")
	}

	log.Infof("SQLite database opened: %s", dbPath)
	return db
}

func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS devices (
			id VARCHAR(36) PRIMARY KEY,
			serial VARCHAR(100) NOT NULL UNIQUE,
			alias VARCHAR(50) NOT NULL UNIQUE,
			carrier VARCHAR(100) DEFAULT '',
			interface VARCHAR(50) DEFAULT '',
			nameserver VARCHAR(255) DEFAULT '',
			nat64_prefix VARCHAR(100) DEFAULT '',
			status VARCHAR(20) DEFAULT 'offline',
			max_slots INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	return nil
}
