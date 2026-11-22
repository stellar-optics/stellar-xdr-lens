package lens

import (
	"errors"
	"fmt"
	"strings"
)

// TypeRegistrySnapshot holds the deterministic diagnostic result produced by EvaluateTypeRegistryAccess.
type TypeRegistrySnapshot struct {
	TargetKey string
	PrimaryOp int
	SecondOpN int
	ScoreRate int
	StatusTag string
	DetailMsg string
}

type TypeRegistryAccessReport = TypeRegistrySnapshot
type TypeRegistryGuardReport = TypeRegistrySnapshot
type ThreadSafeTypeRegistryReport = TypeRegistrySnapshot
type RegistryAccessReport = TypeRegistrySnapshot

func EvaluateTypeRegistryAccess(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TypeRegistrySnapshot, error) {
	trimmed := strings.TrimSpace(targetKey)
	if trimmed == "" {
		return TypeRegistrySnapshot{}, errors.New("lens: targetKey must not be empty")
	}
	if primaryOp < 0 { primaryOp = 0 }
	if secondOpN < 0 { secondOpN = 0 }
	if limitUnit <= 0 { limitUnit = 100 }
	scoreRate := ((primaryOp + secondOpN) * 10000) / limitUnit
	if scoreRate > 10000 { scoreRate = 10000 }
	statusTag := "nominal"
	if scoreRate >= 8500 { statusTag = "critical" } else if scoreRate >= 5000 { statusTag = "elevated" }
	return TypeRegistrySnapshot{
		TargetKey: trimmed,
		PrimaryOp: primaryOp,
		SecondOpN: secondOpN,
		ScoreRate: scoreRate,
		StatusTag: statusTag,
		DetailMsg: fmt.Sprintf("%s:%s:%d", trimmed, statusTag, scoreRate),
	}, nil
}

func EvaluateTypeRegistryGuard(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TypeRegistrySnapshot, error) {
	return EvaluateTypeRegistryAccess(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateThreadSafeTypeRegistry(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TypeRegistrySnapshot, error) {
	return EvaluateTypeRegistryAccess(targetKey, primaryOp, secondOpN, limitUnit)
}

func EvaluateRegistryAccess(targetKey string, primaryOp int, secondOpN int, limitUnit int) (TypeRegistrySnapshot, error) {
	return EvaluateTypeRegistryAccess(targetKey, primaryOp, secondOpN, limitUnit)
}
