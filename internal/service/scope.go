package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Scope's limits on what one press of Send may hold.
const (
	ScopeMaxPinsPerBatch    = 20
	ScopeMaxScreenshotBytes = 500 * 1024
	scopeMaxCommentRunes    = 4000
	scopeMaxNoteRunes       = 500
	scopeLineCommentRunes   = 200
)

var (
	// ErrScopeInvalidOrigin: not an http(s) origin (scheme, host, port).
	ErrScopeInvalidOrigin = errors.New("not a page origin")
	// ErrScopeInvalid: a batch, pin or outcome that cannot be kept.
	ErrScopeInvalid = errors.New("invalid pin")
	// ErrScopeTooLarge: past the per-batch or per-image limit.
	ErrScopeTooLarge = errors.New("over Scope's limits")
	// ErrScopePinState: the pin's state does not allow the change.
	ErrScopePinState = errors.New("the pin's state does not allow this")
)

// ScopeMessenger queues a message for a task slot's agent (the slot comes
// from WithAgentSlot). AgentService implements it.
type ScopeMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// ScopeSize is a width and height in CSS pixels.
type ScopeSize struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ScopePoint is a position in CSS pixels.
type ScopePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// ScopeBox is an element's bounding box in page coordinates.
type ScopeBox struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ScopePage is the page a pin was placed on.
type ScopePage struct {
	URL      string     `json:"url"`
	Title    string     `json:"title,omitempty"`
	Viewport ScopeSize  `json:"viewport"`
	Scroll   ScopePoint `json:"scroll"`
	DPR      float64    `json:"dpr,omitempty"`
}

// ScopeTarget is the element a pin is on.
type ScopeTarget struct {
	// Selectors are candidates, the most stable first.
	Selectors []string `json:"selectors"`
	Text      string   `json:"text,omitempty"`
	Box       ScopeBox `json:"box"`
}

// ScopeStyles are the element's styles that differ from the browser's
// defaults, its classes, inline style and its parent's layout.
type ScopeStyles struct {
	Computed map[string]string `json:"computed,omitempty"`
	Classes  []string          `json:"classes,omitempty"`
	Inline   string            `json:"inline,omitempty"`
	Parent   map[string]string `json:"parent,omitempty"`
}

// ScopeSource is the component that rendered the element, when the
// extension could tell. Confidence is exact, file-only, name-only or none.
type ScopeSource struct {
	Name       string   `json:"name,omitempty"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	Column     int      `json:"column,omitempty"`
	Owners     []string `json:"owners,omitempty"`
	Confidence string   `json:"confidence"`
}

// Location is file:line (or the file, or the name) when known.
func (s *ScopeSource) Location() string {
	if s == nil {
		return ""
	}
	switch {
	case s.File != "" && s.Line > 0:
		return fmt.Sprintf("%s:%d", s.File, s.Line)
	case s.File != "":
		return s.File
	case s.Name != "":
		return "<" + s.Name + ">"
	}
	return ""
}

// ScopePinInput is one pin of a batch the operator sent.
type ScopePinInput struct {
	Comment    string       `json:"comment"`
	Page       ScopePage    `json:"page"`
	Target     ScopeTarget  `json:"target"`
	Styles     ScopeStyles  `json:"styles"`
	Source     *ScopeSource `json:"source,omitempty"`
	Screenshot []byte       `json:"-"`
}

// scopeContext is what context_json holds.
type scopeContext struct {
	Page   ScopePage   `json:"page"`
	Target ScopeTarget `json:"target"`
	Styles ScopeStyles `json:"styles"`
}

// ScopePin is a pin as the extension and agents list it.
type ScopePin struct {
	ID            string    `json:"id"`
	BatchID       string    `json:"batch_id"`
	Origin        string    `json:"origin"`
	URL           string    `json:"url"`
	Path          string    `json:"path"`
	Comment       string    `json:"comment"`
	State         string    `json:"state"`
	Note          string    `json:"note,omitempty"`
	Selector      string    `json:"selector,omitempty"`
	Selectors     []string  `json:"selectors"`
	Text          string    `json:"text,omitempty"`
	Box           ScopeBox  `json:"box"`
	HasScreenshot bool      `json:"has_screenshot"`
	Location      string    `json:"location,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ScopePinDetail is a pin with all it captured.
type ScopePinDetail struct {
	ScopePin
	Page   ScopePage    `json:"page"`
	Target ScopeTarget  `json:"target"`
	Styles ScopeStyles  `json:"styles"`
	Source *ScopeSource `json:"source,omitempty"`
}

// ScopeBatchResult is what became of a batch.
type ScopeBatchResult struct {
	BatchID string   `json:"batch_id"`
	PinIDs  []string `json:"pin_ids"`
	// Subscribed: a slot subscribes to the origin. Delivered: its agent
	// was told of the batch.
	Subscribed bool `json:"subscribed"`
	Delivered  bool `json:"delivered"`
}

// ScopeSubscribeResult is what a subscribe did.
type ScopeSubscribeResult struct {
	Origin string
	// TookOverFrom is the agent ID that had the origin, if another did.
	TookOverFrom string
	// Waiting are the pins that were waiting in the pool, now delivered.
	Waiting []ScopePin
}

// Report is the subscribe's answer to the agent.
func (r ScopeSubscribeResult) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subscribed to %s: batches the operator sends from it come to you.", r.Origin)
	if r.TookOverFrom != "" {
		fmt.Fprintf(&b, " The agent of %s had it and was told.", r.TookOverFrom)
	}
	if len(r.Waiting) == 0 {
		b.WriteString(" No pins are waiting.")
		return b.String()
	}
	fmt.Fprintf(&b, "\n\n%s", scopePinLines(r.Waiting))
	b.WriteString("\n" + scopeInstructions)
	return b.String()
}

// ScopeServiceOptions configures NewScopeService.
type ScopeServiceOptions struct {
	Store *store.Store
	// Dir holds the screenshots: <data_dir>/scope.
	Dir       string
	Messenger ScopeMessenger
	Bus       *events.Bus
	Now       func() time.Time
}

// ScopeService keeps Legato Scope's pool: the pins the operator sent from
// live pages, by origin, and the task slots that subscribe to origins. See
// docs/ai/scope.md.
type ScopeService struct {
	store     *store.Store
	dir       string
	messenger ScopeMessenger
	bus       *events.Bus
	now       func() time.Time
	// mu keeps subscribing, pushing, listing for a slot and withdrawing one
	// at a time, so a pin is pushed once and never withdrawn while an agent
	// is being told of it.
	mu sync.Mutex
}

// NewScopeService returns a ScopeService.
func NewScopeService(opts ScopeServiceOptions) *ScopeService {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &ScopeService{store: opts.Store, dir: opts.Dir, messenger: opts.Messenger, bus: opts.Bus, now: now}
}

// SetMessenger gives the service the agent service once it exists.
func (s *ScopeService) SetMessenger(m ScopeMessenger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messenger = m
}

// NormalizeScopeOrigin returns origin as scheme://host[:port], lower case,
// or ErrScopeInvalidOrigin.
func NormalizeScopeOrigin(origin string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("%w: %q (want scheme://host[:port], such as http://localhost:5173)", ErrScopeInvalidOrigin, origin)
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

func scopeOriginOf(rawURL string) (string, *url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", nil, fmt.Errorf("%w: page address %q", ErrScopeInvalid, rawURL)
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), u, nil
}

func scopeID(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *ScopeService) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

func (s *ScopeService) changed(origins ...string) {
	if s.bus == nil {
		return
	}
	seen := map[string]bool{}
	for _, origin := range origins {
		if origin == "" || seen[origin] {
			continue
		}
		seen[origin] = true
		s.bus.Publish(events.Event{Type: events.EventScopeChanged, At: s.now(), Payload: events.ScopeChangedPayload{Origin: origin}})
	}
}

var scopeImageTypes = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

// SendBatch puts a batch into the pool and, when a running agent
// subscribes to the origin, pushes it to that agent as one message.
func (s *ScopeService) SendBatch(ctx context.Context, origin string, inputs []ScopePinInput) (ScopeBatchResult, error) {
	origin, err := NormalizeScopeOrigin(origin)
	if err != nil {
		return ScopeBatchResult{}, err
	}
	if len(inputs) == 0 {
		return ScopeBatchResult{}, fmt.Errorf("%w: a batch has no pins", ErrScopeInvalid)
	}
	if len(inputs) > ScopeMaxPinsPerBatch {
		return ScopeBatchResult{}, fmt.Errorf("%w: %d pins in one batch, at most %d", ErrScopeTooLarge, len(inputs), ScopeMaxPinsPerBatch)
	}
	now := s.stamp()
	batch := store.ScopeBatch{ID: scopeID("sb-"), Origin: origin, CreatedAt: now}
	pins := make([]store.ScopePin, 0, len(inputs))
	shots := map[string][]byte{}
	for i, in := range inputs {
		pin, shot, err := s.pinRecord(batch, i, in, now)
		if err != nil {
			return ScopeBatchResult{}, err
		}
		pins = append(pins, pin)
		if shot != nil {
			shots[pin.Screenshot] = shot
		}
	}
	if len(shots) > 0 {
		if err := os.MkdirAll(s.dir, 0o700); err != nil {
			return ScopeBatchResult{}, err
		}
	}
	written := []string{}
	removeWritten := func() {
		for _, name := range written {
			_ = os.Remove(filepath.Join(s.dir, name))
		}
	}
	for name, data := range shots {
		if err := os.WriteFile(filepath.Join(s.dir, name), data, 0o600); err != nil {
			removeWritten()
			return ScopeBatchResult{}, err
		}
		written = append(written, name)
	}
	if err := s.store.InsertScopeBatch(ctx, batch, pins); err != nil {
		removeWritten()
		return ScopeBatchResult{}, err
	}
	result := ScopeBatchResult{BatchID: batch.ID}
	for _, pin := range pins {
		result.PinIDs = append(result.PinIDs, pin.ID)
	}
	result.Subscribed, result.Delivered = s.pushOrigin(ctx, origin, batch.ID)
	s.changed(origin)
	return result, nil
}

func (s *ScopeService) pinRecord(batch store.ScopeBatch, i int, in ScopePinInput, now string) (store.ScopePin, []byte, error) {
	comment := strings.TrimSpace(in.Comment)
	if comment == "" {
		return store.ScopePin{}, nil, fmt.Errorf("%w: pin %d has no comment", ErrScopeInvalid, i+1)
	}
	if len([]rune(comment)) > scopeMaxCommentRunes {
		return store.ScopePin{}, nil, fmt.Errorf("%w: pin %d's comment is longer than %d characters", ErrScopeTooLarge, i+1, scopeMaxCommentRunes)
	}
	pageOrigin, u, err := scopeOriginOf(in.Page.URL)
	if err != nil {
		return store.ScopePin{}, nil, err
	}
	if pageOrigin != batch.Origin {
		return store.ScopePin{}, nil, fmt.Errorf("%w: pin %d is on %s, not %s", ErrScopeInvalid, i+1, pageOrigin, batch.Origin)
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	contextJSON, err := json.Marshal(scopeContext{Page: in.Page, Target: in.Target, Styles: in.Styles})
	if err != nil {
		return store.ScopePin{}, nil, err
	}
	pin := store.ScopePin{
		ID: scopeID("sp-"), BatchID: batch.ID, Origin: batch.Origin, Seq: i,
		URL: in.Page.URL, Path: path, Comment: comment, State: store.ScopeSent,
		ContextJSON: string(contextJSON), CreatedAt: now, UpdatedAt: now,
	}
	if len(in.Target.Selectors) > 0 {
		pin.Selector = in.Target.Selectors[0]
	}
	if in.Source != nil {
		source, err := json.Marshal(in.Source)
		if err != nil {
			return store.ScopePin{}, nil, err
		}
		text := string(source)
		pin.SourceJSON = &text
	}
	if len(in.Screenshot) == 0 {
		return pin, nil, nil
	}
	if len(in.Screenshot) > ScopeMaxScreenshotBytes {
		return store.ScopePin{}, nil, fmt.Errorf("%w: pin %d's screenshot is %d KB, at most %d KB", ErrScopeTooLarge, i+1, len(in.Screenshot)/1024, ScopeMaxScreenshotBytes/1024)
	}
	ext, ok := scopeImageTypes[http.DetectContentType(in.Screenshot)]
	if !ok {
		return store.ScopePin{}, nil, fmt.Errorf("%w: pin %d's screenshot is not a PNG, JPEG or WebP image", ErrScopeInvalid, i+1)
	}
	pin.Screenshot = pin.ID + ext
	return pin, in.Screenshot, nil
}

// pushOrigin pushes the origin's waiting pins to its subscriber's running
// agent. It says whether the origin has a subscriber and whether the
// batch (any waiting pin when batchID is empty) was delivered.
func (s *ScopeService) pushOrigin(ctx context.Context, origin, batchID string) (subscribed, delivered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, err := s.store.GetScopeSubscription(ctx, origin)
	if err != nil {
		return false, false
	}
	pushed := s.pushLocked(ctx, sub.TaskID, sub.Slot, []string{origin})
	for _, pin := range pushed {
		if batchID == "" || pin.BatchID == batchID {
			return true, true
		}
	}
	return true, false
}

// pushLocked pushes the waiting (sent) pins on a slot's origins to its
// running agent as one message, and returns the pins delivered. A failed
// push leaves their batches undelivered.
func (s *ScopeService) pushLocked(ctx context.Context, taskID, slot string, origins []string) []ScopePin {
	if s.messenger == nil || len(origins) == 0 {
		return nil
	}
	if status, err := s.store.ScopeSlotStatus(ctx, taskID, slot); err != nil || status != "running" {
		return nil
	}
	records, err := s.store.ListScopePins(ctx, store.ScopePinFilter{Origins: origins, States: []string{store.ScopeSent}})
	if err != nil || len(records) == 0 {
		return nil
	}
	pins := scopePins(records)
	message := "[legato scope] The operator pinned comments on a page you subscribe to.\n\n" + scopePinLines(pins) + "\n" + scopeInstructions
	if err := s.messenger.QueueMessage(WithAgentSlot(ctx, slot), taskID, message); err != nil {
		log.Printf("scope: pushing %d pin(s) to %s: %v", len(pins), AgentSlotKey(taskID, slot), err)
		_ = s.store.SetScopeBatchesUndelivered(ctx, batchIDsOf(pins), err.Error())
		return nil
	}
	if err := s.store.MarkScopePinsDelivered(ctx, pinIDsOf(pins), s.stamp()); err != nil {
		log.Printf("scope: marking pins delivered: %v", err)
	}
	s.changed(origins...)
	return pins
}

const scopeInstructions = "Read a pin's full context and screenshot with page_pin_get. When you have handled a pin, mark it with page_pin_resolve (fixed or wont_fix, with a one-line note); the operator sees it on the page."

// scopePinLines is one compact line per pin, grouped by origin.
func scopePinLines(pins []ScopePin) string {
	var b strings.Builder
	origin := ""
	for _, pin := range pins {
		if pin.Origin != origin {
			origin = pin.Origin
			fmt.Fprintf(&b, "%s:\n", origin)
		}
		fmt.Fprintf(&b, "- %s %s %q", pin.ID, pin.Path, cutRunes(oneLine(pin.Comment), scopeLineCommentRunes))
		if pin.Selector != "" {
			fmt.Fprintf(&b, " at %s", pin.Selector)
		}
		if pin.Location != "" {
			fmt.Fprintf(&b, " (%s)", pin.Location)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func pinIDsOf(pins []ScopePin) []string {
	ids := make([]string, len(pins))
	for i, pin := range pins {
		ids[i] = pin.ID
	}
	return ids
}

func batchIDsOf(pins []ScopePin) []string {
	seen := map[string]bool{}
	var ids []string
	for _, pin := range pins {
		if !seen[pin.BatchID] {
			seen[pin.BatchID] = true
			ids = append(ids, pin.BatchID)
		}
	}
	return ids
}

func parseScopeTime(text string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, text)
	return t
}

func scopeDetail(record store.ScopePin) ScopePinDetail {
	var ctx scopeContext
	_ = json.Unmarshal([]byte(record.ContextJSON), &ctx)
	var source *ScopeSource
	if record.SourceJSON != nil {
		source = &ScopeSource{}
		if json.Unmarshal([]byte(*record.SourceJSON), source) != nil {
			source = nil
		}
	}
	selectors := ctx.Target.Selectors
	if selectors == nil {
		selectors = []string{}
	}
	return ScopePinDetail{
		ScopePin: ScopePin{
			ID: record.ID, BatchID: record.BatchID, Origin: record.Origin, URL: record.URL, Path: record.Path,
			Comment: record.Comment, State: record.State, Note: record.Note, Selector: record.Selector,
			Selectors: selectors, Text: ctx.Target.Text, Box: ctx.Target.Box, HasScreenshot: record.Screenshot != "",
			Location: source.Location(), CreatedAt: parseScopeTime(record.CreatedAt), UpdatedAt: parseScopeTime(record.UpdatedAt),
		},
		Page: ctx.Page, Target: ctx.Target, Styles: ctx.Styles, Source: source,
	}
}

func scopePins(records []store.ScopePin) []ScopePin {
	pins := make([]ScopePin, len(records))
	for i, record := range records {
		pins[i] = scopeDetail(record).ScopePin
	}
	return pins
}

// Text is the pin's full context, for an agent.
func (d ScopePinDetail) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pin %s (%s) on %s\n", d.ID, strings.ReplaceAll(d.State, "_", " "), d.URL)
	fmt.Fprintf(&b, "Comment: %s\n", d.Comment)
	if d.Note != "" {
		fmt.Fprintf(&b, "Note: %s\n", d.Note)
	}
	b.WriteString("\nPage:\n")
	if d.Page.Title != "" {
		fmt.Fprintf(&b, "- title: %s\n", d.Page.Title)
	}
	fmt.Fprintf(&b, "- viewport: %g×%g, scrolled to %g,%g", d.Page.Viewport.Width, d.Page.Viewport.Height, d.Page.Scroll.X, d.Page.Scroll.Y)
	if d.Page.DPR > 0 {
		fmt.Fprintf(&b, ", device pixel ratio %g", d.Page.DPR)
	}
	b.WriteString("\n\nTarget:\n")
	for i, selector := range d.Target.Selectors {
		label := "selector"
		if i == 0 {
			label = "best selector"
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, selector)
	}
	if d.Target.Text != "" {
		fmt.Fprintf(&b, "- text: %q\n", d.Target.Text)
	}
	box := d.Target.Box
	fmt.Fprintf(&b, "- box (page coordinates): x %g, y %g, %g×%g\n", box.X, box.Y, box.Width, box.Height)
	if d.Source != nil {
		b.WriteString("\nComponent source (" + d.Source.Confidence + "):\n")
		if d.Source.Name != "" {
			fmt.Fprintf(&b, "- component: %s\n", d.Source.Name)
		}
		if d.Source.File != "" {
			location := d.Source.File
			if d.Source.Line > 0 {
				location += fmt.Sprintf(":%d", d.Source.Line)
				if d.Source.Column > 0 {
					location += fmt.Sprintf(":%d", d.Source.Column)
				}
			}
			fmt.Fprintf(&b, "- file: %s\n", location)
		}
		if len(d.Source.Owners) > 0 {
			fmt.Fprintf(&b, "- owners: %s\n", strings.Join(d.Source.Owners, " < "))
		}
	}
	styles := d.Styles
	if len(styles.Computed) > 0 || len(styles.Classes) > 0 || styles.Inline != "" || len(styles.Parent) > 0 {
		b.WriteString("\nStyles (only those that differ from the browser's defaults):\n")
		writeStyleMap(&b, styles.Computed, "- ")
		if len(styles.Classes) > 0 {
			fmt.Fprintf(&b, "- classes: %s\n", strings.Join(styles.Classes, " "))
		}
		if styles.Inline != "" {
			fmt.Fprintf(&b, "- inline style: %s\n", styles.Inline)
		}
		if len(styles.Parent) > 0 {
			b.WriteString("- parent layout:\n")
			writeStyleMap(&b, styles.Parent, "  - ")
		}
	}
	if d.HasScreenshot {
		b.WriteString("\nThe screenshot crops the page around the element, which is outlined.\n")
	}
	return b.String()
}

func writeStyleMap(b *strings.Builder, styles map[string]string, prefix string) {
	keys := make([]string, 0, len(styles))
	for key := range styles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "%s%s: %s\n", prefix, key, styles[key])
	}
}

// Subscribe gives an origin to a task slot. The newest subscriber takes
// over and the slot that had it is told. Pins waiting on the origin are
// reported in the result and count as delivered.
func (s *ScopeService) Subscribe(ctx context.Context, taskID, slot, origin string) (ScopeSubscribeResult, error) {
	origin, err := NormalizeScopeOrigin(origin)
	if err != nil {
		return ScopeSubscribeResult{}, err
	}
	slot = slotOrMain(slot)
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, err := s.store.SetScopeSubscription(ctx, store.ScopeSubscription{Origin: origin, TaskID: taskID, Slot: slot, CreatedAt: s.stamp()})
	if err != nil {
		return ScopeSubscribeResult{}, err
	}
	result := ScopeSubscribeResult{Origin: origin}
	if previous != nil {
		result.TookOverFrom = AgentSlotKey(previous.TaskID, previous.Slot)
		if s.messenger != nil {
			notice := fmt.Sprintf("[legato scope] The agent of %s subscribed to %s, so batches the operator sends from it now go there, not to you. Subscribe again with page_pin_subscribe if you still serve it.", AgentSlotKey(taskID, slot), origin)
			if err := s.messenger.QueueMessage(WithAgentSlot(ctx, previous.Slot), previous.TaskID, notice); err != nil {
				log.Printf("scope: telling %s it lost %s: %v", result.TookOverFrom, origin, err)
			}
		}
	}
	waiting, err := s.store.ListScopePins(ctx, store.ScopePinFilter{Origins: []string{origin}, States: []string{store.ScopeSent}})
	if err != nil {
		return result, err
	}
	result.Waiting = scopePins(waiting)
	if len(waiting) > 0 {
		if err := s.store.MarkScopePinsDelivered(ctx, pinIDsOf(result.Waiting), s.stamp()); err != nil {
			return result, err
		}
	}
	s.changed(origin)
	return result, nil
}

// Unsubscribe ends a slot's subscription to an origin; ErrNotFound when
// the slot does not hold it.
func (s *ScopeService) Unsubscribe(ctx context.Context, taskID, slot, origin string) error {
	origin, err := NormalizeScopeOrigin(origin)
	if err != nil {
		return err
	}
	ok, err := s.store.DeleteScopeSubscription(ctx, origin, taskID, slotOrMain(slot))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("this agent does not subscribe to %s: %w", origin, store.ErrNotFound)
	}
	s.changed(origin)
	return nil
}

// SubscriptionsOf returns a slot's origins.
func (s *ScopeService) SubscriptionsOf(ctx context.Context, taskID, slot string) ([]string, error) {
	subs, err := s.store.ScopeSubscriptionsOfSlot(ctx, taskID, slotOrMain(slot))
	if err != nil {
		return nil, err
	}
	origins := make([]string, len(subs))
	for i, sub := range subs {
		origins[i] = sub.Origin
	}
	return origins, nil
}

// ListOrigin returns an origin's pins, for the extension.
func (s *ScopeService) ListOrigin(ctx context.Context, origin string) ([]ScopePin, error) {
	origin, err := NormalizeScopeOrigin(origin)
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListScopePins(ctx, store.ScopePinFilter{Origins: []string{origin}})
	if err != nil {
		return nil, err
	}
	return scopePins(records), nil
}

// ListForSlot returns the pins on a slot's origins, or on a named origin,
// optionally in one state. Sent pins on the slot's own origins count as
// delivered once listed.
func (s *ScopeService) ListForSlot(ctx context.Context, taskID, slot, origin, state string) ([]ScopePin, error) {
	owned, err := s.SubscriptionsOf(ctx, taskID, slot)
	if err != nil {
		return nil, err
	}
	origins := owned
	if origin != "" {
		if origin, err = NormalizeScopeOrigin(origin); err != nil {
			return nil, err
		}
		origins = []string{origin}
	}
	filter := store.ScopePinFilter{Origins: origins}
	s.mu.Lock()
	defer s.mu.Unlock()
	if state != "" {
		switch state {
		case store.ScopeSent, store.ScopeDelivered, store.ScopeFixed, store.ScopeWontFix:
		default:
			return nil, fmt.Errorf("%w: state %q (want sent, delivered, fixed or wont_fix)", ErrScopeInvalid, state)
		}
		filter.States = []string{state}
	}
	records, err := s.store.ListScopePins(ctx, filter)
	if err != nil {
		return nil, err
	}
	pins := scopePins(records)
	ownedSet := map[string]bool{}
	for _, o := range owned {
		ownedSet[o] = true
	}
	var deliver []string
	var touched []string
	for i := range pins {
		if pins[i].State == store.ScopeSent && ownedSet[pins[i].Origin] {
			deliver = append(deliver, pins[i].ID)
			touched = append(touched, pins[i].Origin)
			pins[i].State = store.ScopeDelivered
		}
	}
	if len(deliver) > 0 {
		if err := s.store.MarkScopePinsDelivered(ctx, deliver, s.stamp()); err != nil {
			return nil, err
		}
		s.changed(touched...)
	}
	return pins, nil
}

// Get returns a pin with all it captured.
func (s *ScopeService) Get(ctx context.Context, pinID string) (ScopePinDetail, error) {
	record, err := s.store.GetScopePin(ctx, pinID)
	if err != nil {
		return ScopePinDetail{}, err
	}
	return scopeDetail(*record), nil
}

// Screenshot returns a pin's screenshot and its media type; ErrNotFound
// when it has none.
func (s *ScopeService) Screenshot(ctx context.Context, pinID string) ([]byte, string, error) {
	record, err := s.store.GetScopePin(ctx, pinID)
	if err != nil {
		return nil, "", err
	}
	if record.Screenshot == "" || filepath.Base(record.Screenshot) != record.Screenshot {
		return nil, "", fmt.Errorf("screenshot of pin %s: %w", pinID, store.ErrNotFound)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, record.Screenshot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("screenshot of pin %s: %w", pinID, store.ErrNotFound)
	}
	if err != nil {
		return nil, "", err
	}
	return data, http.DetectContentType(data), nil
}

func scopeStateError(err error) error {
	if errors.Is(err, store.ErrConflict) {
		return fmt.Errorf("%w: %v", ErrScopePinState, err)
	}
	return err
}

// Resolve marks a pin fixed or won't fix (outcome fixed or wont_fix) with a
// one-line note, on behalf of taskID's agent.
func (s *ScopeService) Resolve(ctx context.Context, pinID, outcome, note, taskID string) (ScopePin, error) {
	if outcome != store.ScopeFixed && outcome != store.ScopeWontFix {
		return ScopePin{}, fmt.Errorf("%w: outcome %q (want fixed or wont_fix)", ErrScopeInvalid, outcome)
	}
	note = oneLine(note)
	if note == "" {
		return ScopePin{}, fmt.Errorf("%w: say in a line what you did or why not", ErrScopeInvalid)
	}
	note = cutRunes(note, scopeMaxNoteRunes)
	if err := s.store.ResolveScopePin(ctx, pinID, outcome, note, taskID, s.stamp()); err != nil {
		return ScopePin{}, scopeStateError(err)
	}
	detail, err := s.Get(ctx, pinID)
	if err != nil {
		return ScopePin{}, err
	}
	s.changed(detail.Origin)
	return detail.ScopePin, nil
}

// Reopen puts a fixed or won't-fix pin back to sent with the operator's
// new comment, and pushes it again.
func (s *ScopeService) Reopen(ctx context.Context, pinID, comment string) (ScopePin, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return ScopePin{}, fmt.Errorf("%w: reopening needs a comment", ErrScopeInvalid)
	}
	if len([]rune(comment)) > scopeMaxCommentRunes {
		return ScopePin{}, fmt.Errorf("%w: the comment is longer than %d characters", ErrScopeTooLarge, scopeMaxCommentRunes)
	}
	if err := s.store.ReopenScopePin(ctx, pinID, comment, s.stamp()); err != nil {
		return ScopePin{}, scopeStateError(err)
	}
	detail, err := s.Get(ctx, pinID)
	if err != nil {
		return ScopePin{}, err
	}
	s.pushOrigin(ctx, detail.Origin, "")
	s.changed(detail.Origin)
	if detail, err = s.Get(ctx, pinID); err != nil {
		return ScopePin{}, err
	}
	return detail.ScopePin, nil
}

// Withdraw removes a pin no agent has been told of. It waits for a push
// in flight, which may be telling an agent of the pin.
func (s *ScopeService) Withdraw(ctx context.Context, pinID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.store.GetScopePin(ctx, pinID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSentScopePin(ctx, pinID); err != nil {
		return scopeStateError(err)
	}
	if record.Screenshot != "" && filepath.Base(record.Screenshot) == record.Screenshot {
		_ = os.Remove(filepath.Join(s.dir, record.Screenshot))
	}
	s.changed(record.Origin)
	return nil
}

// HandleAgentStarted pushes the pins waiting on the origins of a task's
// main slot to its new agent: after a resume, Reload or restart.
func (s *ScopeService) HandleAgentStarted(ctx context.Context, taskID string) {
	origins, err := s.SubscriptionsOf(ctx, taskID, store.SlotMain)
	if err != nil || len(origins) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushLocked(ctx, taskID, store.SlotMain, origins)
}

// HandleAgentDied ends a slot's subscriptions when its agent has ended for
// good. An interrupted agent keeps them until it is resumed or dismissed.
func (s *ScopeService) HandleAgentDied(ctx context.Context, p events.AgentDiedPayload) {
	if p.TaskID == "" {
		return
	}
	s.endIfGone(ctx, p.TaskID, slotOrMain(p.Slot))
}

// PruneEnded ends the subscriptions of slots with neither a running nor an
// interrupted agent, and of archived or deleted tasks.
func (s *ScopeService) PruneEnded(ctx context.Context) {
	subs, err := s.store.ListScopeSubscriptions(ctx)
	if err != nil {
		log.Printf("scope: listing subscriptions: %v", err)
		return
	}
	seen := map[string]bool{}
	for _, sub := range subs {
		key := AgentSlotKey(sub.TaskID, sub.Slot)
		if seen[key] {
			continue
		}
		seen[key] = true
		s.endIfGone(ctx, sub.TaskID, sub.Slot)
	}
}

func (s *ScopeService) endIfGone(ctx context.Context, taskID, slot string) {
	archived, err := s.store.TaskArchived(ctx, taskID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return
	}
	if err == nil && !archived {
		status, err := s.store.ScopeSlotStatus(ctx, taskID, slot)
		if err != nil || status != "" {
			return
		}
	}
	origins, err := s.store.DeleteScopeSubscriptionsOfSlot(ctx, taskID, slot)
	if err != nil {
		log.Printf("scope: ending the subscriptions of %s: %v", AgentSlotKey(taskID, slot), err)
		return
	}
	s.changed(origins...)
}

// StartEventLoop follows agents starting and ending until ctx ends, and
// prunes when the board changes: archiving a task stops its agents without
// an AgentDied when they were interrupted.
func (s *ScopeService) StartEventLoop(ctx context.Context) (stop func()) {
	if s.bus == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	died := s.bus.Subscribe(events.EventAgentDied)
	started := s.bus.Subscribe(events.EventAgentStarted)
	cards := s.bus.Subscribe(events.EventCardsRefreshed)
	go func() {
		s.PruneEnded(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-died:
				if !ok {
					return
				}
				if p, ok := event.Payload.(events.AgentDiedPayload); ok {
					s.HandleAgentDied(ctx, p)
				}
			case _, ok := <-cards:
				if !ok {
					return
				}
				s.PruneEnded(ctx)
			case event, ok := <-started:
				if !ok {
					return
				}
				if taskID, ok := event.Payload.(string); ok {
					s.HandleAgentStarted(ctx, taskID)
				} else if p, ok := event.Payload.(events.AgentDiedPayload); ok {
					s.HandleAgentStarted(ctx, p.TaskID)
				}
			}
		}
	}()
	return func() { cancel(); s.bus.Unsubscribe(died); s.bus.Unsubscribe(started); s.bus.Unsubscribe(cards) }
}

func slotOrMain(slot string) string {
	if slot == "" {
		return store.SlotMain
	}
	return slot
}
