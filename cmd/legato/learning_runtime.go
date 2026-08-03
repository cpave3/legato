package main

import (
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func configuredLearningService(cfg *config.Config, db *store.Store, bus *events.Bus, resolver ...*service.ProjectIdentityResolver) *service.LearningService {
	svc := service.NewLearningService(db, bus, resolver...)
	if cfg == nil || !cfg.Learning.Embedding.Enabled {
		return svc
	}
	timeout, _ := time.ParseDuration(cfg.Learning.Embedding.Timeout)
	provider := service.NewOpenAICompatibleEmbeddingProvider(service.OpenAICompatibleEmbeddingConfig{
		Model: cfg.Learning.Embedding.Model, BaseURL: cfg.Learning.Embedding.BaseURL,
		APIKey: os.Getenv(cfg.Learning.Embedding.APIKeyEnv), Dimensions: cfg.Learning.Embedding.Dimensions, Timeout: timeout,
	})
	svc.ConfigureEmbeddings(provider, db)
	return svc
}
