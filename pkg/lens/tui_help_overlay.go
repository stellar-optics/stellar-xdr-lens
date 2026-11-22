package lens

import (
	"errors"
	"fmt"
	"strings"
)

// TuiHelpOverlayReport holds the deterministic diagnostic result produced by EvaluateTuiHelpOverlay.
type TuiHelpOverlayReport struct {
	TargetKey string
	PrimaryOp int
	SecondOpN int
	ScoreRate int
	StatusTag string
	DetailMsg string
}

type KeybindingHelpOverlayReport = TuiHelpOverlayReport
type HelpOverlayInspectorReport = TuiHelpOverlayReport
type TuiInspectorSidebarReport = TuiHelpOverlayReport
type InspectorSidebarReport = TuiHelpOverlayReport
type KeybindingsHelpReport = TuiHelpOverlayReport
type TuiHelpInspectorReport = TuiHelpOverlayReport
type HelpOverlayReport = TuiHelpOverlayReport
type TuiHelpModalReport = TuiHelpOverlayReport

func EvaluateTuiHelpOverlay(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	trimmed := strings.TrimSpace(targetKey)
	if trimmed == "" {
		return TuiHelpOverlayReport{}, errors.New("lens: targetKey must not be empty")
	}
	if primaryOp < 0 { primaryOp = 0 }
	if secondOpN < 0 { secondOpN = 0 }
	if limitUnit <= 0 { limitUnit = 100 }
	scoreRate := ((primaryOp + secondOpN) * 10000) / limitUnit
	if scoreRate > 10000 { scoreRate = 10000 }
	statusTag := "nominal"
	if scoreRate >= 8500 { statusTag = "critical" } else if scoreRate >= 5000 { statusTag = "elevated" }
	return TuiHelpOverlayReport{
		TargetKey: trimmed,
		PrimaryOp: primaryOp,
		SecondOpN: secondOpN,
		ScoreRate: scoreRate,
		StatusTag: statusTag,
		DetailMsg: fmt.Sprintf("%s:%s:%d", trimmed, statusTag, scoreRate),
	}, nil
}

func EvaluateKeybindingHelpOverlay(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateHelpOverlayInspector(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiInspectorSidebar(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateInspectorSidebar(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateKeybindingsHelp(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiHelpInspector(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateHelpOverlay(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiHelpModal(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiHelpOverlayReport, error) {
	return EvaluateTuiHelpOverlay(targetKey, primaryOp, secondOpN, limitUnit)
}
