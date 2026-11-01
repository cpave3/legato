package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

// ArgusSettings is the integrations.argus block of the configuration.
type ArgusSettings struct {
	URL, Delivery, Agent string
}

// IntegrationsOverview is what the web UI's settings show of integrations:
// the scoped tokens (never a token itself) and Argus.
type IntegrationsOverview struct {
	Tokens []store.IntegrationToken `json:"tokens"`
	Argus  ArgusOverview            `json:"argus"`
}

// ArgusOverview is Argus's configuration, Legato's connection to it, the
// replies waiting for it and the cases it shows in Attention.
type ArgusOverview struct {
	Configured bool              `json:"configured"`
	URL        string            `json:"url,omitempty"`
	Delivery   string            `json:"delivery,omitempty"`
	Agent      string            `json:"agent,omitempty"`
	Connection IntegrationStatus `json:"connection"`
	OpenCases  int               `json:"open_cases"`
}

// IntegrationOverview reads the overview from the database.
type IntegrationOverview struct {
	db    *store.Store
	argus *ArgusSettings
}

// NewIntegrationOverview makes the overview; argus is nil when
// integrations.argus is not configured.
func NewIntegrationOverview(db *store.Store, argus *ArgusSettings) *IntegrationOverview {
	return &IntegrationOverview{db: db, argus: argus}
}

func (o *IntegrationOverview) Overview(ctx context.Context) (IntegrationsOverview, error) {
	tokens, err := o.db.ListIntegrationTokens(ctx)
	if err != nil {
		return IntegrationsOverview{}, err
	}
	if tokens == nil {
		tokens = []store.IntegrationToken{}
	}
	connection, err := NewIntegrationConnector(o.db, nil, nil).Status(ctx, caseAttentionProducer)
	if err != nil {
		return IntegrationsOverview{}, err
	}
	cases, err := o.db.ListOpenIntegrationAttention(ctx, caseAttentionProducer)
	if err != nil {
		return IntegrationsOverview{}, err
	}
	argus := ArgusOverview{Connection: connection, OpenCases: len(cases)}
	if o.argus != nil {
		argus.Configured, argus.URL, argus.Delivery, argus.Agent = true, o.argus.URL, o.argus.Delivery, o.argus.Agent
	}
	return IntegrationsOverview{Tokens: tokens, Argus: argus}, nil
}
