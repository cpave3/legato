package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestReviewPassCommandsReadAndAnswerTheSessionsFeedback(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+filepath.Join(root, "legato.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "task-1")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "Fix it", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveHandoffReview(ctx, store.HandoffReview{TaskID: "task-1", Phase: "waiting_for_author", Pass: 1, UpdatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertHandoffReviewItem(ctx, store.HandoffReviewItem{ID: "f-1", TaskID: "task-1", Kind: "blocking", Pass: 1, PR: 7, Version: 1, Title: "No bound", Body: "/clear", CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out := captureStdout(t, func() int { return runCLI([]string{"review-pass", "feedback", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID   string `json:"id"`
				Body string `json:"body"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Body != "/clear" {
		t.Fatalf("feedback = %s (%v)", out, err)
	}

	if code := runCLI([]string{"review-pass", "answer", "f-1", "--skipped"}); code == 0 {
		t.Fatal("a blocking finding was skipped")
	}
	if code := runCLI([]string{"review-pass", "answer", "f-1", "--fixed", "--note", "bounded"}); code != 0 {
		t.Fatalf("answer exit %d", code)
	}
	db, _ = openStore(cfg)
	defer db.Close()
	item, err := db.GetHandoffReviewItem(ctx, "task-1", "f-1")
	if err != nil || item.Answer != "fixed" || item.AnswerNote != "bounded" || item.Posted {
		t.Fatalf("item = %+v, %v", item, err)
	}
	if !strings.Contains(strings.Join(forwardedCommands["review-pass"], ","), "answer") {
		t.Fatal("review-pass answer is not forwarded from the sandbox")
	}
}
