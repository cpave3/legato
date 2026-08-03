package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleEmbeddingConfig struct {
	Model      string
	BaseURL    string
	APIKey     string
	Dimensions int
	Timeout    time.Duration
}

type OpenAICompatibleEmbeddingProvider struct {
	config OpenAICompatibleEmbeddingConfig
	client *http.Client
}

func NewOpenAICompatibleEmbeddingProvider(config OpenAICompatibleEmbeddingConfig) *OpenAICompatibleEmbeddingProvider {
	return &OpenAICompatibleEmbeddingProvider{config: config, client: &http.Client{Timeout: config.Timeout}}
}

func (p *OpenAICompatibleEmbeddingProvider) Model() string { return p.config.Model }

func (p *OpenAICompatibleEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	body := map[string]any{"model": p.config.Model, "input": text}
	if p.config.Dimensions > 0 {
		body["dimensions"] = p.config.Dimensions
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.config.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding request returned %s", resp.Status)
	}
	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) != 1 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embedding response did not contain one vector")
	}
	if p.config.Dimensions > 0 && len(result.Data[0].Embedding) != p.config.Dimensions {
		return nil, fmt.Errorf("embedding dimensions = %d, want %d", len(result.Data[0].Embedding), p.config.Dimensions)
	}
	return result.Data[0].Embedding, nil
}
