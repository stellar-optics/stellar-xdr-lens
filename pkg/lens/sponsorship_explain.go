package lens

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// SponsorshipStep records one sponsorship-related transaction operation.
type SponsorshipStep struct {
	Index            int    `json:"index"`
	Type             string `json:"type"`
	Source           string `json:"source,omitempty"`
	SponsoredAccount string `json:"sponsoredAccount,omitempty"`
	ResultCode       string `json:"resultCode,omitempty"`
}

// SponsorshipDiagnosis explains a sponsorship operation failure or invalid
// begin/end sequence in a transaction.
type SponsorshipDiagnosis struct {
	TransactionCode string            `json:"transactionCode,omitempty"`
	OperationIndex  *int              `json:"operationIndex,omitempty"`
	OperationCode   string            `json:"operationCode,omitempty"`
	Summary         string            `json:"summary"`
	Hint            string            `json:"hint,omitempty"`
	Conflicts       []string          `json:"conflicts,omitempty"`
	Operations      []SponsorshipStep `json:"operations,omitempty"`
}

// DiagnoseSponsorship explains sponsorship result codes and checks the
// transaction's begin/end sponsorship instructions for obvious conflicts.
// It returns (nil, nil) when neither the result nor operation sequence points
// to a sponsorship failure. All inputs are inspected locally; no ledger lookup
// is performed.
func DiagnoseSponsorship(envelope *xdr.TransactionEnvelope, result *xdr.TransactionResult) (*SponsorshipDiagnosis, error) {
	if envelope == nil || result == nil {
		return nil, fmt.Errorf("%w: envelope and result are required for sponsorship diagnosis", ErrInvalidArgument)
	}
	transactionConstant, err := resultCodeName(result)
	if err != nil {
		return nil, err
	}
	if inner, ok := innerResultCodeName(result); ok {
		transactionConstant = inner
	}
	operationIndex := -1
	var operationConstant string
	operationResults, _ := result.OperationResults()
	for index, operationResult := range operationResults {
		reason := explainOperationResult(operationResult)
		if !strings.Contains(reason.Constant, "SponsoringFutureReserves") && !strings.Contains(reason.Constant, "RevokeSponsorship") {
			continue
		}
		if reason.Success {
			continue
		}
		operationIndex = index
		operationConstant = reason.Constant
		break
	}
	if transactionConstant != "TransactionResultCodeTxBadSponsorship" && operationIndex < 0 {
		return nil, nil
	}

	d := &SponsorshipDiagnosis{}
	if transactionConstant == "TransactionResultCodeTxBadSponsorship" {
		reason := ExplainCode(transactionConstant)
		d.TransactionCode = reason.Code
		d.Summary = reason.Summary
		d.Hint = reason.Hint
	} else {
		d.OperationIndex = &operationIndex
		d.OperationCode = ExplainCode(operationConstant).Code
		reason := ExplainCode(operationConstant)
		d.Summary = fmt.Sprintf("Sponsorship operation %d failed: %s", operationIndex, reason.Summary)
		d.Hint = reason.Hint
	}

	d.Operations, d.Conflicts = inspectSponsorshipSequence(envelope, operationResults)
	if len(d.Conflicts) > 0 {
		d.Summary += " Transaction sequence issue: " + strings.Join(d.Conflicts, "; ") + "."
		if d.Hint == "" {
			d.Hint = "Pair every begin-sponsoring instruction with the correct end-sponsoring instruction in the same transaction."
		}
	}
	return d, nil
}

func inspectSponsorshipSequence(envelope *xdr.TransactionEnvelope, operationResults []xdr.OperationResult) ([]SponsorshipStep, []string) {
	var steps []SponsorshipStep
	var conflicts []string
	pendingBegin := -1
	for index, operation := range envelope.Operations() {
		resultCode := ""
		if index < len(operationResults) {
			resultCode = explainOperationResult(operationResults[index]).Code
		}
		switch operation.Body.Type {
		case xdr.OperationTypeBeginSponsoringFutureReserves:
			step := SponsorshipStep{Index: index, Type: "begin_sponsoring_future_reserves", Source: operationSource(operation), ResultCode: resultCode}
			if begin := operation.Body.BeginSponsoringFutureReservesOp; begin != nil {
				if begin.SponsoredId.Type == xdr.PublicKeyTypePublicKeyTypeEd25519 && begin.SponsoredId.Ed25519 != nil {
					if address, err := begin.SponsoredId.GetAddress(); err == nil {
						step.SponsoredAccount = address
					}
				}
			}
			steps = append(steps, step)
			if pendingBegin >= 0 {
				conflicts = append(conflicts, fmt.Sprintf("begin-sponsoring at operation %d occurs before operation %d was ended", index, pendingBegin))
			} else {
				pendingBegin = index
			}
		case xdr.OperationTypeEndSponsoringFutureReserves:
			steps = append(steps, SponsorshipStep{Index: index, Type: "end_sponsoring_future_reserves", Source: operationSource(operation), ResultCode: resultCode})
			if pendingBegin < 0 {
				conflicts = append(conflicts, fmt.Sprintf("end-sponsoring at operation %d has no preceding begin-sponsoring", index))
			} else {
				pendingBegin = -1
			}
		case xdr.OperationTypeRevokeSponsorship:
			steps = append(steps, SponsorshipStep{Index: index, Type: "revoke_sponsorship", Source: operationSource(operation), ResultCode: resultCode})
		}
	}
	if pendingBegin >= 0 {
		conflicts = append(conflicts, fmt.Sprintf("begin-sponsoring at operation %d has no matching end-sponsoring", pendingBegin))
	}
	return steps, conflicts
}

// DiagnoseRevokeSponsorship explains a failed revoke_sponsorship operation,
// distinguishing a revocation that targets a ledger entry from one that targets
// a signer. It returns an empty string for a nil operation or a successful
// code. The current sponsor is recorded on the ledger rather than in the
// operation, so the diagnosis states who is allowed to revoke instead of naming
// that account.
func DiagnoseRevokeSponsorship(op *xdr.RevokeSponsorshipOp, code xdr.RevokeSponsorshipResultCode) string {
	if op == nil {
		return ""
	}
	r := ExplainCode(code.String())
	if r.Success {
		return ""
	}
	return reasonDiagnosisText(deepenRevokeSponsorshipFailure("", *op, r))
}

// revokeSponsorshipTarget describes what a revoke_sponsorship operation points
// at, as far as the operation itself can say.
type revokeSponsorshipTarget struct {
	// label names the target in prose, starting with "ledger entry" or "signer".
	label string
	// owner is the account the entry or signer belongs to. A claimable balance
	// has no owning account, so it is empty there.
	owner string
	// baseReserves is the number of base reserves the target locks. It is 0 for
	// a claimable balance, whose reserve depends on its claimant count.
	baseReserves int64
	// claimableBalance reports whether the target is a claimable balance.
	claimableBalance bool
	// malformed names the structural problem stellar-core rejects the target
	// for, when the operation alone proves one.
	malformed string
}

// newRevokeSponsorshipTarget resolves the operation's union arm into a target.
// It tolerates nil arms so that a hand-built operation cannot panic the tool.
func newRevokeSponsorshipTarget(o xdr.RevokeSponsorshipOp) revokeSponsorshipTarget {
	if o.Type == xdr.RevokeSponsorshipTypeRevokeSponsorshipSigner {
		t := revokeSponsorshipTarget{label: "signer", baseReserves: 1}
		if o.Signer != nil {
			t.owner = revokeSponsorshipAccount(o.Signer.AccountId)
			t.label = fmt.Sprintf("%s on account %s", signerKeyLabel(o.Signer.SignerKey), shortAddr(t.owner))
		}
		return t
	}
	if o.LedgerKey == nil {
		return revokeSponsorshipTarget{label: "ledger entry", baseReserves: 1}
	}
	return newRevokeSponsorshipEntryTarget(*o.LedgerKey)
}

// newRevokeSponsorshipEntryTarget mirrors the ledger key checks in
// stellar-core's RevokeSponsorshipOpFrame::doCheckValid and the reserve each
// entry type locks.
func newRevokeSponsorshipEntryTarget(key xdr.LedgerKey) revokeSponsorshipTarget {
	t := revokeSponsorshipTarget{label: "ledger entry", baseReserves: 1}
	switch {
	case key.Type == xdr.LedgerEntryTypeAccount && key.Account != nil:
		t.owner = revokeSponsorshipAccount(key.Account.AccountId)
		t.label = "ledger entry for account " + shortAddr(t.owner)
		t.baseReserves = 2
	case key.Type == xdr.LedgerEntryTypeTrustline && key.TrustLine != nil:
		t.owner = revokeSponsorshipAccount(key.TrustLine.AccountId)
		asset := "liquidity pool shares"
		switch key.TrustLine.Asset.Type {
		case xdr.AssetTypeAssetTypePoolShare:
			t.baseReserves = 2
		case xdr.AssetTypeAssetTypeNative:
			asset = "XLM"
			t.malformed = "the native asset has no trustline"
		default:
			line := key.TrustLine.Asset.ToAsset()
			asset = assetLabel(line)
			if line.GetIssuer() == t.owner {
				t.malformed = "an issuer holds no trustline to its own asset"
			}
		}
		t.label = fmt.Sprintf("ledger entry for the %s trustline of %s", asset, shortAddr(t.owner))
	case key.Type == xdr.LedgerEntryTypeOffer && key.Offer != nil:
		t.owner = revokeSponsorshipAccount(key.Offer.SellerId)
		t.label = fmt.Sprintf("ledger entry for offer %d of %s", key.Offer.OfferId, shortAddr(t.owner))
		if key.Offer.OfferId <= 0 {
			t.malformed = "an offer ID must be positive"
		}
	case key.Type == xdr.LedgerEntryTypeData && key.Data != nil:
		t.owner = revokeSponsorshipAccount(key.Data.AccountId)
		t.label = fmt.Sprintf("ledger entry for data %q of %s", string(key.Data.DataName), shortAddr(t.owner))
		if len(key.Data.DataName) == 0 {
			t.malformed = "a data entry name cannot be empty"
		}
	case key.Type == xdr.LedgerEntryTypeClaimableBalance:
		t.label = "ledger entry for a claimable balance"
		t.baseReserves = 0
		t.claimableBalance = true
		if key.ClaimableBalance != nil && key.ClaimableBalance.BalanceId.V0 != nil {
			id := *key.ClaimableBalance.BalanceId.V0
			t.label = fmt.Sprintf("ledger entry for claimable balance %s…", hex.EncodeToString(id[:])[:8])
		}
	case key.Type == xdr.LedgerEntryTypeLiquidityPool,
		key.Type == xdr.LedgerEntryTypeContractData,
		key.Type == xdr.LedgerEntryTypeContractCode,
		key.Type == xdr.LedgerEntryTypeConfigSetting,
		key.Type == xdr.LedgerEntryTypeTtl:
		t.malformed = "only accounts, trustlines, offers, data entries and claimable balances can be sponsored"
	}
	return t
}

// revokeSponsorshipAccount renders an account ID, tolerating a malformed value.
func revokeSponsorshipAccount(id xdr.AccountId) string {
	addr, err := id.GetAddress()
	if err != nil {
		return ""
	}
	return addr
}

// reserveTransfer states how much reserve moves when the target's sponsorship
// changes hands, in both stroops and XLM.
func (t revokeSponsorshipTarget) reserveTransfer() string {
	base := reserveAmountLabel(int64(DefaultBaseReserve))
	if t.claimableBalance {
		return base + " per claimant"
	}
	return reserveAmountLabel(t.baseReserves * int64(DefaultBaseReserve))
}

// deepenRevokeSponsorshipFailure refines a failed revoke_sponsorship
// explanation. Who may revoke and who inherits the reserve both follow from
// stellar-core's rules: a sponsored target can only be revoked by its sponsor,
// an unsponsored one only by its owner, and the reserve moves to the sponsor
// named by an enclosing begin_sponsoring_future_reserves or, without one, to
// the owner.
func deepenRevokeSponsorshipFailure(source string, o xdr.RevokeSponsorshipOp, r Reason) Reason {
	t := newRevokeSponsorshipTarget(o)
	signer := o.Type == xdr.RevokeSponsorshipTypeRevokeSponsorshipSigner
	owner := "the owning account"
	if t.owner != "" {
		owner = "owner " + shortAddr(t.owner)
	}

	switch r.Constant {
	case "RevokeSponsorshipResultCodeRevokeSponsorshipDoesNotExist":
		if signer {
			r.Summary = fmt.Sprintf("The revocation targets %s, but that account does not exist or does not list that signer.", t.label)
			r.Hint = "Check the account ID and signer key against the account's current signer list."
			return r
		}
		r.Summary = fmt.Sprintf("The revocation targets the %s, but no such entry exists on the ledger.", t.label)
		r.Hint = "Check the ledger key; the entry may already have been removed."

	case "RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor":
		submitter := "The source account"
		if source != "" {
			submitter = "Source account " + shortAddr(source)
		}
		target := t.label
		if !signer {
			target = "the " + t.label
		}
		r.Summary = fmt.Sprintf(
			"%s may not revoke sponsorship of %s: a sponsored target can only be revoked by its current sponsor, and an unsponsored one only by %s.",
			submitter, target, owner)
		r.Hint = "Submit the revocation from the current sponsor, which is recorded on the ledger rather than in this operation; look it up before retrying."

	case "RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve":
		target := t.label
		if !signer {
			target = "the " + t.label
		}
		r.Summary = fmt.Sprintf(
			"Revoking sponsorship of %s moves its reserve of %s to the account taking it over, and that account does not hold enough available XLM.",
			target, t.reserveTransfer())
		r.Hint = fmt.Sprintf(
			"Without an enclosing begin_sponsoring_future_reserves, %s must cover the reserve itself; with one, the new sponsor must. Fund that account with at least %s of available XLM at the 0.5 XLM base reserve.",
			owner, t.reserveTransfer())

	case "RevokeSponsorshipResultCodeRevokeSponsorshipOnlyTransferable":
		if t.claimableBalance {
			r.Summary = fmt.Sprintf("The %s must always have a sponsor, so its sponsorship can be transferred but never removed.", t.label)
			r.Hint = "Wrap the revocation in begin_sponsoring_future_reserves and end_sponsoring_future_reserves so a new sponsor takes over the reserve."
		}

	case "RevokeSponsorshipResultCodeRevokeSponsorshipMalformed":
		if !signer && t.malformed != "" {
			r.Summary = fmt.Sprintf("The revocation targets the %s, which is invalid: %s.", t.label, t.malformed)
			r.Hint = "Correct the ledger key so it identifies a sponsorable entry, then resubmit."
		}
	}
	return r
}
