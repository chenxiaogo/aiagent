package database

import (
	"fmt"
	"strings"
	"time"

	"aiagent/pkg/app/config"
	"aiagent/pkg/ilog"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// New 根据配置初始化 GORM，支持 postgres（pgvector）。
func New(conf *config.DatabaseConfig) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch strings.ToLower(conf.Driver) {
	case "postgres", "pgvector":
		dialector = postgres.Open(conf.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s (only postgres/pgvector supported)", conf.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormLogger.Default.LogMode(parseLogLevel(conf.LogLevel)),
	})
	if err != nil {
		return nil, fmt.Errorf("open database(%s): %w", conf.Driver, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	sqlDB.SetMaxIdleConns(conf.MaxIdleConns)
	sqlDB.SetMaxOpenConns(conf.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(conf.ConnMaxLifetime)

	// 启用 pgvector 扩展
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		ilog.Warnf("pgvector extension may not be available: %v", err)
	} else {
		ilog.Info("pgvector extension enabled")
	}

	return db, nil
}

const (
	DefaultMaxIdleConns    = 10
	DefaultMaxOpenConns    = 100
	DefaultConnMaxLifetime = 2 * time.Hour
)

// parseLogLevel 将配置的字符串映射为 GORM 日志级别，默认 warn（只打印错误和慢查询）。
func parseLogLevel(s string) gormLogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "silent":
		return gormLogger.Silent
	case "error":
		return gormLogger.Error
	case "info":
		return gormLogger.Info
	case "warn":
		return gormLogger.Warn
	default:
		return gormLogger.Warn
	}
}