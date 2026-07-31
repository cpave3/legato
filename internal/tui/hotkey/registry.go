// Package hotkey defines declarative TUI key bindings and derives dispatch and
// display lookups from the same validated metadata.
package hotkey

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type ActionID string
type Page string
type Overlay string
type Mode string
type Capability string
type Predicate string
type Target string
type Precedence string

type Surface uint8

const (
	PageBoard     Page = "board"
	PageDetail    Page = "detail"
	PageAgents    Page = "agents"
	PageReport    Page = "report"
	PageReview    Page = "review"
	PagePlans     Page = "plans"
	PageAttention Page = "attention"
)

const (
	CapabilityWorktrees     Capability = "worktrees"
	CapabilitySwarm         Capability = "swarm"
	CapabilityVoice         Capability = "voice"
	CapabilityNotifications Capability = "notifications"
	CapabilityReview        Capability = "review-service"
	CapabilityPlans         Capability = "plan-service"
	CapabilityAttention     Capability = "attention-service"
	CapabilitySync          Capability = "sync-service"
	CapabilityPR            Capability = "pr-service"
)

const (
	TargetRoot    Target = "root"
	TargetPage    Target = "page"
	TargetOverlay Target = "overlay"
)

const (
	YieldToSpecific Precedence = "yield-to-specific"
)

const (
	FullHelp Surface = 1 << iota
	Shortlist
	ShortcutBar
	InlineHint
)

// Key is a canonical Bubble Tea key plus its user-facing representation.
type Key struct {
	Type    tea.KeyType
	Runes   string
	Display string
}

func Rune(r rune) Key { return Key{Type: tea.KeyRunes, Runes: string(r), Display: string(r)} }

func Runes(runes string, display string) Key {
	return Key{Type: tea.KeyRunes, Runes: runes, Display: display}
}

func Special(keyType tea.KeyType, display string) Key {
	return Key{Type: keyType, Display: display}
}

func (k Key) Matches(msg tea.KeyMsg) bool {
	if msg.Type != k.Type {
		return false
	}
	if k.Type == tea.KeyRunes {
		return string(msg.Runes) == k.Runes
	}
	return true
}

func (k Key) identity() string { return fmt.Sprintf("%d:%s", k.Type, k.Runes) }

// Scope identifies where a binding participates in routing. Empty overlay and
// mode values represent their parent page scope.
type Scope struct {
	Global  bool
	Page    Page
	Overlay Overlay
	Mode    Mode
}

func GlobalScope() Scope                                { return Scope{Global: true} }
func PageScope(page Page) Scope                         { return Scope{Page: page} }
func PageModeScope(page Page, mode Mode) Scope          { return Scope{Page: page, Mode: mode} }
func OverlayScope(overlay Overlay) Scope                { return Scope{Overlay: overlay} }
func OverlayModeScope(overlay Overlay, mode Mode) Scope { return Scope{Overlay: overlay, Mode: mode} }

func (s Scope) String() string {
	parts := make([]string, 0, 2)
	if s.Global {
		return "global"
	}
	if s.Page != "" {
		parts = append(parts, string(s.Page))
	}
	if s.Overlay != "" {
		parts = append(parts, "overlay:"+string(s.Overlay))
	}
	if s.Mode != "" {
		parts = append(parts, "mode:"+string(s.Mode))
	}
	return strings.Join(parts, "/")
}

// Binding is the single source of truth for dispatch and key documentation.
type Binding struct {
	ID           ActionID
	Keys         []Key
	Label        string
	Help         string
	Scope        Scope
	Section      string
	SectionOrder int
	Order        int
	Surfaces     Surface
	Requires     []Capability
	When         []Predicate
	Target       Target
	Precedence   Precedence
}

type Capabilities map[Capability]bool

type Predicates map[Predicate]bool

func NewCapabilities(values ...Capability) Capabilities {
	result := make(Capabilities, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func NewPredicates(values ...Predicate) Predicates {
	result := make(Predicates, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

// Context is the effective routing/display state at a point in time.
type Context struct {
	Page         Page
	Overlay      Overlay
	Mode         Mode
	Capabilities Capabilities
	Predicates   Predicates
	InputFocused bool
}

type Registry struct {
	bindings []Binding
}

func NewRegistry(bindings []Binding) (*Registry, error) {
	copyOfBindings := append([]Binding(nil), bindings...)
	if err := validate(copyOfBindings); err != nil {
		return nil, err
	}
	return &Registry{bindings: copyOfBindings}, nil
}

func MustRegistry(bindings []Binding) *Registry {
	registry, err := NewRegistry(bindings)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *Registry) Resolve(msg tea.KeyMsg, ctx Context) (Binding, bool) {
	if ctx.InputFocused && isPrintable(msg) {
		return Binding{}, false
	}
	var candidates []Binding
	for _, binding := range r.bindings {
		if !binding.active(ctx) || !scopeApplies(binding.Scope, ctx) {
			continue
		}
		for _, key := range binding.Keys {
			if key.Matches(msg) {
				candidates = append(candidates, binding)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return Binding{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return scopeSpecificity(candidates[i].Scope) > scopeSpecificity(candidates[j].Scope)
	})
	return candidates[0], true
}

func (r *Registry) Bindings(ctx Context, surface Surface) []Binding {
	result := make([]Binding, 0)
	for _, binding := range r.bindings {
		if binding.Surfaces&surface != 0 && binding.active(ctx) && displayScopeApplies(binding.Scope, ctx) {
			result = append(result, binding)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SectionOrder != result[j].SectionOrder {
			return result[i].SectionOrder < result[j].SectionOrder
		}
		if result[i].Section != result[j].Section {
			return result[i].Section < result[j].Section
		}
		if result[i].Order != result[j].Order {
			return result[i].Order < result[j].Order
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// ReservedKeys returns fixed keys active in a scope for collision checking by
// dynamic content-specific shortcut allocators.
func (r *Registry) ReservedKeys(ctx Context) map[string]ActionID {
	result := make(map[string]ActionID)
	for _, binding := range r.bindings {
		if !binding.active(ctx) || !scopeApplies(binding.Scope, ctx) {
			continue
		}
		for _, key := range binding.Keys {
			result[key.identity()] = binding.ID
		}
	}
	return result
}

func (b Binding) active(ctx Context) bool {
	for _, capability := range b.Requires {
		if !ctx.Capabilities[capability] {
			return false
		}
	}
	for _, predicate := range b.When {
		if !ctx.Predicates[predicate] {
			return false
		}
	}
	return true
}

func scopeApplies(scope Scope, ctx Context) bool {
	if scope.Global {
		return true
	}
	if scope.Overlay != "" {
		if scope.Overlay != ctx.Overlay {
			return false
		}
	} else if ctx.Overlay != "" {
		return false
	}
	if scope.Page != "" && scope.Page != ctx.Page {
		return false
	}
	return scope.Mode == "" || scope.Mode == ctx.Mode
}

func displayScopeApplies(scope Scope, ctx Context) bool {
	if scope.Global {
		return true
	}
	if scope.Overlay != "" {
		return scope.Overlay == ctx.Overlay && (scope.Mode == "" || scope.Mode == ctx.Mode)
	}
	if ctx.Overlay != "" || scope.Page != ctx.Page {
		return false
	}
	return scope.Mode == "" || scope.Mode == ctx.Mode
}

func scopeSpecificity(scope Scope) int {
	if scope.Global {
		return 0
	}
	specificity := 1
	if scope.Overlay != "" {
		specificity += 2
	}
	if scope.Mode != "" {
		specificity++
	}
	return specificity
}

func isPrintable(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

func validate(bindings []Binding) error {
	validPages := map[Page]bool{
		PageBoard: true, PageDetail: true, PageAgents: true, PageReport: true,
		PageReview: true, PagePlans: true, PageAttention: true,
	}
	for i, binding := range bindings {
		if binding.ID == "" {
			return fmt.Errorf("hotkey binding %d has empty action ID", i)
		}
		if len(binding.Keys) == 0 {
			return fmt.Errorf("hotkey action %q has no keys", binding.ID)
		}
		if binding.Scope.Global == (binding.Scope.Page != "" || binding.Scope.Overlay != "") {
			return fmt.Errorf("hotkey action %q has invalid scope %q", binding.ID, binding.Scope.String())
		}
		if binding.Scope.Page != "" && !validPages[binding.Scope.Page] {
			return fmt.Errorf("hotkey action %q has invalid page scope %q", binding.ID, binding.Scope.Page)
		}
		if binding.Scope.Mode != "" && binding.Scope.Page == "" && binding.Scope.Overlay == "" {
			return fmt.Errorf("hotkey action %q has mode without page or overlay scope", binding.ID)
		}
		if strings.TrimSpace(binding.Label) == "" {
			return fmt.Errorf("hotkey action %q has empty label", binding.ID)
		}
		if strings.TrimSpace(binding.Help) == "" {
			return fmt.Errorf("hotkey action %q has empty help text", binding.ID)
		}
		if binding.Surfaces != 0 && strings.TrimSpace(binding.Section) == "" {
			return fmt.Errorf("displayed hotkey action %q has empty section", binding.ID)
		}
		if binding.Target != TargetRoot && binding.Target != TargetPage && binding.Target != TargetOverlay {
			return fmt.Errorf("hotkey action %q has invalid dispatch target %q", binding.ID, binding.Target)
		}
		for _, key := range binding.Keys {
			if key.Display == "" {
				return fmt.Errorf("hotkey action %q has key with empty display label", binding.ID)
			}
			if key.Type == tea.KeyRunes && key.Runes == "" {
				return fmt.Errorf("hotkey action %q has empty rune key", binding.ID)
			}
		}
	}

	for i := range bindings {
		for j := i + 1; j < len(bindings); j++ {
			if !scopesOverlap(bindings[i].Scope, bindings[j].Scope) {
				continue
			}
			for _, left := range bindings[i].Keys {
				for _, right := range bindings[j].Keys {
					if left.identity() != right.identity() {
						continue
					}
					leftSpecificity := scopeSpecificity(bindings[i].Scope)
					rightSpecificity := scopeSpecificity(bindings[j].Scope)
					if leftSpecificity == rightSpecificity {
						return fmt.Errorf("duplicate hotkey %q for actions %q and %q in effective scope %s", left.Display, bindings[i].ID, bindings[j].ID, overlapName(bindings[i].Scope, bindings[j].Scope))
					}
					parent, child := bindings[i], bindings[j]
					if leftSpecificity > rightSpecificity {
						parent, child = child, parent
					}
					if parent.Precedence != YieldToSpecific {
						return fmt.Errorf("hotkey %q action %q in %s would shadow child action %q in %s; declare yield-to-specific", left.Display, parent.ID, parent.Scope.String(), child.ID, child.Scope.String())
					}
				}
			}
		}
	}
	return nil
}

func scopesOverlap(left, right Scope) bool {
	if left.Global || right.Global {
		return true
	}
	if left.Overlay != "" || right.Overlay != "" {
		if left.Overlay == "" || right.Overlay == "" || left.Overlay != right.Overlay {
			return false
		}
	} else if left.Page != right.Page {
		return false
	}
	return left.Mode == "" || right.Mode == "" || left.Mode == right.Mode
}

func overlapName(left, right Scope) string {
	if scopeSpecificity(left) >= scopeSpecificity(right) {
		return left.String()
	}
	return right.String()
}
