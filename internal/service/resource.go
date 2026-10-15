package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// Resource is a named set of files and folders an agent can be given: an
// Obsidian vault, a folder of specs. Chimera is granted each path; other
// agents are told about them in their prompt.
type Resource struct {
	// ID is that of a saved resource, and 0 for one from the configuration.
	ID    int64          `json:"id,omitempty"`
	Name  string         `json:"name"`
	Paths []ResourcePath `json:"paths"`
	// Configured is true for a resource from the configuration, which cannot
	// be changed from Legato.
	Configured bool `json:"configured"`
}

// ResourcePath is one file or folder of a resource. Path is absolute;
// Access is "read" or "write".
type ResourcePath struct {
	Path   string `json:"path"`
	Access string `json:"access"`
}

// ResourceStore keeps the resources the person saved.
type ResourceStore interface {
	SaveResource(ctx context.Context, resource store.Resource) (store.Resource, error)
	ListResources(ctx context.Context) ([]store.Resource, error)
	DeleteResource(ctx context.Context, id int64) error
}

// ResourceService lists, saves and resolves resources.
type ResourceService struct {
	saved      ResourceStore
	configured []Resource
	home       string
	attacher   ResourceAttacher
}

// NewResourceService makes a resource service. saved may be nil, in which
// case only configured resources exist. home is what a leading ~ stands for.
func NewResourceService(saved ResourceStore, configured []Resource, home string) *ResourceService {
	s := &ResourceService{saved: saved, home: home}
	for _, resource := range configured {
		resource.ID = 0
		resource.Configured = true
		s.configured = append(s.configured, resource)
	}
	return s
}

// List returns the resources of the configuration, then the saved ones by
// name. A saved resource with the name of a configured one is not listed.
func (s *ResourceService) List(ctx context.Context) ([]Resource, error) {
	resources := append([]Resource(nil), s.configured...)
	if s.saved == nil {
		return resources, nil
	}
	saved, err := s.saved.ListResources(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range saved {
		if s.configuredNamed(row.Name) {
			continue
		}
		resources = append(resources, resourceFromRow(row))
	}
	return resources, nil
}

func (s *ResourceService) configuredNamed(name string) bool {
	for _, resource := range s.configured {
		if strings.EqualFold(resource.Name, name) {
			return true
		}
	}
	return false
}

// expandHome turns a leading ~ into the home directory.
func (s *ResourceService) expandHome(path string) string {
	if path == "~" {
		return s.home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(s.home, path[2:])
	}
	return path
}

// ErrResourceInvalid is returned for a resource that cannot be saved as it is.
var ErrResourceInvalid = errors.New("invalid resource")

func invalidResource(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrResourceInvalid, fmt.Sprintf(format, args...))
}

// Save keeps a resource: a new one when its ID is 0, otherwise the saved
// resource with that ID. Each path must exist; a leading ~ is expanded and
// access defaults to read.
func (s *ResourceService) Save(ctx context.Context, resource Resource) (Resource, error) {
	if s.saved == nil {
		return Resource{}, invalidResource("resources cannot be saved here")
	}
	resource.Name = strings.TrimSpace(resource.Name)
	if resource.Name == "" {
		return Resource{}, invalidResource("a name is required")
	}
	if s.configuredNamed(resource.Name) {
		return Resource{}, invalidResource("%q is defined in the configuration", resource.Name)
	}
	if len(resource.Paths) == 0 {
		return Resource{}, invalidResource("%q needs at least one path", resource.Name)
	}
	row := store.Resource{ID: resource.ID, Name: resource.Name}
	for _, entry := range resource.Paths {
		path := s.expandHome(strings.TrimSpace(entry.Path))
		if !filepath.IsAbs(path) {
			return Resource{}, invalidResource("%q is not an absolute path", entry.Path)
		}
		path = filepath.Clean(path)
		if _, err := os.Stat(path); err != nil {
			return Resource{}, invalidResource("%s does not exist", path)
		}
		access := entry.Access
		if access == "" {
			access = "read"
		}
		if access != "read" && access != "write" {
			return Resource{}, invalidResource("access must be read or write, got %q", access)
		}
		row.Paths = append(row.Paths, store.ResourcePath{Path: path, Access: access})
	}
	saved, err := s.saved.SaveResource(ctx, row)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Resource{}, invalidResource("a resource named %q already exists", resource.Name)
		}
		return Resource{}, err
	}
	return resourceFromRow(saved), nil
}

func resourceFromRow(row store.Resource) Resource {
	resource := Resource{ID: row.ID, Name: row.Name}
	for _, path := range row.Paths {
		resource.Paths = append(resource.Paths, ResourcePath{Path: path.Path, Access: path.Access})
	}
	return resource
}

// Delete removes a saved resource. Resources of the configuration have no ID
// and cannot be removed here.
func (s *ResourceService) Delete(ctx context.Context, id int64) error {
	if s.saved == nil || id == 0 {
		return invalidResource("only a saved resource can be removed")
	}
	return s.saved.DeleteResource(ctx, id)
}

// Resolve finds resources by name, ignoring case, each once and in the order
// they were first named. An unknown name is an error.
func (s *ResourceService) Resolve(ctx context.Context, names []string) ([]Resource, error) {
	if len(names) == 0 {
		return nil, nil
	}
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var resolved []Resource
	seen := map[string]bool{}
	for _, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		if seen[key] {
			continue
		}
		seen[key] = true
		found := false
		for _, resource := range all {
			if strings.ToLower(resource.Name) == key {
				resolved = append(resolved, resource)
				found = true
				break
			}
		}
		if !found {
			return nil, invalidResource("there is no resource named %q", name)
		}
	}
	return resolved, nil
}

// usableResources leaves out the paths that do not exist now (a disk that is
// not mounted, a folder that was moved), and the resources left with none.
// Chimera refuses to start with a path it cannot find.
func usableResources(resources []Resource) []Resource {
	var usable []Resource
	for _, resource := range resources {
		kept := resource
		kept.Paths = nil
		for _, path := range resource.Paths {
			if _, err := os.Stat(path.Path); err == nil {
				kept.Paths = append(kept.Paths, path)
			}
		}
		if len(kept.Paths) > 0 {
			usable = append(usable, kept)
		}
	}
	return usable
}

// resourcesPrompt tells an agent which files and folders it was given, so the
// person does not have to name them in every session.
func resourcesPrompt(resources []Resource) string {
	if len(resources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Resources\n\n")
	b.WriteString("The person gave you these files and folders to use beside your working directory. Read them when they bear on the work; change the ones marked read and write only when the work calls for it.\n")
	for _, resource := range resources {
		fmt.Fprintf(&b, "\n- %s\n", resource.Name)
		for _, path := range resource.Paths {
			access := "read only"
			if path.Access == "write" {
				access = "read and write"
			}
			fmt.Fprintf(&b, "  - `%s` (%s)\n", path.Path, access)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// resourceGrants lists each path of the resources once, by what it may be
// used for. A path that one resource gives to read and another to write is
// written.
func resourceGrants(resources []Resource) (read, write []string) {
	writable := map[string]bool{}
	for _, resource := range resources {
		for _, path := range resource.Paths {
			if path.Access == "write" {
				writable[path.Path] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, resource := range resources {
		for _, path := range resource.Paths {
			if seen[path.Path] {
				continue
			}
			seen[path.Path] = true
			if writable[path.Path] {
				write = append(write, path.Path)
			} else {
				read = append(read, path.Path)
			}
		}
	}
	return read, write
}

// SetAttacher lets the service give resources to running agents.
func (s *ResourceService) SetAttacher(attacher ResourceAttacher) {
	s.attacher = attacher
}

// Attach gives the running agent of a task the named resources.
func (s *ResourceService) Attach(ctx context.Context, taskID string, names []string) error {
	if s.attacher == nil {
		return invalidResource("resources cannot be added to running agents here")
	}
	resources, err := s.Resolve(ctx, names)
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return invalidResource("name at least one resource")
	}
	return s.attacher.AttachResources(ctx, taskID, resources)
}

// Attached names the resources the running agent of a task was given.
func (s *ResourceService) Attached(ctx context.Context, taskID string) ([]string, error) {
	if s.attacher == nil {
		return nil, nil
	}
	return s.attacher.AttachedResources(ctx, taskID)
}

// AddPaths adds paths to the saved resource of that name, creating it when
// there is none. A path it already has takes the access given here.
func (s *ResourceService) AddPaths(ctx context.Context, name string, paths []ResourcePath) (Resource, error) {
	existing, err := s.savedNamed(ctx, name)
	if err != nil {
		return Resource{}, err
	}
	resource := Resource{Name: name}
	if existing != nil {
		resource = *existing
	}
	for _, path := range paths {
		path.Path = filepath.Clean(s.expandHome(strings.TrimSpace(path.Path)))
		replaced := false
		for i := range resource.Paths {
			if resource.Paths[i].Path == path.Path {
				resource.Paths[i].Access = path.Access
				replaced = true
			}
		}
		if !replaced {
			resource.Paths = append(resource.Paths, path)
		}
	}
	return s.Save(ctx, resource)
}

// RemoveNamed removes the saved resource of that name.
func (s *ResourceService) RemoveNamed(ctx context.Context, name string) error {
	existing, err := s.savedNamed(ctx, name)
	if err != nil {
		return err
	}
	if existing == nil {
		if s.configuredNamed(name) {
			return invalidResource("%q is defined in the configuration; remove it there", name)
		}
		return invalidResource("there is no resource named %q", name)
	}
	return s.Delete(ctx, existing.ID)
}

func (s *ResourceService) savedNamed(ctx context.Context, name string) (*Resource, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, resource := range all {
		if !resource.Configured && strings.EqualFold(resource.Name, strings.TrimSpace(name)) {
			return &resource, nil
		}
	}
	return nil, nil
}
