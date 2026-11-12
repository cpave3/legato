package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// RepositoryResourceSource gives the resources every agent working in a
// repository (host/owner/name) gets, as if they were ticked when it started.
type RepositoryResourceSource interface {
	ResourcesForRepository(ctx context.Context, repo string) ([]Resource, error)
}

// RepositoryResources is agents.repository_resources: the configuration's
// resources, by repository. config.Load has normalised the repositories and
// checked every name is a configured resource.
type RepositoryResources struct {
	byRepo map[string][]Resource
}

// NewRepositoryResources resolves each entry's names against the
// configuration's resources. A name that is not there is left out (Load
// refuses one, so it is never there).
func NewRepositoryResources(entries []config.RepositoryResources, resources []config.ResourceConfig) *RepositoryResources {
	r := &RepositoryResources{byRepo: map[string][]Resource{}}
	for _, entry := range entries {
		for _, name := range entry.Resources {
			for _, resource := range resources {
				if resource.Name != name {
					continue
				}
				item := Resource{Name: resource.Name, Configured: true}
				for _, path := range resource.Paths {
					item.Paths = append(item.Paths, ResourcePath{Path: path.Path, Access: path.Access})
				}
				r.byRepo[entry.Repo] = append(r.byRepo[entry.Repo], item)
			}
		}
	}
	return r
}

// ResourcesForRepository implements RepositoryResourceSource.
func (r *RepositoryResources) ResourcesForRepository(_ context.Context, repo string) ([]Resource, error) {
	if r == nil {
		return nil, nil
	}
	return append([]Resource(nil), r.byRepo[repo]...), nil
}

// mergeResources is chosen, then each of extra whose name (ignoring case) is
// not already there.
func mergeResources(chosen, extra []Resource) (all, added []Resource) {
	all = append([]Resource(nil), chosen...)
	seen := map[string]bool{}
	for _, resource := range chosen {
		seen[lowerName(resource.Name)] = true
	}
	for _, resource := range extra {
		if key := lowerName(resource.Name); !seen[key] {
			seen[key] = true
			all = append(all, resource)
			added = append(added, resource)
		}
	}
	return all, added
}

func lowerName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// spawnProjectKey is the project an agent about to start works in: the
// repository of the clone it was given, else its task's code context, else
// its checkout's origin remote (ResolveProjectIdentity). "" when none is
// found; an error only for a task whose code context cannot be read.
func (a *agentService) spawnProjectKey(ctx context.Context, taskID, workDir, cloneRepo string) (string, error) {
	if repo, ok := config.NormalizeRepository(cloneRepo); ok {
		return repositoryIdentity(repo).Key, nil
	}
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return "", nil
	}
	codeContext, err := store.ParseCodeContext(task.CodeContext)
	if err != nil {
		return "", fmt.Errorf("parsing task code context: %w", err)
	}
	identity, err := ResolveProjectIdentity(codeContext, workDir, a.checkoutInspector)
	if err != nil {
		return "", nil
	}
	return identity.Key, nil
}

// ChosenResources names the resources the running agent of a task was given
// by choice, not by its repository: what a restart carries, since the spawn
// works the repository's out again.
func (a *agentService) ChosenResources(ctx context.Context, taskID string) ([]string, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if session.ExecutionID == "" {
		return nil, nil
	}
	return a.store.ListChosenExecutionResources(ctx, session.ExecutionID)
}
