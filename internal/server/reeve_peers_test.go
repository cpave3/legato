package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

type fakeReevePeers struct {
	approvals service.InstanceApprovals
	asked     int
}

func (f *fakeReevePeers) List(context.Context) ([]service.Peer, error) { return nil, nil }
func (f *fakeReevePeers) Pair(context.Context, string, string) (service.Peer, error) {
	return service.Peer{}, nil
}
func (f *fakeReevePeers) Remove(context.Context, string) error { return nil }
func (f *fakeReevePeers) Receive(context.Context, service.IntegrationIdentity, service.PeerMessage) (service.PeerAnswer, error) {
	return service.PeerAnswer{}, nil
}
func (f *fakeReevePeers) OwnApprovals(context.Context) ([]service.PeerApproval, error) {
	return nil, nil
}
func (f *fakeReevePeers) PeerApprovals(context.Context) (service.InstanceApprovals, error) {
	f.asked++
	return f.approvals, nil
}

// Paired instances' change sets are asked for in the background: a change
// tells web clients, and the tray reads what was last heard.
func TestPairedInstancesApprovalsAreKeptAndAChangeIsAnnounced(t *testing.T) {
	srv := New(&mockBoardService{}, nil, nil, ":0")
	peers := &fakeReevePeers{approvals: service.InstanceApprovals{Approvals: []service.InstanceApproval{{PeerApproval: service.PeerApproval{ID: "ap-1", Title: "Restart"}, Instance: "vm-1"}}}}
	srv.SetReevePeers(peers, nil)
	messages := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{ctx: context.Background(), sendFn: func(m WSMessage) { messages <- m }})

	srv.refreshPeerApprovals(context.Background())
	select {
	case m := <-messages:
		if m.Type != MsgApprovalsChanged {
			t.Fatalf("message %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no approvals_changed after a new change set")
	}
	// The same list again is not news.
	srv.refreshPeerApprovals(context.Background())
	select {
	case m := <-messages:
		t.Fatalf("announced an unchanged list: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
	if srv.peerApprovalCount() != 1 {
		t.Fatalf("count %d", srv.peerApprovalCount())
	}

	w := httptest.NewRecorder()
	srv.reevePeerApprovalsHandler()(w, httptest.NewRequest(http.MethodGet, "/api/reeve/peers/approvals", nil))
	var got service.InstanceApprovals
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Approvals) != 1 || got.Approvals[0].Instance != "vm-1" || peers.asked != 2 {
		t.Fatalf("served %+v after %d asks", got, peers.asked)
	}
}
