package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func number(value float64) *float64 { return &value }

func TestAgentUIShowStoresAComponentCardAsJSON(t *testing.T) {
	ui, _ := newAgentUIService(t)
	ctx := context.Background()

	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatComponents,
		Title: "Weather in Brisbane", FallbackText: "27 C and sunny in Brisbane",
		Components: []service.UIComponent{
			{Type: "heading", Text: "Weather in Brisbane"},
			{Type: "stats", Items: []service.UIComponentItem{
				{Label: "Temperature", Value: "27 C", Tone: "positive"},
				{Label: "Wind", Value: "12 km/h", Detail: "from the east"},
			}},
			{Type: "bar_chart", Label: "Rain this week", Items: []service.UIComponentItem{
				{Label: "Mon", Number: number(0)}, {Label: "Tue", Number: number(4.5)},
			}},
			{Type: "actions", Items: []service.UIComponentItem{{Label: "Forecast", Message: "Show me the 7 day forecast"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != service.AgentUIFormatComponents {
		t.Fatalf("format = %q", got.Format)
	}
	var stored []service.UIComponent
	if err := json.Unmarshal([]byte(got.Content), &stored); err != nil {
		t.Fatalf("content is not a component list: %v\n%s", err, got.Content)
	}
	if len(stored) != 4 || stored[0].Text != "Weather in Brisbane" || stored[1].Items[0].Tone != "positive" {
		t.Fatalf("stored components = %#v", stored)
	}
	if zero := stored[2].Items[0].Number; zero == nil || *zero != 0 {
		t.Fatalf("a bar of zero was lost: %#v", stored[2].Items[0])
	}
	if stored[3].Items[0].Message != "Show me the 7 day forecast" {
		t.Fatalf("action = %#v", stored[3].Items[0])
	}
}

func TestAgentUIShowRejectsComponentCardsThatCannotBeDrawn(t *testing.T) {
	cases := map[string]struct {
		components []service.UIComponent
		wantInErr  string
	}{
		"no components":   {nil, "at least one component"},
		"unknown type":    {[]service.UIComponent{{Type: "carousel"}}, `components[0]: unknown type "carousel"`},
		"heading no text": {[]service.UIComponent{{Type: "heading"}}, "components[0] (heading): text is required"},
		"heading level":   {[]service.UIComponent{{Type: "heading", Text: "x", Level: 7}}, "components[0] (heading): level must be 1, 2 or 3"},
		"unknown tone":    {[]service.UIComponent{{Type: "text", Text: "x", Tone: "angry"}}, `components[0] (text): unknown tone "angry"`},
		"stat no value":   {[]service.UIComponent{{Type: "stat", Label: "Temp"}}, "components[0] (stat): value is required"},
		"stats empty":     {[]service.UIComponent{{Type: "stats"}}, "components[0] (stats): items is required"},
		"stats item":      {[]service.UIComponent{{Type: "text", Text: "ok"}, {Type: "stats", Items: []service.UIComponentItem{{Label: "a", Value: "1"}, {Label: "b"}}}}, "components[1] (stats): items[1]: value is required"},
		"key value label": {[]service.UIComponent{{Type: "key_value", Items: []service.UIComponentItem{{Value: "1"}}}}, "components[0] (key_value): items[0]: label is required"},
		"table columns":   {[]service.UIComponent{{Type: "table", Rows: [][]string{{"a"}}}}, "components[0] (table): columns is required"},
		"table row width": {[]service.UIComponent{{Type: "table", Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}, {"3"}}}}, "components[0] (table): rows[1] has 1 cells; the table has 2 columns"},
		"list item text":  {[]service.UIComponent{{Type: "list", Items: []service.UIComponentItem{{Label: "x"}}}}, "components[0] (list): items[0]: text is required"},
		"badge text":      {[]service.UIComponent{{Type: "badges", Items: []service.UIComponentItem{{Tone: "info"}}}}, "components[0] (badges): items[0]: text is required"},
		"progress absent": {[]service.UIComponent{{Type: "progress", Label: "Build"}}, "components[0] (progress): fraction is required"},
		"progress range":  {[]service.UIComponent{{Type: "progress", Fraction: number(1.5)}}, "components[0] (progress): fraction must be between 0 and 1"},
		"bar no number":   {[]service.UIComponent{{Type: "bar_chart", Items: []service.UIComponentItem{{Label: "Mon"}}}}, "components[0] (bar_chart): items[0]: number is required"},
		"bar negative":    {[]service.UIComponent{{Type: "bar_chart", Items: []service.UIComponentItem{{Label: "Mon", Number: number(-1)}}}}, "components[0] (bar_chart): items[0]: number must not be negative"},
		"code no text":    {[]service.UIComponent{{Type: "code", Language: "go"}}, "components[0] (code): text is required"},
		"action message":  {[]service.UIComponent{{Type: "actions", Items: []service.UIComponentItem{{Label: "Run"}}}}, "components[0] (actions): items[0]: message is required"},
		"too many actions": {[]service.UIComponent{{Type: "actions", Items: []service.UIComponentItem{
			{Label: "1", Message: "m"}, {Label: "2", Message: "m"}, {Label: "3", Message: "m"}, {Label: "4", Message: "m"}, {Label: "5", Message: "m"},
		}}}, "components[0] (actions): at most 4 items"},
		"text too long":       {[]service.UIComponent{{Type: "text", Text: strings.Repeat("x", 4001)}}, "components[0] (text): text is longer than 4000 characters"},
		"too many components": {make([]service.UIComponent, 41), "at most 40 components"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ui, _ := newAgentUIService(t)
			_, err := ui.Show(context.Background(), service.AgentUIShowInput{
				TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatComponents,
				FallbackText: "fallback", Components: tc.components,
			})
			if !errors.Is(err, service.ErrAgentUIInvalid) {
				t.Fatalf("error = %v, want ErrAgentUIInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantInErr)
			}
		})
	}
}
