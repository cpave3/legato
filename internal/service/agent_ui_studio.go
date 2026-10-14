package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/libretto"
)

// AgentUIFormatStudio is a libretto studio: a panel in which the person
// records the scenes of a storyboard in their own voice, beside the
// conversation in which they and the agent agreed what the scenes are. The
// item's content says where the storyboard file is. Legato keeps no copy of
// it: the agent goes on editing that file, and the studio reads it as it is.
const AgentUIFormatStudio = "studio"

// ErrAgentUINotAStudio marks a request for the studio of an item that is
// something else.
var ErrAgentUINotAStudio = errors.New("the item is not a studio")

// agentUIStudio is the content of a studio item.
type agentUIStudio struct {
	Storyboard string `json:"storyboard"`
	// Scene is the scene the agent last put in front of the person; empty
	// when it named none. The panel turns to it when the item is shown again.
	Scene string `json:"scene,omitempty"`
}

// AgentUIStudioContent is the content of a studio item for a storyboard
// file. scene names a scene to put in front of the person, or is empty.
func AgentUIStudioContent(storyboard, scene string) string {
	data, _ := json.Marshal(agentUIStudio{Storyboard: storyboard, Scene: strings.TrimSpace(scene)})
	return string(data)
}

// AgentUIStoryboard returns the storyboard file a studio item is for; ok is
// false when the item is not a studio.
func AgentUIStoryboard(item *AgentUIItem) (path string, ok bool) {
	if item == nil || item.Format != AgentUIFormatStudio {
		return "", false
	}
	var studio agentUIStudio
	if err := json.Unmarshal([]byte(item.Content), &studio); err != nil || studio.Storyboard == "" {
		return "", false
	}
	return studio.Storyboard, true
}

// StudioNote is what a task's agent is told when the person does something
// in the studio that is the agent's to act on: a take to review, a comment
// to deal with, a scene they removed. method and rest are the studio request
// that did it, and answer is what the studio answered: the storyboard as it
// is after. ok is false when nothing needs saying.
//
// The person is presenting, not typing, so the agent is asked to act without
// being asked by them, and to keep out of the conversation unless it needs
// them.
func StudioNote(storyboard, method, rest string, answer []byte) (note string, ok bool) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	asked := func(what string) (string, bool) {
		return fmt.Sprintf("[legato studio] The person %s in the studio, on the storyboard %s. libretto validate lists the comments that wait for you. "+
			"Read the storyboard again, do what each comment asks, and delete it when it is done. Then say in a sentence what you changed.", what, storyboard), true
	}
	switch {
	case len(parts) < 2 || parts[0] != "api":
		return "", false
	case method == http.MethodPost && len(parts) == 2 && parts[1] == "brief":
		return fmt.Sprintf("[legato studio] The person wrote what they want the video to cover, in the brief of the storyboard %s. Draft the plan from it now. "+
			"Give it chapters and scenes, and each scene talking points (notes: two to four short lines starting with \"- \") and a shows line that says in a few words what its picture will be. "+
			"Draw no pictures yet: a scene with shows and no picture is planned, the studio shows it as a placeholder, and they will change the plan before they agree it. "+
			"Keep any scene that is already recorded, and what they wrote by hand. Then tell them in a sentence or two what you propose.", storyboard), true
	case method == http.MethodPost && len(parts) == 2 && parts[1] == "agree":
		return asked("agreed the plan")
	case method == http.MethodPost && len(parts) == 2 && parts[1] == "scenes":
		return asked("added a scene to the plan")
	case parts[1] != "scenes" || len(parts) < 3:
		return "", false
	}
	scene := parts[2]
	switch {
	case method == http.MethodPatch && len(parts) == 3:
		return asked(fmt.Sprintf("changed what the scene %q is to show", scene))
	case method == http.MethodPost && len(parts) == 4 && parts[3] == "takes":
		var project struct {
			Scenes []struct {
				ID   string `json:"id"`
				Take *struct {
					Src string `json:"src"`
				} `json:"take"`
			} `json:"scenes"`
		}
		if err := json.Unmarshal(answer, &project); err != nil {
			return "", false
		}
		for _, sc := range project.Scenes {
			if sc.ID != scene || sc.Take == nil || sc.Take.Src == "" {
				continue
			}
			take := sc.Take.Src
			if !filepath.IsAbs(take) {
				take = filepath.Join(filepath.Dir(storyboard), take)
			}
			return fmt.Sprintf("[legato studio] The person recorded a take in the studio: scene %q, %s, of the storyboard %s. Review it now, without being asked.\n"+
				"1. Read what they said: libretto transcribe %s\n"+
				"2. Write what you make of it with libretto review (its --help has the format): where they covered each talking point, and each stretch that left the plan, with a note to them that says what they went into and what could be done with it. The studio shows it over their transcript, and they decide.\n"+
				"3. Bring the scene in line with what they said: its narration, a marker before each word where something should happen, and a skip in the take for a false start or a stumble.\n"+
				"Do not add, remove or split scenes for what left the plan until they have decided: a comment on the scene then asks you. "+
				"Read the storyboard again before every edit, because the studio writes to it as they record. They are presenting, so say nothing in the conversation unless you need them.",
				scene, take, storyboard, take), true
		}
		return "", false
	case method == http.MethodPost && len(parts) == 4 && parts[3] == "comments",
		method == http.MethodPost && len(parts) == 5 && parts[3] == "offplan":
		return asked(fmt.Sprintf("left you a comment on the scene %q", scene))
	case method == http.MethodDelete && len(parts) == 3:
		return asked(fmt.Sprintf("removed the scene %q", scene))
	}
	return "", false
}

// validateAgentUIStudio checks a request to show a studio: it must say where
// a storyboard file is, and the file must be there.
func validateAgentUIStudio(in AgentUIShowInput) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentUIInvalid, fmt.Sprintf(format, args...))
	}
	if len(in.Images) > 0 || in.Video != "" {
		return invalid("a studio takes no image or video files")
	}
	var studio agentUIStudio
	if err := json.Unmarshal([]byte(in.Content), &studio); err != nil || strings.TrimSpace(studio.Storyboard) == "" {
		return invalid("a studio item does not say where the storyboard is")
	}
	if !filepath.IsAbs(studio.Storyboard) {
		return invalid("the storyboard %q must be an absolute path", studio.Storyboard)
	}
	if info, err := os.Stat(studio.Storyboard); err != nil || !info.Mode().IsRegular() {
		return invalid("there is no storyboard file at %s", studio.Storyboard)
	}
	return nil
}

// StudioRunner is the part of the libretto command a studio needs.
type StudioRunner interface {
	Probe(ctx context.Context) (libretto.Info, error)
	StartStudio(ctx context.Context, storyboard string) (*libretto.Studio, error)
}

// AgentStudioService runs the libretto studios behind studio items. A studio
// is a process of libretto's, started the first time its panel is opened and
// kept until Legato stops. One storyboard has one studio, however many items
// name it.
type AgentStudioService struct {
	ui  *AgentUIService
	run StudioRunner

	mu      sync.Mutex
	studios map[string]*libretto.Studio // by storyboard path
}

// NewAgentStudioService returns the service. Nothing is started until a
// studio is asked for.
func NewAgentStudioService(ui *AgentUIService, run StudioRunner) *AgentStudioService {
	return &AgentStudioService{ui: ui, run: run, studios: map[string]*libretto.Studio{}}
}

// Address returns where the studio of a studio item listens, starting it if
// it is not running. The error is libretto.ErrNotInstalled or a
// *libretto.TooOldError when this machine's libretto cannot serve one, and
// ErrAgentUINotAStudio when the item is not a studio.
func (s *AgentStudioService) Address(ctx context.Context, itemID string) (string, error) {
	item, err := s.ui.Get(ctx, itemID, 0)
	if err != nil {
		return "", err
	}
	storyboard, ok := AgentUIStoryboard(item)
	if !ok {
		return "", ErrAgentUINotAStudio
	}
	// One studio is started at a time. Starting takes a second or two, and
	// two panels opened together must not start two.
	s.mu.Lock()
	defer s.mu.Unlock()
	if running := s.studios[storyboard]; running != nil {
		select {
		case <-running.Exited:
			delete(s.studios, storyboard)
		default:
			return running.URL, nil
		}
	}
	// Asked each time a studio is started, so that libretto installed or
	// updated while Legato runs is found without a restart.
	if _, err := s.run.Probe(ctx); err != nil {
		return "", err
	}
	started, err := s.run.StartStudio(ctx, storyboard)
	if err != nil {
		return "", err
	}
	s.studios[storyboard] = started
	return started.URL, nil
}

// Close stops every studio that is running.
func (s *AgentStudioService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for storyboard, running := range s.studios {
		select {
		case <-running.Exited:
		default:
			running.Stop()
		}
		delete(s.studios, storyboard)
	}
}
