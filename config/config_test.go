package config_test

import (
	"testing"

	"github.com/mrhumster/stats-service/config"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := config.LoadConfig()
	require.NoError(t, err)

	require.Equal(t, ":8080", cfg.Server.ServerAddr)
	require.Equal(t, "debug", cfg.Server.Mode)
	require.Equal(t, "stats", cfg.Database.Name)
	require.Equal(t, "localhost", cfg.Database.Host)
	require.Equal(t, 3, cfg.Redis.QueueDB)
	require.Equal(t, "http://stream-service:80", cfg.Stream.BaseURL)
}

func TestLoadConfig_ReadsEnv(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("MODE", "release")
	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_NAME", "gpdb")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://a.com, https://b.com,")
	t.Setenv("REDIS_QUEUE_DB", "7")
	t.Setenv("STREAM_SERVICE_URL", "http://stream:8080")

	cfg, err := config.LoadConfig()
	require.NoError(t, err)

	require.Equal(t, ":9090", cfg.Server.ServerAddr)
	require.Equal(t, "release", cfg.Server.Mode)
	require.Equal(t, "db", cfg.Database.Host)
	require.Equal(t, "gpdb", cfg.Database.Name)
	require.Equal(t, []string{"https://a.com", "https://b.com"}, cfg.Server.AllowedOrigins)
	require.Equal(t, 7, cfg.Redis.QueueDB)
	require.Equal(t, "http://stream:8080", cfg.Stream.BaseURL)
}