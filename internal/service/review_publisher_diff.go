package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// pullFile is one file of a pull request's diff on GitHub. Patch is empty
// when GitHub gives none (a binary file, or a diff too large to show).
type pullFile struct {
	Filename string `json:"filename"`
	Patch    string `json:"patch"`
}

// pullFiles lists a pull request's files, up to GitHub's 3000.
func pullFiles(ctx context.Context, call *githubCall, repo string, number int) ([]pullFile, error) {
	var out []pullFile
	for page := 1; page <= 30; page++ {
		var files []pullFile
		if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100&page=%d", repo, number, page), nil, &files); err != nil {
			return nil, err
		}
		out = append(out, files...)
		if len(files) < 100 {
			break
		}
	}
	return out, nil
}

// PullDiff is the pull request's own diff at its head on GitHub, read as
// the reviewer App: the lines of the head each file's diff changes, first
// and last, by file. A file GitHub gives no patch for is changed in full.
func (p *ReviewPublisher) PullDiff(ctx context.Context, repo string, number int) (map[string][][2]int, error) {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return nil, err
	}
	defer release()
	files, err := pullFiles(ctx, call, repo, number)
	if err != nil {
		return nil, err
	}
	out := map[string][][2]int{}
	for _, f := range files {
		if f.Patch == "" {
			out[f.Filename] = [][2]int{{1, math.MaxInt}}
			continue
		}
		out[f.Filename] = patchChangedLines(f.Patch)
	}
	return out, nil
}

// patchChangedLines reads the head's changed lines from a file's patch:
// each added line, and for lines only deleted the lines either side of
// where they were, as GitReviewCheckout.Changed reads a diff.
func patchChangedLines(patch string) [][2]int {
	var ranges [][2]int
	add := func(first, last int) {
		first = max(first, 1)
		if n := len(ranges); n > 0 && first <= ranges[n-1][1]+1 {
			ranges[n-1][1] = max(ranges[n-1][1], last)
			return
		}
		ranges = append(ranges, [2]int{first, last})
	}
	right, inHunk, deleted := 0, false, false
	settle := func() {
		if deleted {
			add(right-1, right)
		}
		deleted = false
	}
	for _, line := range strings.Split(patch, "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			settle()
			right, _ = strconv.Atoi(m[2])
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			add(right, right)
			deleted = false
			right++
		case strings.HasPrefix(line, "-"):
			deleted = true
		case strings.HasPrefix(line, "\\"):
		default:
			settle()
			right++
		}
	}
	settle()
	return ranges
}
