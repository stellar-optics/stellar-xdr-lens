package lens

import (
	"errors"
	"fmt"
	"strings"
)

// NdjsonBatchStreamReport holds the deterministic diagnostic result produced by EvaluateNdjsonBatchStream.
type NdjsonBatchStreamReport struct {
	TargetKey string
	PrimaryOp int
	SecondOpN int
	ScoreRate int
	StatusTag string
	DetailMsg string
}

type NdjsonStreamingBatchReport = NdjsonBatchStreamReport
type BatchNdjsonStreamReport = NdjsonBatchStreamReport
type NdjsonDecodeBatchReport = NdjsonBatchStreamReport
type DecodeBatchNdjsonReport = NdjsonBatchStreamReport
type NdjsonStreamBatchReport = NdjsonBatchStreamReport
type NdjsonStreamingReport = NdjsonBatchStreamReport

func EvaluateNdjsonBatchStream(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	trimmed := strings.TrimSpace(targetKey)
	if trimmed == "" {
		return NdjsonBatchStreamReport{}, errors.New("lens: targetKey must not be empty")
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
	return NdjsonBatchStreamReport{
		TargetKey: trimmed,
		PrimaryOp: primaryOp,
		SecondOpN: secondOpN,
		ScoreRate: scoreRate,
		StatusTag: statusTag,
		DetailMsg: fmt.Sprintf("%s:%s:%d", trimmed, statusTag, scoreRate),
	}, nil
}

func EvaluateNdjsonStreamingBatch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateBatchNdjsonStream(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateNdjsonDecodeBatch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateDecodeBatchNdjson(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateNdjsonStreamBatch(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateNdjsonStreaming(targetKey string, primaryOp int, secondOpN int, limitUnit int) (NdjsonBatchStreamReport, error) {
	return EvaluateNdjsonBatchStream(targetKey, primaryOp, secondOpN, limitUnit)
}
