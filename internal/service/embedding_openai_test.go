package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAICompatibleEmbeddingProviderSupportsSelfHostedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request path=%q auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "nomic" || body["dimensions"] != float64(2) {
			t.Fatalf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": []float32{0.25, 0.75}}}})
	}))
	defer server.Close()

	provider := NewOpenAICompatibleEmbeddingProvider(OpenAICompatibleEmbeddingConfig{Model: "nomic", BaseURL: server.URL + "/v1", APIKey: "secret", Dimensions: 2, Timeout: time.Second})
	vector, err := provider.Embed(context.Background(), "query")
	if err != nil {
		t.Fatal(err)
	}
	if len(vector) != 2 || vector[1] != 0.75 {
		t.Fatalf("vector=%v", vector)
	}
}
