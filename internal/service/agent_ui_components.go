package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// AgentUIFormatComponents is a card built from Legato's own components
// instead of HTML: the agent names what to show and Legato draws it.
const AgentUIFormatComponents = "components"

const (
	uiMaxComponents = 40
	uiMaxTextRunes  = 4000
	uiMaxTableRows  = 100
)

// UIComponentGuide tells an agent what a component card can hold. It is the
// reference text for whoever writes one; validateUIComponents enforces it.
const UIComponentGuide = `Each component is an object with a "type" and the fields that type uses:
- heading: text; level 1-3 (default 2)
- text: text (Markdown); tone
- stat: label, value, detail, tone (one large figure)
- stats: items of {label, value, detail, tone} (1-6 figures side by side)
- key_value: items of {label, value} (1-50 rows)
- table: columns (1-8 names); rows (up to 100, one string per column)
- list: items of {text}; ordered true for a numbered list (1-50 items)
- badges: items of {text, tone} (1-20)
- progress: fraction 0-1; label; detail
- bar_chart: label; items of {label, number} with number 0 or more (1-30 bars)
- code: text; language
- image: path (an image file on disk); alt (what it shows); caption. The person can click it to see it full size.
- image_grid: items of {path, alt, caption} (1-24 images) as thumbnails. A click opens one full size, with the others to step through.
- divider: no fields
- actions: items of {label, message} (1-4 buttons). Pressing one puts its message in the person's message box for them to send; it sends nothing by itself.
tone is one of: neutral, positive, warning, negative, info.
An image path is an absolute path to a PNG, JPEG, GIF, WebP or BMP file of up to 25 MB. Legato keeps its own copy, so the file may change afterwards.`

// UIComponent is one entry of a component card. It is a flat record: Type
// says which of the other fields apply.
type UIComponent struct {
	Type     string            `json:"type" jsonschema:"heading, text, stat, stats, key_value, table, list, badges, progress, bar_chart, code, image, image_grid, divider or actions"`
	Text     string            `json:"text,omitempty" jsonschema:"heading, text (Markdown) and code: the text to show"`
	Label    string            `json:"label,omitempty" jsonschema:"stat, progress and bar_chart: what the figure is"`
	Value    string            `json:"value,omitempty" jsonschema:"stat: the figure, with its unit"`
	Detail   string            `json:"detail,omitempty" jsonschema:"stat and progress: a short note under the figure"`
	Tone     string            `json:"tone,omitempty" jsonschema:"neutral, positive, warning, negative or info"`
	Level    int               `json:"level,omitempty" jsonschema:"heading: 1, 2 or 3"`
	Ordered  bool              `json:"ordered,omitempty" jsonschema:"list: number the items"`
	Language string            `json:"language,omitempty" jsonschema:"code: the language, for example go"`
	Path     string            `json:"path,omitempty" jsonschema:"image: the image file on disk, as an absolute path"`
	Alt      string            `json:"alt,omitempty" jsonschema:"image: what the image shows, for someone who cannot see it"`
	Caption  string            `json:"caption,omitempty" jsonschema:"image: a short line under the image"`
	Fraction *float64          `json:"fraction,omitempty" jsonschema:"progress: how much is done, from 0 to 1"`
	Columns  []string          `json:"columns,omitempty" jsonschema:"table: the column names"`
	Rows     [][]string        `json:"rows,omitempty" jsonschema:"table: one array of cell texts per row, in column order"`
	Items    []UIComponentItem `json:"items,omitempty" jsonschema:"stats, key_value, list, badges, bar_chart, image_grid and actions: the entries"`
}

// UIComponentItem is one entry of a component that holds several.
type UIComponentItem struct {
	Label   string   `json:"label,omitempty" jsonschema:"stats, key_value, bar_chart and actions: the entry's name"`
	Value   string   `json:"value,omitempty" jsonschema:"stats and key_value: the entry's value"`
	Detail  string   `json:"detail,omitempty" jsonschema:"stats: a short note under the figure"`
	Tone    string   `json:"tone,omitempty" jsonschema:"stats and badges: neutral, positive, warning, negative or info"`
	Text    string   `json:"text,omitempty" jsonschema:"list and badges: the entry's text"`
	Number  *float64 `json:"number,omitempty" jsonschema:"bar_chart: the bar's value, 0 or more"`
	Message string   `json:"message,omitempty" jsonschema:"actions: the text put in the person's message box when they press the button"`
	Path    string   `json:"path,omitempty" jsonschema:"image_grid: the image file on disk, as an absolute path"`
	Alt     string   `json:"alt,omitempty" jsonschema:"image_grid: what the image shows, for someone who cannot see it"`
	Caption string   `json:"caption,omitempty" jsonschema:"image_grid: a short line under the image"`
}

var uiTones = map[string]bool{"": true, "neutral": true, "positive": true, "warning": true, "negative": true, "info": true}

// uiItemRule says what one kind of component asks of its items.
type uiItemRule struct {
	max      int
	required []string // of: label, value, text, number, message, path
}

var uiItemRules = map[string]uiItemRule{
	"stats":      {max: 6, required: []string{"value"}},
	"key_value":  {max: 50, required: []string{"label", "value"}},
	"list":       {max: 50, required: []string{"text"}},
	"badges":     {max: 20, required: []string{"text"}},
	"bar_chart":  {max: 30, required: []string{"label", "number"}},
	"actions":    {max: 4, required: []string{"label", "message"}},
	"image_grid": {max: 24, required: []string{"path"}},
}

// encodeUIComponents checks a component card and returns it as the JSON that
// is stored and sent to the client that draws it.
func encodeUIComponents(components []UIComponent) (string, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentUIInvalid, fmt.Sprintf(format, args...))
	}
	if len(components) == 0 {
		return "", invalid("a component card needs at least one component")
	}
	if len(components) > uiMaxComponents {
		return "", invalid("at most %d components fit in one card", uiMaxComponents)
	}
	for i, component := range components {
		known, err := validateUIComponent(component)
		if err == nil {
			continue
		}
		if known {
			return "", invalid("components[%d] (%s): %s", i, component.Type, err)
		}
		return "", invalid("components[%d]: %s", i, err)
	}
	encoded, err := json.Marshal(components)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// validateUIComponent reports what stops one component from being drawn, and
// whether its type was one of the catalog's.
func validateUIComponent(c UIComponent) (known bool, err error) {
	switch c.Type {
	case "heading", "text", "stat", "stats", "key_value", "table", "list", "badges", "progress", "bar_chart", "code", "image", "image_grid", "divider", "actions":
	default:
		return false, fmt.Errorf("unknown type %q", c.Type)
	}
	for _, field := range []struct{ name, value string }{
		{"text", c.Text}, {"label", c.Label}, {"value", c.Value}, {"detail", c.Detail}, {"language", c.Language},
		{"path", c.Path}, {"alt", c.Alt}, {"caption", c.Caption},
	} {
		if utf8.RuneCountInString(field.value) > uiMaxTextRunes {
			return true, fmt.Errorf("%s is longer than %d characters", field.name, uiMaxTextRunes)
		}
	}
	if !uiTones[c.Tone] {
		return true, fmt.Errorf("unknown tone %q", c.Tone)
	}

	switch c.Type {
	case "heading":
		if c.Text == "" {
			return true, fmt.Errorf("text is required")
		}
		if c.Level < 0 || c.Level > 3 {
			return true, fmt.Errorf("level must be 1, 2 or 3")
		}
	case "text", "code":
		if c.Text == "" {
			return true, fmt.Errorf("text is required")
		}
	case "stat":
		if c.Value == "" {
			return true, fmt.Errorf("value is required")
		}
	case "image":
		if strings.TrimSpace(c.Path) == "" {
			return true, fmt.Errorf("path is required")
		}
	case "progress":
		if c.Fraction == nil {
			return true, fmt.Errorf("fraction is required")
		}
		if *c.Fraction < 0 || *c.Fraction > 1 {
			return true, fmt.Errorf("fraction must be between 0 and 1")
		}
	case "table":
		if len(c.Columns) == 0 {
			return true, fmt.Errorf("columns is required")
		}
		if len(c.Columns) > 8 {
			return true, fmt.Errorf("at most 8 columns")
		}
		if len(c.Rows) > uiMaxTableRows {
			return true, fmt.Errorf("at most %d rows", uiMaxTableRows)
		}
		for i, row := range c.Rows {
			if len(row) != len(c.Columns) {
				return true, fmt.Errorf("rows[%d] has %d cells; the table has %d columns", i, len(row), len(c.Columns))
			}
		}
	}

	rule, hasItems := uiItemRules[c.Type]
	if !hasItems {
		return true, nil
	}
	if len(c.Items) == 0 {
		return true, fmt.Errorf("items is required")
	}
	if len(c.Items) > rule.max {
		return true, fmt.Errorf("at most %d items", rule.max)
	}
	for i, item := range c.Items {
		if err := validateUIComponentItem(item, rule); err != nil {
			return true, fmt.Errorf("items[%d]: %s", i, err)
		}
	}
	return true, nil
}

func validateUIComponentItem(item UIComponentItem, rule uiItemRule) error {
	values := map[string]string{
		"label": item.Label, "value": item.Value, "text": item.Text, "message": item.Message, "detail": item.Detail,
		"path": strings.TrimSpace(item.Path), "alt": item.Alt, "caption": item.Caption,
	}
	for _, name := range rule.required {
		if name == "number" {
			if item.Number == nil {
				return fmt.Errorf("number is required")
			}
			if *item.Number < 0 {
				return fmt.Errorf("number must not be negative")
			}
			continue
		}
		if values[name] == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	for _, name := range []string{"label", "value", "text", "message", "detail", "path", "alt", "caption"} {
		if utf8.RuneCountInString(values[name]) > uiMaxTextRunes {
			return fmt.Errorf("%s is longer than %d characters", name, uiMaxTextRunes)
		}
	}
	if !uiTones[item.Tone] {
		return fmt.Errorf("unknown tone %q", item.Tone)
	}
	return nil
}

// uiComponentImages lists the image files a component card uses, each once
// and in the order the card shows them. A component names its image by the
// path the agent gave, which is also how the client finds the stored copy.
func uiComponentImages(components []UIComponent) []AgentUIImageInput {
	var images []AgentUIImageInput
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		images = append(images, AgentUIImageInput{Name: path, Path: path})
	}
	for _, component := range components {
		switch component.Type {
		case "image":
			add(component.Path)
		case "image_grid":
			for _, item := range component.Items {
				add(item.Path)
			}
		}
	}
	return images
}
