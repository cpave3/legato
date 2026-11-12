package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReeveSeedTemplate is the seed Legato issues the Reeve's engram with when
// its store does not have it yet. From then on the seed is engram's
// (identity.md); Legato never writes it again unless the person, with a
// signature, asks it to.
const ReeveSeedTemplate = `You are the Reeve of a Legato instance: the agent of that instance itself. You run and configure it with the operator who owns it: its settings, its restarts, its workers and integrations, its logs, and the machine it runs on when that is needed. You do not work on tasks in repositories; other agents do.

Keep what you learn about this instance, so that the next session starts where this one ended: the operator's preferences, why settings are as they are, the problems you met and how they were fixed, and the work left open as threads. Keep the why and the how: Legato's own database already records what changed, who approved it and how it ran.

Never keep a secret, or anything that would help someone find one.
`

// ReeveEngramFor is the Reeve's engram on an instance: reeve- and the first
// eight characters of the instance ID after its srv- prefix, in the store
// inside the Reeve's folder.
func ReeveEngramFor(instanceID, folder string) ReeveEngram {
	id := strings.TrimPrefix(instanceID, "srv-")
	if len(id) > 8 {
		id = id[:8]
	}
	return ReeveEngram{Name: "reeve-" + strings.ToLower(id), Home: filepath.Join(folder, "engram"), Seed: ReeveSeedTemplate}
}

// reeveSeedTemplates is every built-in seed Legato has shipped, oldest
// first, the current one last. A seed that is one of them was never edited:
// when it is an older one, Settings offers the current one. Keep the old
// ones when the template changes.
var reeveSeedTemplates = []string{ReeveSeedTemplate}

// ApprovalSourceReeveSeed is the approval of a change to the Reeve's seed,
// one of its own rules: it needs a signature.
const ApprovalSourceReeveSeed = "reeve_seed"

// reeveSeedTemplateKey records, in instance_values, the hash of the
// built-in seed current when a signed edit last wrote the seed.
const reeveSeedTemplateKey = "reeve_seed_template"

// ReeveEngramOptions wire a ReeveEngramService.
type ReeveEngramOptions struct {
	// Engram is this instance's Reeve engram, or why there is none.
	Engram    func(ctx context.Context) (ReeveEngram, error)
	Engrams   *EngramService
	Approvals *ApprovalService
	Store     *store.Store
}

// ReeveEngramService is the Reeve's engram as Settings shows it: its seed,
// which the person changes with a signature, and its memories and open
// threads, which the person can remove. Editing a memory stays with the
// engram CLI.
type ReeveEngramService struct {
	opts      ReeveEngramOptions
	templates []string
}

func NewReeveEngramService(opts ReeveEngramOptions) *ReeveEngramService {
	return &ReeveEngramService{opts: opts, templates: reeveSeedTemplates}
}

// Register makes the approval service write signed seeds.
func (s *ReeveEngramService) Register() {
	s.opts.Approvals.RegisterResolver(ApprovalSourceReeveSeed, reeveSeedResolver{s})
}

// ReeveSeedView is the Reeve's seed.
type ReeveSeedView struct {
	// Text is the engram's seed, or the built-in one before it exists.
	Text string `json:"text"`
	// Edited is a seed that is none of Legato's built-in ones.
	Edited bool `json:"edited"`
	// Template is this version's built-in seed.
	Template string `json:"template"`
	// Newer says the built-in seed changed since the seed was written.
	Newer bool `json:"newer"`
}

// ReeveEngramView is the Reeve's engram in Settings.
type ReeveEngramView struct {
	// Enabled says the Reeve has an engram here; Error says why not.
	Enabled bool   `json:"enabled"`
	Error   string `json:"error,omitempty"`
	Name    string `json:"name"`
	Store   string `json:"store"`
	// Exists is false until its first session issues it.
	Exists   bool            `json:"exists"`
	Seed     ReeveSeedView   `json:"seed"`
	Memories []engram.Memory `json:"memories"`
	Threads  []engram.Thread `json:"threads"`
}

func (s *ReeveEngramService) current() string { return s.templates[len(s.templates)-1] }

// seedPath is where engram keeps an engram's seed.
func seedPath(e ReeveEngram) string {
	return filepath.Join(e.Home, "identities", e.Name, "identity.md")
}

// splitSeed parts identity.md into engram's header and the seed.
func splitSeed(data string) (header, seed string) {
	if rest, ok := strings.CutPrefix(data, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			return data[:4+i+5], strings.TrimPrefix(rest[i+5:], "\n")
		}
	}
	return "", data
}

func sameSeed(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

func seedHash(seed string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(seed)))
	return hex.EncodeToString(sum[:])
}

// engram is the Reeve's engram and the engram client for its store.
func (s *ReeveEngramService) engram(ctx context.Context) (ReeveEngram, EngramCLI, error) {
	if s.opts.Engram == nil {
		return ReeveEngram{}, nil, errors.New("this Legato gives the Reeve no engram")
	}
	e, err := s.opts.Engram(ctx)
	if err != nil {
		return ReeveEngram{}, nil, err
	}
	cli, err := s.opts.Engrams.In(ctx, e.Home)
	return e, cli, err
}

// View is the Reeve's engram: whether it has one, its seed, memories and
// open threads.
func (s *ReeveEngramService) View(ctx context.Context) (ReeveEngramView, error) {
	view := ReeveEngramView{Memories: []engram.Memory{}, Threads: []engram.Thread{}}
	e, cli, err := s.engram(ctx)
	view.Name, view.Store = e.Name, e.Home
	view.Seed = ReeveSeedView{Text: s.current(), Template: s.current()}
	if err != nil {
		view.Error = engramProblem(err)
		return view, nil
	}
	view.Enabled = true
	data, err := os.ReadFile(seedPath(e))
	if errors.Is(err, fs.ErrNotExist) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Exists = true
	_, view.Seed.Text = splitSeed(string(data))
	view.Seed.Edited = !slices.ContainsFunc(s.templates, func(t string) bool { return sameSeed(t, view.Seed.Text) })
	if view.Seed.Edited {
		written, _ := s.opts.Store.GetInstanceValue(ctx, reeveSeedTemplateKey)
		view.Seed.Newer = written != "" && written != seedHash(s.current())
	} else {
		view.Seed.Newer = !sameSeed(view.Seed.Text, s.current())
	}
	if view.Memories, err = cli.Memories(ctx, e.Name); err != nil {
		view.Error = engramProblem(err)
	}
	if view.Threads, err = cli.Threads(ctx, e.Name); err != nil {
		view.Error = engramProblem(err)
	}
	if view.Memories == nil {
		view.Memories = []engram.Memory{}
	}
	if view.Threads == nil {
		view.Threads = []engram.Thread{}
	}
	return view, nil
}

// reeveSeedChange is a seed the person asked for, in the approval's
// details, so the signature covers it.
type reeveSeedChange struct {
	Engram string `json:"engram"`
	Seed   string `json:"seed"`
}

// ProposeSeed puts a new seed to the person as a signed approval: it is one
// of the Reeve's own rules. Refused when no device is enrolled.
func (s *ReeveEngramService) ProposeSeed(ctx context.Context, seed, by string) (*Approval, error) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return nil, errors.New("the seed is empty")
	}
	e, _, err := s.engram(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := s.opts.Store.ListApprovalDevices(ctx, store.DeviceActive)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, ErrNoDeviceEnrolled
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	approval, _, err := s.opts.Approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceReeveSeed, SourceKey: hex.EncodeToString(b),
		Title: "Change " + s.opts.Approvals.ReeveName(ctx).Mid() + "'s seed", Summary: seed,
		Details: reeveSeedChange{Engram: e.Name, Seed: seed}, Signed: true,
		Options: []ApprovalOption{{ID: "allow", Label: "Allow", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
	})
	_ = by
	return approval, err
}

// writeSeed writes a signed seed: into identity.md, keeping engram's
// header, or by issuing the engram with it when it does not exist yet.
func (s *ReeveEngramService) writeSeed(ctx context.Context, change reeveSeedChange) error {
	e, cli, err := s.engram(ctx)
	if err != nil {
		return err
	}
	if change.Engram != e.Name {
		return fmt.Errorf("the seed was for %s, and the Reeve's engram is now %s", change.Engram, e.Name)
	}
	seed := strings.TrimSpace(change.Seed) + "\n"
	path := seedPath(e)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, err := cli.NewIdentity(ctx, e.Name, seed); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		header, _ := splitSeed(string(data))
		if err := writeFileAtomic(path, []byte(header+"\n"+seed)); err != nil {
			return err
		}
	}
	return s.opts.Store.SetInstanceValue(ctx, reeveSeedTemplateKey, seedHash(s.current()))
}

// writeFileAtomic replaces a file through a temporary one beside it,
// keeping its mode.
func writeFileAtomic(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".identity-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

type reeveSeedResolver struct{ s *ReeveEngramService }

func (r reeveSeedResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	if decision.Decision != ApprovalAllow {
		return nil
	}
	var change reeveSeedChange
	if err := json.Unmarshal(approval.Details, &change); err != nil {
		return err
	}
	return r.s.writeSeed(ctx, change)
}

// RemoveMemory deletes one of the Reeve's memories for good.
func (s *ReeveEngramService) RemoveMemory(ctx context.Context, id string) error {
	e, cli, err := s.engram(ctx)
	if err != nil {
		return err
	}
	return cli.RemoveMemory(ctx, e.Name, id)
}

// CloseThread removes one of the Reeve's open threads: engram closes it,
// saying the person removed it.
func (s *ReeveEngramService) CloseThread(ctx context.Context, id string) error {
	e, cli, err := s.engram(ctx)
	if err != nil {
		return err
	}
	return cli.CloseThread(ctx, e.Name, id, "Removed by the operator in Legato's Settings.")
}
