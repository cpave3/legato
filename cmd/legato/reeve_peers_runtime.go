package main

import (
	"context"
	"errors"
	"os"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/discovery"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// newReevePeers is Reeve to Reeve for this process: messages to the Reeves
// of paired instances, and theirs to this one's, given to reeve by the
// reeve.peer setting. reeve may be nil (the tool's side, which only sends).
// address is how others reach this server; "" works it out as approval
// links do.
func newReevePeers(cfg *config.Config, db *store.Store, reeve *service.ReeveService, address func() string, attention *service.AttentionService) *service.ReevePeerService {
	secrets := configuredSecretStore(cfg, db)
	opts := service.ReevePeerOptions{
		Store:   db,
		Tokens:  service.NewIntegrationTokenService(db),
		Secrets: secrets,
		Workers: service.NewWorkerService(db, service.WorkerServiceOptions{Secrets: secrets, MainName: pairingName(cfg)}),
		Self: func(ctx context.Context) service.ReeveSelf {
			self := service.ReeveSelf{Name: pairingName(cfg), Role: service.InstanceRole(ctx, db)}
			self.InstanceID, _ = service.InstanceID(ctx, db)
			if address != nil {
				self.Address = address()
			}
			if self.Address == "" {
				self.Address = approvalServerURL(cfg, "", discovery.ReachableIPv4)
			}
			_, _, caCert := resolveTLS(cfg)
			if caCert == "" && cfg.Web.TLS.Cert != "" {
				caCert = cfg.Web.TLS.Cert
			}
			if caCert != "" {
				if pem, err := os.ReadFile(caCert); err == nil {
					self.CACertificate = string(pem)
				}
			}
			return self
		},
	}
	if reeve != nil {
		opts.Deliver = func(ctx context.Context, from service.PeerInfo, text string) error {
			policy := service.PeerStart
			if effective, err := settingsFor(db).Effective(ctx); err == nil && effective.Reeve.Peer != "" {
				policy = effective.Reeve.Peer
			}
			err := reeve.PeerMessage(ctx, from, text, policy)
			if errors.Is(err, service.ErrPeerAsk) && attention != nil {
				_ = attention.Reconcile(ctx, peerAttentionProducer)
			}
			return err
		}
	}
	return service.NewReevePeerService(opts)
}

// wireReevePeers serves Reeve to Reeve on srv, and asks the person in
// attention when another Reeve wants to talk and reeve.peer says to ask.
func wireReevePeers(srv *server.Server, cfg *config.Config, db *store.Store, reeve *service.ReeveService, attention *service.AttentionService) {
	if attention != nil && reeve != nil && !attention.HasProducer(peerAttentionProducer) {
		adapter := service.NewReevePeerAttentionAdapter(db, reeve)
		mustRegister(attention.RegisterProducer(adapter))
		mustRegister(attention.RegisterHandler(adapter.Producer(), adapter))
	}
	srv.SetReevePeers(newReevePeers(cfg, db, reeve, approvalAddress(cfg), attention), service.NewIntegrationTokenService(db))
}

// peerAttentionProducer is the Attention producer of Reeves that want to
// talk.
const peerAttentionProducer = "reeve-peer"

// approvalAddress is this server's address for other instances.
func approvalAddress(cfg *config.Config) func() string {
	return func() string { return approvalServerURL(cfg, "", discovery.ReachableIPv4) }
}
