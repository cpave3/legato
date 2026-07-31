package overlay

// PrintableInputOwner marks an overlay mode whose focused control owns
// printable keys before application-global bindings.
type PrintableInputOwner interface {
	ConsumesPrintableInput() bool
}

// ConsumesPrintableInput reports whether the active overlay is in a text-entry
// mode. Input-oriented overlays consume printable keys by default.
func ConsumesPrintableInput(model any) bool {
	if owner, ok := model.(PrintableInputOwner); ok {
		return owner.ConsumesPrintableInput()
	}
	switch model.(type) {
	case SearchOverlay, CreateOverlay, ImportOverlay, TitleEditOverlay,
		LinkPROverlay, AgentSpawnOverlay, SwarmInitOverlay, GroupOverlay,
		WorkflowTriggerOverlay, PlanApprovalOverlay, AgentActionOverlay,
		WorktreeOverlay:
		return true
	default:
		return false
	}
}
