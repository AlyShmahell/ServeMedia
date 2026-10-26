package fetch

import (
	"strings"

	"github.com/alyshmahell/servemedia/src/internal/db"
	"github.com/alyshmahell/servemedia/src/internal/matchmedia"
)

// Action is the next step for one MatchMedia job.
type Action int

const (
	ActionApply Action = iota
	ActionFillArt
	ActionParkManual
	ActionMarkUnmatched
	ActionMarkError
	ActionSkip
	ActionLeaveSkipped
	ActionAdoptSidecar
	ActionKeepIdentity
	ActionSkipExtras
)

// Decision is the scan state Next reads. Queue is the live match callback.
// The apply path leaves Queue false.
type Decision struct {
	Mode, Status                                                  string
	Queue, Skipped, HasMeta, Overwrite, ManualSelect              bool
	HasMatch, HasCandidates, Extras, FinishedSidecar, ChangesSkip bool
}

// Next picks one step from the scan mode, the job status, and the library row.
func Next(d Decision) Action {
	if d.Skipped {
		return ActionLeaveSkipped
	}
	if d.Queue {
		if d.Status == "matched" && d.HasMatch && !d.Overwrite && d.Mode == "changes" && d.FinishedSidecar {
			return ActionAdoptSidecar
		}
		if d.Status == "matched" && d.HasMatch {
			return ActionApply
		}
		return ActionSkip
	}
	if d.ChangesSkip {
		return ActionKeepIdentity
	}
	if d.Extras {
		return ActionSkipExtras
	}
	keep := d.HasMeta && !d.Overwrite && !d.ManualSelect
	switch d.Status {
	case "manual", "multiple":
		if d.ManualSelect || !keep {
			return ActionParkManual
		}
		return ActionSkip
	case "unmatched":
		if d.ManualSelect || !keep {
			if d.HasCandidates {
				return ActionParkManual
			}
			return ActionMarkUnmatched
		}
		return ActionSkip
	case "error":
		if keep {
			return ActionSkip
		}
		if d.HasCandidates {
			return ActionParkManual
		}
		return ActionMarkError
	case "matched":
		if d.ManualSelect {
			return ActionParkManual
		}
		if d.HasMeta && !d.Overwrite {
			return ActionFillArt
		}
		if d.HasMatch {
			return ActionApply
		}
		return ActionSkip
	default:
		return ActionSkip
	}
}

func queueDecision(opts Opts, j matchmedia.Job) Decision {
	return Decision{
		Mode:            strings.TrimSpace(opts.ScanMode),
		Status:          j.Status,
		Queue:           true,
		Overwrite:       opts.Overwrite,
		HasMatch:        j.Match != nil,
		FinishedSidecar: matchmedia.SidecarComplete(j),
	}
}

func applyDecision(opts Opts, it *db.MediaItem, j matchmedia.Job) Decision {
	hasMeta := it != nil && it.MetaID.Valid && strings.TrimSpace(it.MetaID.String) != ""
	return Decision{
		Mode:          strings.TrimSpace(opts.ScanMode),
		Status:        j.Status,
		HasMeta:       hasMeta,
		Overwrite:     opts.Overwrite,
		ManualSelect:  opts.ManualSelect,
		HasMatch:      j.Match != nil,
		HasCandidates: JobHasPickerCandidates(j),
		Extras:        extrasShapedJob(j),
		ChangesSkip:   changesSkipApply(opts, it, j),
	}
}
