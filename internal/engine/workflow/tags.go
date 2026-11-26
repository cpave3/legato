package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxTags is how many tags a workflow may have.
const MaxTags = 8

// tagPattern is a tag: a lower-case letter or digit, then up to 31 more of
// those or -.
var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidateTags checks a workflow's tags: at most MaxTags, each a short
// lower-case word, none twice.
func ValidateTags(tags []string) error {
	if len(tags) > MaxTags {
		return fmt.Errorf("tags: at most %d, not %d", MaxTags, len(tags))
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		if !tagPattern.MatchString(tag) {
			return fmt.Errorf("tags: %q must be lower-case letters, digits and -, starting with a letter or digit, at most 32 characters", tag)
		}
		if seen[tag] {
			return fmt.Errorf("tags: %q is listed twice", tag)
		}
		seen[tag] = true
	}
	return nil
}

// Callers are, for each workflow, the names of the workflows whose
// workflow.run steps may run it, in the order of definitions.
func Callers(definitions []*Definition) map[string][]string {
	out := map[string][]string{}
	for _, definition := range definitions {
		seen := map[string]bool{}
		for _, called := range callsOf(definition) {
			if called == definition.Name || seen[called] {
				continue
			}
			seen[called] = true
			out[called] = append(out[called], definition.Name)
		}
	}
	return out
}

// SetTags is a workflow file's text with its tags: written as one
// "tags: [a, b]" line where the file had tags, else after its title, and
// removed when there are none. Every other line, comments included, is
// kept as it was.
func SetTags(text string, tags []string) (string, error) {
	if err := ValidateTags(tags); err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", fmt.Errorf("read the file: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode || doc.Content[0].Style&yaml.FlowStyle != 0 {
		return "", fmt.Errorf("the file is not a block mapping: edit it to change its tags")
	}
	mapping := doc.Content[0]
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	line := ""
	if len(tags) > 0 {
		quoted := make([]string, len(tags))
		for i, tag := range tags {
			quoted[i] = yamlWord(tag)
		}
		line = "tags: [" + strings.Join(quoted, ", ") + "]\n"
	}
	var out []string
	if i := pairIndex(mapping, "tags"); i >= 0 {
		start, end := pairLines(mapping, i, lines)
		out = append(append(append([]string{}, lines[:start]...), nonEmpty(line)...), lines[end:]...)
	} else if line == "" {
		return text, nil
	} else if i := pairIndex(mapping, "title"); i >= 0 {
		_, end := pairLines(mapping, i, lines)
		if end > 0 && !strings.HasSuffix(lines[end-1], "\n") {
			lines[end-1] += "\n"
		}
		out = append(append(append([]string{}, lines[:end]...), line), lines[end:]...)
	} else {
		out = append(lines, line)
		if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
			out[n-1] += "\n"
		}
	}
	result := strings.Join(out, "")
	var check struct {
		Tags []string `yaml:"tags"`
	}
	if err := yaml.Unmarshal([]byte(result), &check); err != nil || !slices.Equal(check.Tags, tags) && (len(check.Tags) > 0 || len(tags) > 0) {
		return "", fmt.Errorf("the tags could not be written into this file: edit it to change them")
	}
	return result, nil
}

func nonEmpty(line string) []string {
	if line == "" {
		return nil
	}
	return []string{line}
}

// yamlWord is a tag as YAML reads it back as that string: plain unless
// plain would read as a number, a boolean or null.
func yamlWord(tag string) string {
	var value any
	if yaml.Unmarshal([]byte(tag), &value) == nil {
		if text, ok := value.(string); ok && text == tag {
			return tag
		}
	}
	return strconv.Quote(tag)
}

func pairIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// pairLines are the lines, [start, end) indexes into lines, of a top-level
// pair: from its key's line to the next key's, less the blank and comment
// lines above that key, which are the next key's.
func pairLines(mapping *yaml.Node, i int, lines []string) (start, end int) {
	start = mapping.Content[i].Line - 1
	end = len(lines)
	if i+2 < len(mapping.Content) {
		end = mapping.Content[i+2].Line - 1
	}
	for end > start+1 {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed != "" && !strings.HasPrefix(lines[end-1], "#") {
			break
		}
		end--
	}
	return start, end
}
