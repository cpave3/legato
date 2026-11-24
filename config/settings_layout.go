package config

import (
	"fmt"
	"slices"
	"strings"
)

// Where each setting lives in Settings: its section and the group within
// it, and what a number counts. Kept apart from the registry so its lines
// stay readable; init copies it onto each Setting, and fails at start if a
// setting has no place. See docs/ai/settings.md § Sections and groups.

// SettingUnit is what a whole number counts.
type SettingUnit string

const (
	UnitSeconds SettingUnit = "seconds"
	UnitMs      SettingUnit = "ms"
	UnitBytes   SettingUnit = "bytes"
)

// The sections of Settings that hold registry settings, in nav order.
// Macros, the sixteenth, is a page of its own with no settings.
const (
	SectionAppearance   = "appearance"
	SectionAttention    = "attention"
	SectionInput        = "input"
	SectionAgents       = "agents"
	SectionRepositories = "repositories"
	SectionSandbox      = "sandbox"
	SectionReeve        = "reeve"
	SectionSwarms       = "swarms"
	SectionReviewPasses = "review-passes"
	SectionLearn        = "learn"
	SectionBoard        = "board"
	SectionIntegrations = "integrations"
	SectionSecurity     = "security"
	SectionConnections  = "connections"
	SectionSystem       = "system"
)

var settingSections = []string{
	SectionAppearance, SectionAttention, SectionInput,
	SectionAgents, SectionRepositories, SectionSandbox,
	SectionReeve, SectionSwarms, SectionReviewPasses, SectionLearn,
	SectionBoard, SectionIntegrations,
	SectionSecurity, SectionConnections,
	SectionSystem,
}

// SettingSections lists the section IDs a setting may name, in nav order.
func SettingSections() []string { return slices.Clone(settingSections) }

// GroupLead is the group of a feature's own on/off switch: shown first in
// its section, above the headed groups, with no heading of its own.
const GroupLead = "On or off"

type settingPlace struct {
	section, group string
	unit           SettingUnit
}

// settingLayout places keys; a prefix ending in "." places every key under
// it that has no entry of its own.
var settingLayout = map[string]settingPlace{
	// Appearance.
	"web.tint":                {SectionAppearance, "This Legato", ""},
	"agents.sparkline_window": {SectionAppearance, "This Legato", ""},
	"theme":                   {SectionAppearance, "TUI", ""},
	"icons":                   {SectionAppearance, "TUI", ""},

	// Attention.
	"web.rail.":               {SectionAttention, "Needs-you rail", ""},
	"notifications.os.":       {SectionAttention, "Notify me", ""},
	"notifications.ntfy.":     {SectionAttention, "Notify me", ""},
	"notifications.approvals": {SectionAttention, "What notifies", ""},
	"notifications.handoff":   {SectionAttention, "What notifies", ""},

	// Input and terminal.
	"agents.escape_key":    {SectionInput, "TUI and tmux", ""},
	"editor":               {SectionInput, "TUI and tmux", ""},
	"keybindings.vim_mode": {SectionInput, "TUI and tmux", ""},
	"voice.":               {SectionInput, "Dictation", ""},

	// Agents.
	"agents.default_tool":         {SectionAgents, "Defaults", ""},
	"groups.defaults":             {SectionAgents, "Defaults", ""},
	"agents.reviews":              {SectionAgents, "What agents get", ""},
	"agents.peer_awareness":       {SectionAgents, "What agents get", ""},
	"agents.mcp.enabled":          {SectionAgents, "What agents get", ""},
	"agents.mcp.resource_domains": {SectionAgents, "What agents get", ""},
	"agents.history.keep":         {SectionAgents, "Session history", ""},
	"agents.compact.auto_percent": {SectionAgents, "Compaction", ""},
	"skills.dir":                  {SectionAgents, "Skills", ""},
	"engram.":                     {SectionAgents, "Engrams", ""},

	// Repositories.
	"repository_cache.": {SectionRepositories, "Fresh clones", ""},
	"worktrees.":        {SectionRepositories, "Worktrees", ""},

	// Sandbox.
	"agents.sandbox.enabled":        {SectionSandbox, GroupLead, ""},
	"agents.sandbox.default_mounts": {SectionSandbox, "What agents see", ""},
	"agents.sandbox.docker.":        {SectionSandbox, "Docker", ""},
	"agents.sandbox.backend":        {SectionSandbox, "Advanced", ""},
	"agents.sandbox.scratch_dir":    {SectionSandbox, "Advanced", ""},

	// The Reeve.
	"reeve.name":                 {SectionReeve, "Session", ""},
	"reeve.agent":                {SectionReeve, "Session", ""},
	"reeve.model":                {SectionReeve, "Session", ""},
	"reeve.idle_timeout":         {SectionReeve, "Session", ""},
	"reeve.launch_args":          {SectionReeve, "Session", ""},
	"reeve.levels.":              {SectionReeve, "Approvals", ""},
	"reeve.host_operations":      {SectionReeve, "Host operations", ""},
	"reeve.host_operations_auto": {SectionReeve, "Host operations", ""},
	"reeve.host_timeout":         {SectionReeve, "Host operations", ""},
	"reeve.host_output_cap":      {SectionReeve, "Host operations", UnitBytes},
	"reeve.watch.":               {SectionReeve, "Watching agents", ""},
	"reeve.voice.":               {SectionReeve, "Voice", ""},
	"reeve.peer":                 {SectionReeve, "Pairings", ""},

	// Swarms.
	"swarm.enabled":                {SectionSwarms, GroupLead, ""},
	"swarm.conductor.":             {SectionSwarms, "Conductor and workers", ""},
	"swarm.worker.":                {SectionSwarms, "Conductor and workers", ""},
	"swarm.max_concurrent_agents":  {SectionSwarms, "Limits", ""},
	"swarm.max_subtasks_per_plan":  {SectionSwarms, "Limits", ""},
	"swarm.max_steps_per_plan":     {SectionSwarms, "Limits", ""},
	"swarm.strict_scope":           {SectionSwarms, "Behaviour", ""},
	"swarm.brief_kickoff_delay_ms": {SectionSwarms, "Advanced", UnitMs},

	// Review passes.
	"review_passes.enabled":           {SectionReviewPasses, GroupLead, ""},
	"review_passes.reviewer.":         {SectionReviewPasses, "Reviewer", ""},
	"review_passes.github_credential": {SectionReviewPasses, "Reviewer", ""},
	"review_passes.review_column":     {SectionReviewPasses, "Reviewer", ""},
	"review_passes.max_passes":        {SectionReviewPasses, "Limits", ""},
	"review_passes.max_concurrent":    {SectionReviewPasses, "Limits", ""},
	"review_passes.pass_timeout":      {SectionReviewPasses, "Timing", ""},
	"review_passes.poll_interval":     {SectionReviewPasses, "Timing", ""},
	"review_passes.quiet_time":        {SectionReviewPasses, "Timing", ""},
	"review_passes.counted_bots":      {SectionReviewPasses, "Bots and checks", ""},
	"review_passes.required_checks":   {SectionReviewPasses, "Bots and checks", ""},

	// Learn.
	"learning.enabled":                 {SectionLearn, GroupLead, ""},
	"learning.transition_columns":      {SectionLearn, "Reflection", ""},
	"learning.reflection_timeout":      {SectionLearn, "Reflection", ""},
	"learning.reflection_retries":      {SectionLearn, "Reflection", ""},
	"learning.curator_adapter":         {SectionLearn, "Curator", ""},
	"learning.curator_tier":            {SectionLearn, "Curator", ""},
	"learning.curator_working_dir":     {SectionLearn, "Curator", ""},
	"learning.consolidation_automatic": {SectionLearn, "Curator", ""},
	"learning.consolidation_threshold": {SectionLearn, "Curator", ""},
	"learning.prompt_enabled":          {SectionLearn, "Prompts", ""},
	"learning.prompt_max_items":        {SectionLearn, "Prompts", ""},
	"learning.embedding.":              {SectionLearn, "Semantic search", ""},
	"learning.packet_max_items":        {SectionLearn, "Advanced", ""},
	"learning.packet_max_bytes":        {SectionLearn, "Advanced", UnitBytes},
	"learning.prompt_max_bytes":        {SectionLearn, "Advanced", UnitBytes},

	// Board and tickets.
	"default_workspace":              {SectionBoard, "Workspaces", ""},
	"default_import_workspace":       {SectionBoard, "Workspaces", ""},
	"jira.":                          {SectionBoard, "Jira", ""},
	"jira.sync_interval_seconds":     {SectionBoard, "Jira", UnitSeconds},
	"jira.attachment_max_size_bytes": {SectionBoard, "Jira", UnitBytes},
	"burnlite.":                      {SectionBoard, "Burnlite", ""},
	"burnlite.sync_interval_seconds": {SectionBoard, "Burnlite", UnitSeconds},

	// Integrations.
	"github.":                               {SectionIntegrations, "GitHub", ""},
	"github.poll_interval_seconds":          {SectionIntegrations, "GitHub", UnitSeconds},
	"github.resolved_poll_interval_seconds": {SectionIntegrations, "GitHub", UnitSeconds},
	"integrations.argus.":                   {SectionIntegrations, "Argus", ""},
	"integrations.mqtt.":                    {SectionIntegrations, "Home Assistant", ""},
	"manticore.":                            {SectionIntegrations, "Manticore", ""},
	"integrations.slack.":                   {SectionIntegrations, "Slack", ""},
	"integrations.discord.":                 {SectionIntegrations, "Discord", ""},

	// Security.
	"credential_channels.": {SectionSecurity, "Credential channels", ""},
	"ssh_agent_socket":     {SectionSecurity, "Credential channels", ""},

	// Connections.
	"web.discovery": {SectionConnections, "Network", ""},

	// System.
	"updates.mode": {SectionSystem, "Updates", ""},
}

// placeOf finds a key's place: its own entry, else the longest prefix.
func placeOf(key string) (settingPlace, bool) {
	if p, ok := settingLayout[key]; ok {
		return p, true
	}
	best, found := "", false
	for prefix := range settingLayout {
		if strings.HasSuffix(prefix, ".") && strings.HasPrefix(key, prefix) && len(prefix) > len(best) {
			best, found = prefix, true
		}
	}
	return settingLayout[best], found
}

func init() {
	for i := range registry {
		s := &registry[i]
		p, ok := placeOf(s.Key)
		if !ok {
			panic(fmt.Sprintf("config: setting %s has no place in settingLayout", s.Key))
		}
		s.Section, s.Group, s.Unit = p.section, p.group, p.unit
		s.Description = s.Help
	}
}
