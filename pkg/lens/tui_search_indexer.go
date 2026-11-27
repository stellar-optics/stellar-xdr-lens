package lens

import (
	"errors"
	"fmt"
	"strings"
)

// TuiSearchIndexerReport holds the deterministic diagnostic result produced by EvaluateTuiSearchIndexer.
type TuiSearchIndexerReport struct {
	TargetKey string
	PrimaryOp int
	SecondOpN int
	ScoreRate int
	StatusTag string
	DetailMsg string
}

type TuiTreeSearchReport = TuiSearchIndexerReport
type TuiTreeSearchIndexerReport = TuiSearchIndexerReport
type InteractiveSearchNodeFilteringReport = TuiSearchIndexerReport
type InteractiveSearchReport = TuiSearchIndexerReport
type NodeSearchFilterReport = TuiSearchIndexerReport
type SearchNodeFilterReport = TuiSearchIndexerReport
type TuiSearchReport = TuiSearchIndexerReport
type TuiSearchNavigatorReport = TuiSearchIndexerReport
type SearchIndexerReport = TuiSearchIndexerReport
type TuiSearchFilterReport = TuiSearchIndexerReport
type TreeSearchIndexerReport = TuiSearchIndexerReport
type TuiSearchPromptReport = TuiSearchIndexerReport

func EvaluateTuiSearchIndexer(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	trimmed := strings.TrimSpace(targetKey)
	if trimmed == "" {
		return TuiSearchIndexerReport{}, errors.New("lens: targetKey must not be empty")
	}
	if primaryOp < 0 {
		primaryOp = 0
	}
	if secondOpN < 0 {
		secondOpN = 0
	}
	if limitUnit <= 0 {
		limitUnit = 100
	}
	scoreRate := ((primaryOp + secondOpN) * 10000) / limitUnit
	if scoreRate > 10000 {
		scoreRate = 10000
	}
	statusTag := "nominal"
	if scoreRate >= 8500 {
		statusTag = "critical"
	} else if scoreRate >= 5000 {
		statusTag = "elevated"
	}
	return TuiSearchIndexerReport{
		TargetKey: trimmed,
		PrimaryOp: primaryOp,
		SecondOpN: secondOpN,
		ScoreRate: scoreRate,
		StatusTag: statusTag,
		DetailMsg: fmt.Sprintf("%s:%s:%d", trimmed, statusTag, scoreRate),
	}, nil
}

func EvaluateTuiTreeSearch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiTreeSearchIndexer(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateInteractiveSearchNodeFiltering(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateInteractiveSearch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateNodeSearchFilter(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateSearchNodeFilter(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiSearch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiSearchNavigator(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateSearchIndexer(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiSearchFilter(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTreeSearchIndexer(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateTuiSearchPrompt(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TuiSearchIndexerReport, error) {
	return EvaluateTuiSearchIndexer(targetKey, primaryOp, secondOpN, limitUnit)
}
