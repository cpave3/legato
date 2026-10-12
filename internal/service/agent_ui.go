package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Where an agent UI item is drawn.
const (
	// AgentUIPanel is an artifact in the side panel beside the agent.
	AgentUIPanel = "panel"
	// AgentUIInline is a card in the conversation, at the call that made it.
	AgentUIInline = "inline"
)

// What an agent UI item's content is.
const (
	AgentUIFormatHTML     = "html"
	AgentUIFormatMarkdown = "markdown"
)

// Bounds on what one task may keep and one item may hold.
const (
	// AgentUIMaxContentBytes bounds one item's content.
	AgentUIMaxContentBytes  = 1 << 20
	agentUIMaxTitleRunes    = 200
	agentUIMaxFallbackRunes = 4000
	// A task past agentUIMaxItems loses the items changed longest ago, so a
	// long session never reaches a point where nothing more can be shown.
	agentUIMaxItems     = 200
	agentUIMaxRevisions = 20
)

// ErrAgentUIInvalid marks a request to show something that was rejected
// before anything was stored.
var ErrAgentUIInvalid = errors.New("invalid agent UI item")

var agentUIKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// agentUIFormats lists what each placement can draw.
var agentUIFormats = map[string][]string{
	AgentUIPanel:  {AgentUIFormatHTML, AgentUIFormatMarkdown, AgentUIFormatComponents},
	AgentUIInline: {AgentUIFormatHTML, AgentUIFormatComponents},
}

// AgentUIItem is content an agent asked Legato to show to the person.
type AgentUIItem struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Placement string `json:"placement"`
	Format    string `json:"format"`
	Title     string `json:"title"`
	Key       string `json:"key,omitempty"`
	Revision  int    `json:"revision"`
	// OldestRevision is the earliest revision still stored.
	OldestRevision int    `json:"oldest_revision"`
	Content        string `json:"content,omitempty"`
	FallbackText   string `json:"fallback_text,omitempty"`
	// ResourceDomains are the hosts HTML content may load scripts, styles,
	// fonts and images from. Set only on an HTML item read with its content.
	ResourceDomains []string `json:"resource_domains,omitempty"`
	// Images are the image files this revision uses, by the name its content
	// refers to each by.
	Images    []AgentUIImage `json:"images,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

// AgentUIShowInput is one request from an agent to show something.
type AgentUIShowInput struct {
	TaskID      string
	ExecutionID string
	Placement   string
	Format      string
	Title       string
	Key         string
	// Content is the HTML or Markdown; the components format has Components instead.
	Content      string
	Components   []UIComponent
	FallbackText string
	// Images are image files the HTML or Markdown uses. The content refers to
	// one as legato-image:<name>.
	Images []AgentUIImageInput
}

// AgentUIOptions configures an AgentUIService.
type AgentUIOptions struct {
	// ResourceDomains are the hosts an agent's HTML may load scripts, styles,
	// fonts and images from. Empty means none: everything must be inline.
	ResourceDomains []string
	// ImageDir is where copies of the image files agents show are kept, a
	// folder for each task. Without it an agent cannot show image files.
	ImageDir string
}

// AgentUIService stores what agents ask Legato to show and tells running
// clients when it changes.
type AgentUIService struct {
	store    *store.Store
	bus      *events.Bus
	domains  []string
	imageDir string
}

// NewAgentUIService returns a service over s. bus may be nil in a process
// with nobody to tell, such as a CLI command.
func NewAgentUIService(s *store.Store, bus *events.Bus, opts AgentUIOptions) *AgentUIService {
	return &AgentUIService{store: s, bus: bus, domains: append([]string(nil), opts.ResourceDomains...), imageDir: opts.ImageDir}
}

// ResourceDomains returns the hosts an agent's HTML may load from.
func (s *AgentUIService) ResourceDomains() []string {
	return append([]string(nil), s.domains...)
}

// Show stores an item and announces it. Showing under a key the task already
// used in that placement replaces the item's content as a new revision.
func (s *AgentUIService) Show(ctx context.Context, in AgentUIShowInput) (*AgentUIItem, error) {
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.Title = strings.TrimSpace(in.Title)
	in.Key = strings.TrimSpace(in.Key)
	in.FallbackText = strings.TrimSpace(in.FallbackText)
	files := in.Images
	if in.Format == AgentUIFormatComponents {
		encoded, err := encodeUIComponents(in.Components)
		if err != nil {
			return nil, err
		}
		if len(in.Images) > 0 {
			return nil, fmt.Errorf("%w: components name their image files themselves, in image components; images is for HTML and Markdown", ErrAgentUIInvalid)
		}
		in.Content, in.Images = encoded, nil
		files = uiComponentImages(in.Components)
		if len(files) > agentUIMaxImages {
			return nil, fmt.Errorf("%w: at most %d images fit in one item", ErrAgentUIInvalid, agentUIMaxImages)
		}
	}
	if err := validateAgentUIShow(in); err != nil {
		return nil, err
	}
	// Last, so that a request turned down for another reason leaves no files.
	images, err := s.keepImages(in.TaskID, files)
	if err != nil {
		return nil, err
	}
	saved, err := s.store.SaveAgentUIItem(ctx, store.AgentUIItem{
		ID: generateAgentUIItemID(), TaskID: in.TaskID, ExecutionID: in.ExecutionID,
		Placement: in.Placement, Key: in.Key, Format: in.Format, Title: in.Title,
		Content: in.Content, FallbackText: in.FallbackText, Images: images,
	}, store.AgentUILimits{MaxItems: agentUIMaxItems, MaxRevisions: agentUIMaxRevisions})
	if err != nil {
		return nil, err
	}
	s.publish(saved.TaskID, saved.ID, saved.Placement, saved.Revision, "shown")
	return agentUIItemFromStore(saved), nil
}

// Get returns an item with the content of one revision; revision 0 means the
// latest.
func (s *AgentUIService) Get(ctx context.Context, id string, revision int) (*AgentUIItem, error) {
	stored, err := s.store.GetAgentUIItem(ctx, id, revision)
	if err != nil {
		return nil, err
	}
	item := agentUIItemFromStore(stored)
	item.Images = s.storedImages(stored)
	if item.Format == AgentUIFormatHTML {
		item.ResourceDomains = s.ResourceDomains()
	}
	return item, nil
}

// List returns a task's items at their latest revision, oldest first and
// without content.
func (s *AgentUIService) List(ctx context.Context, taskID string) ([]AgentUIItem, error) {
	stored, err := s.store.ListAgentUIItems(ctx, taskID)
	if err != nil {
		return nil, err
	}
	items := make([]AgentUIItem, 0, len(stored))
	for i := range stored {
		items = append(items, *agentUIItemFromStore(&stored[i]))
	}
	return items, nil
}

// AgentUIShown says how much one agent has shown and what it showed last,
// for a client that cannot draw it and links to it instead.
type AgentUIShown struct {
	Cards       int
	Artifacts   int
	LatestID    string
	LatestTitle string
}

// Shown summarises what a task's agent has shown.
func (s *AgentUIService) Shown(ctx context.Context, taskID string) (AgentUIShown, error) {
	summary, err := s.store.SummarizeAgentUIItems(ctx, taskID)
	if err != nil {
		return AgentUIShown{}, err
	}
	return AgentUIShown(summary), nil
}

// AgentUIWebLink returns the address in the web UI that opens an agent with
// one thing it showed in the side panel; with no item it opens the agent
// alone. baseURL is where the web UI is served ("http://localhost:3080");
// with none there is nothing to link to and the result is empty.
func AgentUIWebLink(baseURL, taskID, itemID string) string {
	if baseURL == "" {
		return ""
	}
	link := strings.TrimRight(baseURL, "/") + "/agents?task=" + url.QueryEscape(taskID)
	if itemID != "" {
		link += "&artifact=" + url.QueryEscape(itemID)
	}
	return link
}

// Delete removes an item and every revision of it.
func (s *AgentUIService) Delete(ctx context.Context, id string) error {
	item, err := s.store.GetAgentUIItem(ctx, id, 0)
	if err != nil {
		return err
	}
	if err := s.store.DeleteAgentUIItem(ctx, id); err != nil {
		return err
	}
	s.publish(item.TaskID, item.ID, item.Placement, item.Revision, "deleted")
	return nil
}

func validateAgentUIShow(in AgentUIShowInput) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentUIInvalid, fmt.Sprintf(format, args...))
	}
	if in.TaskID == "" {
		return invalid("no Legato session to show it in")
	}
	formats, ok := agentUIFormats[in.Placement]
	if !ok {
		return invalid("unknown placement %q", in.Placement)
	}
	supported := false
	for _, format := range formats {
		supported = supported || format == in.Format
	}
	if !supported {
		return invalid("format %q cannot be shown here; use one of: %s", in.Format, strings.Join(formats, ", "))
	}
	if strings.TrimSpace(in.Content) == "" {
		return invalid("content is empty")
	}
	if len(in.Content) > AgentUIMaxContentBytes {
		return invalid("content is %d bytes; the limit is %d", len(in.Content), AgentUIMaxContentBytes)
	}
	if !utf8.ValidString(in.Content) {
		return invalid("content is not valid UTF-8 text")
	}
	if utf8.RuneCountInString(in.Title) > agentUIMaxTitleRunes {
		return invalid("title is longer than %d characters", agentUIMaxTitleRunes)
	}
	if in.Placement == AgentUIPanel && in.Title == "" {
		return invalid("an artifact needs a title")
	}
	if in.Key != "" && !agentUIKeyPattern.MatchString(in.Key) {
		return invalid("key %q must be 1-64 letters, digits, dots, dashes or underscores", in.Key)
	}
	if in.Placement == AgentUIInline && in.FallbackText == "" {
		return invalid("a card needs fallback_text: the same information as plain text")
	}
	if utf8.RuneCountInString(in.FallbackText) > agentUIMaxFallbackRunes {
		return invalid("fallback_text is longer than %d characters", agentUIMaxFallbackRunes)
	}
	return validateAgentUIImages(in.Content, in.Images)
}

func (s *AgentUIService) publish(taskID, itemID, placement string, revision int, kind string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{Type: events.EventAgentUIChanged, At: time.Now(), Payload: events.AgentUIChangedPayload{
		TaskID: taskID, ItemID: itemID, Placement: placement, Revision: revision, Kind: kind,
	}})
}

func agentUIItemFromStore(item *store.AgentUIItem) *AgentUIItem {
	out := &AgentUIItem{
		ID: item.ID, TaskID: item.TaskID, Placement: item.Placement, Format: item.Format,
		Title: item.Title, Key: item.Key, Revision: item.Revision, OldestRevision: item.OldestRevision,
		Content: item.Content, FallbackText: item.FallbackText,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	out.Images = agentUIImagesFromStore(item.TaskID, item.Images)
	return out
}

func generateAgentUIItemID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "ui-" + hex.EncodeToString(b)
}
