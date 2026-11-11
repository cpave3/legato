package whisper

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDescribeSucceedsWhenTheServerAnswersWithItsInfo(t *testing.T) {
	srv := newWyomingTestServer(t, "")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := NewClient(srv.Addr()).Describe(ctx); err != nil {
		t.Fatalf("describe: %v", err)
	}
}

func TestDescribeFailsWhenTheServerDoesNotAnswerInTime(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		// Accepts, reads nothing back: a server that is stuck.
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(2 * time.Second)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := NewClient(ln.Addr().String()).Describe(ctx); err == nil {
		t.Fatal("describe of a silent server succeeded")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("describe waited %v past its context", waited)
	}
}

func TestDescribeFailsWhenNothingListens(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if err := NewClient(addr).Describe(context.Background()); err == nil {
		t.Fatal("describe with nothing listening succeeded")
	}
}
