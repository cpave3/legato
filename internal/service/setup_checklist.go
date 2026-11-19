package service

import (
	"context"
	"log"
)

// SetupItem is one thing on the web UI's setup checklist.
type SetupItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
	// Optional items are listed but not counted.
	Optional bool `json:"optional,omitempty"`
	// Settings is the part of the web UI's Settings that does it.
	Settings string `json:"settings,omitempty"`
}

// Feature states for More setup.
const (
	FeatureOn    = "on"
	FeatureOff   = "off"
	FeatureNeeds = "needs"
)

// SetupFeature is a "More setup" card: an optional feature, whether it is
// on, off, or on but missing something (Detail says what), where it is
// set up in the web UI, and its page on the user site.
type SetupFeature struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	// Link is a path in the web UI, such as /settings#credentials.
	Link string `json:"link"`
	Docs string `json:"docs"`
}

// SetupStatus is the setup checklist: its items, how many of the counted
// ones are done, whether it was dismissed, and whether the web UI shows its
// chip (not all done and not dismissed, or a required check failing).
type SetupStatus struct {
	Items     []SetupItem `json:"items"`
	Done      int         `json:"done"`
	Total     int         `json:"total"`
	Dismissed bool        `json:"dismissed"`
	Show      bool        `json:"show"`
	// More are the optional features, not counted.
	More []SetupFeature `json:"more"`
}

// InstanceValues keeps values of this Legato in its database.
type InstanceValues interface {
	GetInstanceValue(ctx context.Context, key string) (string, error)
	SetInstanceValue(ctx context.Context, key, value string) error
}

// SetupChecklistOptions are where the checklist reads each item.
type SetupChecklistOptions struct {
	// Checks is the doctor's last report, if any (DoctorReports.Known).
	Checks func() (DoctorReport, bool)
	// DefaultTool is the default agent tool the operator chose, "" when
	// none was.
	DefaultTool func(ctx context.Context) string
	// Repositories counts the repository presets.
	Repositories func(ctx context.Context) (int, error)
	// Devices counts the devices enrolled to sign approvals.
	Devices func(ctx context.Context) (int, error)
	// Tracker says a ticket tracker is connected.
	Tracker func(ctx context.Context) bool
	// Features are the More setup cards; nil lists none.
	Features func(ctx context.Context) []SetupFeature
	Values   InstanceValues
}

// SetupChecklist is what is left to set up, for the web UI's "Setup n/N"
// chip. Its dismissal is kept per Legato, so every device agrees.
type SetupChecklist struct{ opts SetupChecklistOptions }

const (
	setupDismissedKey   = "setup.dismissed"
	setupPairedPhoneKey = "setup.paired_phone"
)

// NewSetupChecklist reads the checklist through opts.
func NewSetupChecklist(opts SetupChecklistOptions) *SetupChecklist {
	return &SetupChecklist{opts: opts}
}

// Status is the checklist now.
func (c *SetupChecklist) Status(ctx context.Context) SetupStatus {
	report, known := c.opts.Checks()
	checksOK := !known || report.OK()
	repos, err := c.opts.Repositories(ctx)
	if err != nil {
		log.Printf("setup checklist: %v", err)
	}
	devices, err := c.opts.Devices(ctx)
	if err != nil {
		log.Printf("setup checklist: %v", err)
	}
	paired, _ := c.opts.Values.GetInstanceValue(ctx, setupPairedPhoneKey)
	st := SetupStatus{Items: []SetupItem{
		{ID: "checks", Title: "This machine has what Legato needs", Done: known && report.OK(), Settings: "server"},
		{ID: "default_tool", Title: "Choose the default agent tool", Done: c.opts.DefaultTool(ctx) != "", Settings: "setting-agents.default_tool"},
		{ID: "repository", Title: "Add a repository", Done: repos > 0, Settings: "repositories"},
		{ID: "device", Title: "Pair a phone or another device", Done: devices > 0 || paired != "", Settings: "pair-phone"},
		{ID: "tracker", Title: "Connect a ticket tracker", Done: c.opts.Tracker(ctx), Optional: true, Settings: "integrations"},
	}}
	for _, item := range st.Items {
		if item.Optional {
			continue
		}
		st.Total++
		if item.Done {
			st.Done++
		}
	}
	dismissed, _ := c.opts.Values.GetInstanceValue(ctx, setupDismissedKey)
	st.Dismissed = dismissed == "true"
	st.Show = (st.Done < st.Total && !st.Dismissed) || !checksOK
	st.More = []SetupFeature{}
	if c.opts.Features != nil {
		st.More = c.opts.Features(ctx)
	}
	return st
}

// SetDismissed hides the chip, or brings it back, on every device.
func (c *SetupChecklist) SetDismissed(ctx context.Context, dismissed bool) error {
	value := ""
	if dismissed {
		value = "true"
	}
	return c.opts.Values.SetInstanceValue(ctx, setupDismissedKey, value)
}

// RecordPairing remembers that a phone paired by phrase, which keeps no
// record of its own: PairingService.OnChange calls it.
func (c *SetupChecklist) RecordPairing(ctx context.Context, st PairingStatus) {
	if st.State != PairingPaired || st.Purpose != PairingPhone {
		return
	}
	name := st.Device
	if name == "" {
		name = "a phone"
	}
	if err := c.opts.Values.SetInstanceValue(ctx, setupPairedPhoneKey, name); err != nil {
		log.Printf("setup checklist: %v", err)
	}
}
