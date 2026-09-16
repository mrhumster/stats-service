package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/common-nighthawk/go-figure"
	"github.com/hibiken/asynq"
	"github.com/mrhumster/stats-service/config"
	"github.com/mrhumster/stats-service/internal/database"
	"github.com/mrhumster/stats-service/internal/delivery/http/routes"
	"github.com/mrhumster/stats-service/internal/queue"
	"github.com/mrhumster/stats-service/internal/repository"
	"github.com/mrhumster/stats-service/internal/service"
	"github.com/mrhumster/stats-service/internal/stream"
)

var (
	version   = "dev"
	buildDate = "unknown"
)

func main() {
	figure.NewFigure("stats "+version, "graffiti", true).Print()

	opts := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
	slog.Info("Start stats reader", "version", version, "build_date", buildDate)

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("error load config: %v", err)
	}

	db, err := database.SetupDatabase(cfg)
	if err != nil {
		log.Fatalf("error open database: %v", err)
	}

	tokens, err := service.NewTokenService(cfg.JWT.AccessPublicKeyURL)
	if err != nil {
		log.Fatalf("error init token service: %v", err)
	}

	recorder := queue.NewAsyncActivityRecorder(asynq.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.QueueDB,
	})
	defer func() {
		if err := recorder.Close(); err != nil {
			log.Printf("close activity recorder: %v", err)
		}
	}()

	repo := repository.NewGormStatsRepository(db)
	svc := service.NewStatsServiceImpl(repo)
	svc.WithActivityRecorder(recorder)
	svc.WithStreamStatusClient(stream.NewHTTPStatusClient(cfg.Stream.BaseURL))

	r := routes.SetupRoutes(db, cfg, svc, tokens)

	srv := &http.Server{
		Addr:         cfg.Server.ServerAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	defer func() {
		sqlDB, err := db.DB()
		if err != nil {
			log.Printf("failed to get sql.DB: %s", err.Error())
			return
		}
		if err := sqlDB.Close(); err != nil {
			log.Printf("database pool closed")
		}
	}()

	go func() {
		log.Printf("stats reader listening on %s", cfg.Server.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down stats reader...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}