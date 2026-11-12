package dockerproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// Cleaner removes what executions made from a daemon.
type Cleaner struct {
	Upstream Upstream
}

func (c Cleaner) do(ctx context.Context, method, path string, v any) error {
	client := &http.Client{Transport: c.Upstream.transport(), Timeout: 2 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && method == http.MethodDelete {
		return nil // already gone
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, body)
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

func labelFilter(value string) string {
	filter := Label
	if value != "" {
		filter += "=" + value
	}
	data, _ := json.Marshal(map[string][]string{"label": {filter}})
	return url.QueryEscape(string(data))
}

// Clean removes the containers an execution made (stopping them), then its
// networks and volumes. Images stay: they are shared.
func (c Cleaner) Clean(ctx context.Context, execution string) error {
	if execution == "" {
		return errors.New("no execution to clean")
	}
	filter := labelFilter(execution)
	var errs []error
	var containers []struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodGet, "/containers/json?all=1&filters="+filter, &containers); err != nil {
		return err
	}
	for _, ct := range containers {
		errs = append(errs, c.do(ctx, http.MethodDelete, "/containers/"+ct.ID+"?force=1&v=1", nil))
	}
	var networks []struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodGet, "/networks?filters="+filter, &networks); err != nil {
		errs = append(errs, err)
	}
	for _, n := range networks {
		errs = append(errs, c.do(ctx, http.MethodDelete, "/networks/"+n.ID, nil))
	}
	var volumes struct{ Volumes []struct{ Name string } }
	if err := c.do(ctx, http.MethodGet, "/volumes?filters="+filter, &volumes); err != nil {
		errs = append(errs, err)
	}
	for _, v := range volumes.Volumes {
		errs = append(errs, c.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(v.Name)+"?force=1", nil))
	}
	return errors.Join(errs...)
}

// Executions lists the executions that have containers, networks or volumes
// on the daemon, by their label.
func (c Cleaner) Executions(ctx context.Context) ([]string, error) {
	filter := labelFilter("")
	seen := map[string]bool{}
	var containers []struct{ Labels map[string]string }
	if err := c.do(ctx, http.MethodGet, "/containers/json?all=1&filters="+filter, &containers); err != nil {
		return nil, err
	}
	for _, ct := range containers {
		seen[ct.Labels[Label]] = true
	}
	var networks []struct{ Labels map[string]string }
	if err := c.do(ctx, http.MethodGet, "/networks?filters="+filter, &networks); err != nil {
		return nil, err
	}
	for _, n := range networks {
		seen[n.Labels[Label]] = true
	}
	var volumes struct {
		Volumes []struct{ Labels map[string]string }
	}
	if err := c.do(ctx, http.MethodGet, "/volumes?filters="+filter, &volumes); err != nil {
		return nil, err
	}
	for _, v := range volumes.Volumes {
		seen[v.Labels[Label]] = true
	}
	delete(seen, "")
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
