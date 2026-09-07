package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/cpave3/legato/internal/service"
)

func ReviewHandoffSubmit(svc *service.ReviewService, tourID string, input io.Reader) (*service.HandoffPacket, error) {
	packet, err := service.DecodeHandoff(input)
	if err != nil {
		return nil, err
	}
	saved, err := svc.SubmitHandoff(context.Background(), tourID, packet)
	if err != nil {
		return nil, err
	}
	broadcastReviewChanged(tourID, "", "handoff_submitted")
	return saved, nil
}

func ReviewHandoffShow(svc *service.ReviewService, tourID string, asJSON bool, w io.Writer) error {
	view, err := svc.Handoff(context.Background(), tourID)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(w, view)
	}
	if len(view.Packets) == 0 {
		_, err = fmt.Fprintln(w, "No handoff packet. Prepare a clean review with review ready, then review handoff submit --file packet.json. Use review handoff template for the authoring schema.")
		return err
	}
	if _, err = fmt.Fprintf(w, "Handoff for %s — last checked: %s\n", view.TourID, view.CheckedAt); err != nil {
		return err
	}
	for _, p := range view.Packets {
		state := "snapshot (not continuously checked)"
		if p.Outdated {
			state = "OUTDATED: " + p.OutdatedReason
		}
		if _, err = fmt.Fprintf(w, "\n%s revision %d — %s\nPass: %s\nBase: %s\nHead: %s\nBefore: %s\nAfter: %s\nWhy: %s\nRisk: %s\nVerification gaps: %s\n", p.ID, p.Revision, state, p.Input.PassID, p.Input.BaseSHA, p.Input.HeadSHA, p.Input.Overview.Before, p.Input.Overview.After, p.Input.Overview.Why, p.Input.Overview.Risk, p.Input.Overview.VerificationGaps); err != nil {
			return err
		}
		for _, c := range p.Input.Concepts {
			if _, err = fmt.Fprintf(w, "  %s: %s\n    %s\n", c.ID, c.Title, c.Claim); err != nil {
				return err
			}
			if len(c.EvidenceIDs) == 0 {
				if _, err = fmt.Fprintln(w, "    No evidence attached"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ReviewHandoffExport(svc *service.ReviewService, tourID string, input service.HandoffExportInput, asJSON bool, w io.Writer) error {
	result, err := svc.ExportHandoff(context.Background(), tourID, input)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(w, result)
	}
	_, err = io.WriteString(w, result.Markdown)
	return err
}
