package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// newApprovalService is the approval service of any Legato process: changes
// reach other processes over IPC, and the process that creates an approval
// sends its phone notification. serverURL gives the address for the link.
func newApprovalService(cfg *config.Config, db *store.Store, bus *events.Bus, serverURL func() string) *service.ApprovalService {
	opts := service.ApprovalServiceOptions{
		Bus: bus,
		Broadcast: func(approvalID, taskID, kind string) {
			ipc.Broadcast(ipc.Message{Type: "approval_changed", TaskID: taskID, ItemID: approvalID, Kind: kind})
		},
	}
	if cfg.Notifications.ApprovalsEnabled() && cfg.Notifications.Ntfy.Topic != "" {
		notifier := service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
		opts.Created = func(approval service.Approval) {
			url := cfg.Web.PublicURL
			if serverURL != nil {
				url = serverURL()
			}
			go service.ApprovalNotifications(notifier, url)(approval)
		}
	}
	return service.NewApprovalService(db, opts)
}

// configuredSecretStore is Legato's own secret store, legato://name.
func configuredSecretStore(cfg *config.Config, db *store.Store) *service.SecretStoreService {
	return service.NewSecretStoreService(db, credential.MasterKeyOptions{
		DataDir:    config.ResolveDataDir(cfg),
		Source:     cfg.Secrets.MasterKey,
		Passphrase: os.Getenv("LEGATO_SECRETS_PASSPHRASE"),
	})
}

// credentialProfileDirs are the folders whose profiles add to or replace the
// shipped ones: <config dir>/profiles.
func credentialProfileDirs() []string {
	return []string{filepath.Join(filepath.Dir(config.ResolveConfigPath()), "profiles")}
}

// configuredCredentialBroker builds the broker over the configured
// credentials. messenger is set only in a host process.
func configuredCredentialBroker(cfg *config.Config, db *store.Store, approvals *service.ApprovalService, messenger service.AgentMessenger) (*service.CredentialBroker, error) {
	return service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{
		Credentials:      cfg.Credentials,
		ProfileDirs:      credentialProfileDirs(),
		Stores:           map[string]credential.Store{"legato": configuredSecretStore(cfg, db)},
		OnePasswordToken: cfg.Secrets.OnePassword.Token,
		Messenger:        messenger,
	})
}

// startCredentials gives a host process (TUI, legato serve) the broker that
// applies decisions on credential approvals and ends the grants of agents
// that end. nil, with the reason logged, when the credentials are wrong.
func startCredentials(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus, approvals *service.ApprovalService, agents service.AgentService) (*service.CredentialBroker, func()) {
	var messenger service.AgentMessenger
	if m, ok := agents.(service.AgentMessenger); ok {
		messenger = m
	}
	broker, err := configuredCredentialBroker(cfg, db, approvals, messenger)
	if err != nil {
		log.Printf("credentials: %v (credential requests are refused until this is fixed)", err)
		return nil, func() {}
	}
	broker.RegisterResolver()
	return broker, broker.StartEventLoop(ctx, bus)
}
