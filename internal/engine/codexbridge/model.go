package codexbridge

// modelProvider names Codex's models to the panel, which shows a model as
// provider/model.
const modelProvider = "openai"

// effortSetting is the name the native panel's model settings use for effort.
const effortSetting = "reasoningEffort"

// protocolVersion names the bridge to Legato in GET /v1/instance.
const protocolVersion = "legato-codex-bridge/1"

// modelInfo is one model the app server offers.
type modelInfo struct {
	ID        string
	IsDefault bool
	Efforts   []string
}

// modelSettingsState is the model settings contract the native panel reads
// and writes: what is chosen, what the thread would use on its own, the
// result of the two, and which settings can be adjusted at all.
type modelSettingsState struct {
	Model        string            `json:"model"`
	Overrides    map[string]any    `json:"overrides"`
	Defaults     map[string]any    `json:"defaults"`
	Effective    map[string]any    `json:"effective"`
	Capabilities modelCapabilities `json:"capabilities"`
}

type modelCapabilities struct {
	SchemaVersion int                          `json:"schemaVersion"`
	ModelID       string                       `json:"modelId"`
	Profile       string                       `json:"profile"`
	Source        string                       `json:"source"`
	Fingerprint   string                       `json:"fingerprint"`
	Settings      map[string]settingCapability `json:"settings"`
}

type settingCapability struct {
	Status  string   `json:"status"`
	Type    string   `json:"type"`
	Values  []string `json:"values,omitempty"`
	Default any      `json:"default,omitempty"`
}

// effectiveModel is the model the next turn Legato starts will use.
func (s *session) effectiveModel() string {
	if s.modelOverride != "" {
		return s.modelOverride
	}
	return s.model
}

func (s *session) effectiveEffort() string {
	if s.effortOverride != "" {
		return s.effortOverride
	}
	return s.effort
}

func (s *session) modelRef() string {
	return modelProvider + "/" + s.effectiveModel()
}

// settingsState describes the one setting a Codex thread exposes: reasoning
// effort, with the levels the model in use takes when the server listed them.
func (s *session) settingsState(models []modelInfo) modelSettingsState {
	state := modelSettingsState{
		Model:     s.modelRef(),
		Overrides: map[string]any{},
		Defaults:  map[string]any{},
		Effective: map[string]any{},
		Capabilities: modelCapabilities{
			SchemaVersion: 1,
			ModelID:       s.effectiveModel(),
			Profile:       "codex",
			Source:        protocolVersion,
			Fingerprint:   s.effectiveModel(),
			Settings:      map[string]settingCapability{},
		},
	}
	capability := settingCapability{Status: "supported", Type: "enum"}
	for _, model := range models {
		if model.ID == s.effectiveModel() {
			capability.Values = model.Efforts
		}
	}
	if s.effort != "" {
		state.Defaults[effortSetting] = s.effort
		capability.Default = s.effort
	}
	if len(capability.Values) > 0 {
		state.Capabilities.Settings[effortSetting] = capability
	}
	if s.effortOverride != "" {
		state.Overrides[effortSetting] = s.effortOverride
	}
	if effort := s.effectiveEffort(); effort != "" {
		state.Effective[effortSetting] = effort
	}
	return state
}
