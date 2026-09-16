package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mrhumster/stats-service/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SetupDatabase opens the stats Postgres connection pool. Schema migrations
// are owned by db-migrate (target `stats`), not by gorm AutoMigrate.
func SetupDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open stats database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("stats database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctxPing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctxPing); err != nil {
		return nil, fmt.Errorf("ping stats database: %w", err)
	}

	slog.Info("stats database connected", "db", cfg.Database.Name)
	return db, nil
}