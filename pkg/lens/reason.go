package lens

import "strings"

// Reason is a plain-English account of a single Stellar result code.
type Reason struct {
	// Code is the canonical short form, e.g. "tx_bad_seq" or
	// "payment_underfunded". It is derived from the XDR constant name.
	Code string `json:"code"`
	// Constant is the generated XDR constant, e.g.
	// "PaymentResultCodePaymentUnderfunded". Useful when cross-referencing
	// the protocol definition.
	Constant string `json:"constant"`
	// Summary explains what went wrong in one sentence, in the terms a
	// developer debugging the transaction would use.
	Summary string `json:"summary"`
	// Hint suggests what to do about it. It is empty when the code is a
	// success, or when no general advice applies.
	Hint string `json:"hint,omitempty"`
	// Success reports whether this code represents a successful outcome.
	Success bool `json:"success"`
}

// reasonText pairs a summary with a hint at map-construction time.
type reasonText struct {
	summary string
	hint    string
	success bool
}

func ok(summary string) reasonText { return reasonText{summary: summary, success: true} }

func bad(summary, hint string) reasonText { return reasonText{summary: summary, hint: hint} }

// reasons maps every generated XDR result-code constant to plain English.
//
// Keys are exactly the strings returned by the constants' String() method, so
// looking a code up never requires a type switch over the 28 distinct result
// enums. To cover a new protocol result code, add an entry here — nothing
// else needs to change.
//
// Wording deliberately avoids restating the constant. "tx_bad_seq" is not an
// explanation; "the sequence number did not match the source account's next
// expected number" is.
var reasons = map[string]reasonText{
	// ---- Transaction level ------------------------------------------------
	"TransactionResultCodeTxSuccess":             ok("All operations in the transaction succeeded."),
	"TransactionResultCodeTxFeeBumpInnerSuccess": ok("The fee-bump wrapper succeeded and so did the inner transaction."),
	"TransactionResultCodeTxFailed": bad(
		"The transaction was well-formed and authorised, but at least one of its operations failed.",
		"Check the per-operation results: the transaction itself is fine, the failure is inside an operation."),
	"TransactionResultCodeTxFeeBumpInnerFailed": bad(
		"The fee-bump wrapper was valid, but the inner transaction it paid for failed.",
		"The fee was still charged to the fee-bump source. Inspect the inner result for the real cause."),
	"TransactionResultCodeTxTooEarly": bad(
		"The ledger closed before the transaction's minimum time bound.",
		"Wait until minTime has passed, or widen the time bounds."),
	"TransactionResultCodeTxTooLate": bad(
		"The transaction's maximum time bound had already passed when it was submitted.",
		"Rebuild the transaction with a later maxTime. This commonly means the transaction sat in a queue too long."),
	"TransactionResultCodeTxMissingOperation": bad(
		"The transaction contained no operations.",
		"A transaction must carry at least one operation."),
	"TransactionResultCodeTxBadSeq": bad(
		"The sequence number did not match the source account's next expected sequence number.",
		"Reload the source account's current sequence number and rebuild. This is the usual symptom of two clients submitting from one account at once."),
	"TransactionResultCodeTxBadAuth": bad(
		"The signatures present were not enough to meet the required signing threshold.",
		"Check that every required signer has signed and that the network passphrase used for signing matches the target network."),
	"TransactionResultCodeTxInsufficientBalance": bad(
		"Paying the fee would drop the source account below its minimum reserve.",
		"Fund the source account, or lower the fee."),
	"TransactionResultCodeTxNoAccount": bad(
		"The source account does not exist on this network.",
		"Confirm the account was created and funded, and that you are pointed at the right network."),
	"TransactionResultCodeTxInsufficientFee": bad(
		"The fee offered was below what the network accepted for this ledger.",
		"Raise the fee, or submit a fee-bump transaction over the original."),
	"TransactionResultCodeTxBadAuthExtra": bad(
		"The transaction carried signatures that were not needed.",
		"Remove the surplus signatures. Every signature must contribute to meeting a threshold."),
	"TransactionResultCodeTxInternalError": bad(
		"stellar-core hit an unexpected internal error while applying the transaction.",
		"This is not usually caused by your transaction. Retry, and report it if it persists."),
	"TransactionResultCodeTxNotSupported": bad(
		"The transaction uses a feature this network version does not support.",
		"Check the protocol version of the network you are targeting."),
	"TransactionResultCodeTxBadSponsorship": bad(
		"The sponsorship instructions in the transaction were not valid.",
		"Every begin-sponsoring must be matched by an end-sponsoring in the same transaction."),
	"TransactionResultCodeTxBadMinSeqAgeOrGap": bad(
		"The minSeqAge or minSeqLedgerGap precondition was not satisfied.",
		"The source account's sequence number is too recent for the preconditions you set."),
	"TransactionResultCodeTxMalformed": bad(
		"The transaction could not be parsed as a valid transaction.",
		"Something is structurally wrong with the envelope."),
	"TransactionResultCodeTxSorobanInvalid": bad(
		"The Soroban resource declarations attached to the transaction were invalid.",
		"Re-run transaction simulation and use the footprint and resource values it returns."),
	"TransactionResultCodeTxFrozenKeyAccessed": bad(
		"The transaction touched a ledger key that is currently frozen.",
		""),

	// ---- Operation wrapper ------------------------------------------------
	"OperationResultCodeOpInner": ok("The operation ran; see the inner result for its outcome."),
	"OperationResultCodeOpBadAuth": bad(
		"The operation's own source account did not meet its signing threshold.",
		"Operations may specify a source account distinct from the transaction's. That account needs its own signatures."),
	"OperationResultCodeOpNoAccount": bad(
		"The operation's source account does not exist.",
		""),
	"OperationResultCodeOpNotSupported": bad(
		"This operation is not supported by the current network version.",
		""),
	"OperationResultCodeOpTooManySubentries": bad(
		"The operation would push the account past its subentry limit.",
		"An account may hold at most 1000 subentries: trustlines, offers, signers and data entries."),
	"OperationResultCodeOpExceededWorkLimit": bad(
		"The operation exceeded the network's work limit.",
		""),
	"OperationResultCodeOpTooManySponsoring": bad(
		"The account is already sponsoring as many entries as it may.",
		""),

	// ---- Create account ---------------------------------------------------
	"CreateAccountResultCodeCreateAccountSuccess": ok("The account was created."),
	"CreateAccountResultCodeCreateAccountMalformed": bad(
		"The destination account id was not valid.", ""),
	"CreateAccountResultCodeCreateAccountUnderfunded": bad(
		"The source account cannot cover the starting balance without going below its own reserve.", ""),
	"CreateAccountResultCodeCreateAccountLowReserve": bad(
		"The starting balance was below the network's minimum account reserve.",
		"Fund the new account with at least the base reserve (2 XLM on Mainnet at the time of writing)."),
	"CreateAccountResultCodeCreateAccountAlreadyExist": bad(
		"An account with that address already exists.",
		"Use a payment rather than create-account to fund an existing account."),

	// ---- Payment ----------------------------------------------------------
	"PaymentResultCodePaymentSuccess": ok("The payment was made."),
	"PaymentResultCodePaymentMalformed": bad(
		"The payment was structurally invalid, such as a non-positive amount.", ""),
	"PaymentResultCodePaymentUnderfunded": bad(
		"The source account does not hold enough of the asset to send this amount.",
		"Remember that XLM balances must stay above the minimum reserve, so the spendable balance is lower than the total."),
	"PaymentResultCodePaymentSrcNoTrust": bad(
		"The sending account has no trustline for this asset.", ""),
	"PaymentResultCodePaymentSrcNotAuthorized": bad(
		"The sending account is not authorised to transact this asset.",
		"The asset issuer has not authorised this account's trustline."),
	"PaymentResultCodePaymentNoDestination": bad(
		"The destination account does not exist.",
		"Use create-account to fund a brand-new account, or check the address."),
	"PaymentResultCodePaymentNoTrust": bad(
		"The destination has no trustline for this asset.",
		"The destination must establish a trustline before it can receive a non-native asset."),
	"PaymentResultCodePaymentNotAuthorized": bad(
		"The destination is not authorised to hold this asset.", ""),
	"PaymentResultCodePaymentLineFull": bad(
		"The payment would push the destination past its trustline limit.", ""),
	"PaymentResultCodePaymentNoIssuer": bad(
		"The asset's issuing account does not exist.", ""),

	// ---- Path payment (strict receive) ------------------------------------
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSuccess": ok("The path payment completed."),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveMalformed": bad(
		"The path payment was structurally invalid.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveUnderfunded": bad(
		"The source account does not hold enough of the send asset.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSrcNoTrust": bad(
		"The sending account has no trustline for the send asset.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveSrcNotAuthorized": bad(
		"The sending account is not authorised to send this asset.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoDestination": bad(
		"The destination account does not exist.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoTrust": bad(
		"The destination has no trustline for the destination asset.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNotAuthorized": bad(
		"The destination is not authorised to hold the destination asset.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveLineFull": bad(
		"The payment would push the destination past its trustline limit.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveNoIssuer": bad(
		"One of the assets on the path has no issuing account.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveTooFewOffers": bad(
		"There was not enough order-book depth to complete the path.",
		"Liquidity moved between building and submitting. Re-run path finding and resubmit."),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveOfferCrossSelf": bad(
		"The path would have crossed one of the source account's own offers.", ""),
	"PathPaymentStrictReceiveResultCodePathPaymentStrictReceiveOverSendmax": bad(
		"Completing the path would have cost more than sendMax allowed.",
		"The price moved against you. Raise sendMax or re-quote the path."),

	// ---- Path payment (strict send) ---------------------------------------
	"PathPaymentStrictSendResultCodePathPaymentStrictSendSuccess": ok("The path payment completed."),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendMalformed": bad(
		"The path payment was structurally invalid.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendUnderfunded": bad(
		"The source account does not hold enough of the send asset.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendSrcNoTrust": bad(
		"The sending account has no trustline for the send asset.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendSrcNotAuthorized": bad(
		"The sending account is not authorised to send this asset.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendNoDestination": bad(
		"The destination account does not exist.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendNoTrust": bad(
		"The destination has no trustline for the destination asset.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendNotAuthorized": bad(
		"The destination is not authorised to hold the destination asset.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendLineFull": bad(
		"The payment would push the destination past its trustline limit.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendNoIssuer": bad(
		"One of the assets on the path has no issuing account.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendTooFewOffers": bad(
		"There was not enough order-book depth to complete the path.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendOfferCrossSelf": bad(
		"The path would have crossed one of the source account's own offers.", ""),
	"PathPaymentStrictSendResultCodePathPaymentStrictSendUnderDestmin": bad(
		"The path would have delivered less than destMin required.",
		"The price moved against you. Lower destMin or re-quote the path."),

	// ---- Manage sell offer ------------------------------------------------
	"ManageSellOfferResultCodeManageSellOfferSuccess": ok("The offer was created, updated or removed."),
	"ManageSellOfferResultCodeManageSellOfferMalformed": bad(
		"The offer was structurally invalid.", ""),
	"ManageSellOfferResultCodeManageSellOfferSellNoTrust": bad(
		"The account has no trustline for the asset it is selling.", ""),
	"ManageSellOfferResultCodeManageSellOfferBuyNoTrust": bad(
		"The account has no trustline for the asset it is buying.", ""),
	"ManageSellOfferResultCodeManageSellOfferSellNotAuthorized": bad(
		"The account is not authorised to sell this asset.", ""),
	"ManageSellOfferResultCodeManageSellOfferBuyNotAuthorized": bad(
		"The account is not authorised to buy this asset.", ""),
	"ManageSellOfferResultCodeManageSellOfferLineFull": bad(
		"Executing the offer would exceed the account's limit on the bought asset.", ""),
	"ManageSellOfferResultCodeManageSellOfferUnderfunded": bad(
		"The account does not hold enough of the asset it is offering to sell.", ""),
	"ManageSellOfferResultCodeManageSellOfferCrossSelf": bad(
		"The offer would have crossed another offer from the same account.", ""),
	"ManageSellOfferResultCodeManageSellOfferSellNoIssuer": bad(
		"The issuer of the asset being sold does not exist.", ""),
	"ManageSellOfferResultCodeManageSellOfferBuyNoIssuer": bad(
		"The issuer of the asset being bought does not exist.", ""),
	"ManageSellOfferResultCodeManageSellOfferNotFound": bad(
		"The offer id given for update or deletion does not exist.", ""),
	"ManageSellOfferResultCodeManageSellOfferLowReserve": bad(
		"The account cannot meet the reserve required for another offer.", ""),

	// ---- Manage buy offer -------------------------------------------------
	"ManageBuyOfferResultCodeManageBuyOfferSuccess": ok("The offer was created, updated or removed."),
	"ManageBuyOfferResultCodeManageBuyOfferMalformed": bad(
		"The offer was structurally invalid.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferSellNoTrust": bad(
		"The account has no trustline for the asset it is selling.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferBuyNoTrust": bad(
		"The account has no trustline for the asset it is buying.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferSellNotAuthorized": bad(
		"The account is not authorised to sell this asset.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferBuyNotAuthorized": bad(
		"The account is not authorised to buy this asset.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferLineFull": bad(
		"Executing the offer would exceed the account's limit on the bought asset.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferUnderfunded": bad(
		"The account does not hold enough of the asset it is offering to sell.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferCrossSelf": bad(
		"The offer would have crossed another offer from the same account.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferSellNoIssuer": bad(
		"The issuer of the asset being sold does not exist.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferBuyNoIssuer": bad(
		"The issuer of the asset being bought does not exist.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferNotFound": bad(
		"The offer id given for update or deletion does not exist.", ""),
	"ManageBuyOfferResultCodeManageBuyOfferLowReserve": bad(
		"The account cannot meet the reserve required for another offer.", ""),

	// ---- Set options ------------------------------------------------------
	"SetOptionsResultCodeSetOptionsSuccess": ok("The account options were updated."),
	"SetOptionsResultCodeSetOptionsLowReserve": bad(
		"Adding the signer would take the account below its minimum reserve.",
		"Each additional signer raises the account's reserve requirement."),
	"SetOptionsResultCodeSetOptionsTooManySigners": bad(
		"The account already has the maximum number of signers.", ""),
	"SetOptionsResultCodeSetOptionsBadFlags": bad(
		"The combination of account flags requested is not allowed.", ""),
	"SetOptionsResultCodeSetOptionsInvalidInflation": bad(
		"The inflation destination account does not exist.", ""),
	"SetOptionsResultCodeSetOptionsCantChange": bad(
		"One of the requested settings cannot be changed once set.",
		"The auth-immutable flag makes an issuer's flags permanent."),
	"SetOptionsResultCodeSetOptionsUnknownFlag": bad(
		"An unrecognised account flag was set.", ""),
	"SetOptionsResultCodeSetOptionsThresholdOutOfRange": bad(
		"A threshold value was outside the valid range of 0 to 255.", ""),
	"SetOptionsResultCodeSetOptionsBadSigner": bad(
		"The signer given was not valid.",
		"An account cannot add itself as a signer this way."),
	"SetOptionsResultCodeSetOptionsInvalidHomeDomain": bad(
		"The home domain was not a valid domain name.", ""),
	"SetOptionsResultCodeSetOptionsAuthRevocableRequired": bad(
		"The auth-revocable flag must be set before this change is allowed.", ""),

	// ---- Change trust -----------------------------------------------------
	"ChangeTrustResultCodeChangeTrustSuccess": ok("The trustline was created, updated or removed."),
	"ChangeTrustResultCodeChangeTrustMalformed": bad(
		"The asset or limit given was not valid.", ""),
	"ChangeTrustResultCodeChangeTrustNoIssuer": bad(
		"The asset's issuing account does not exist.", ""),
	"ChangeTrustResultCodeChangeTrustInvalidLimit": bad(
		"The new limit is below the balance already held.",
		"Reduce the balance first, or set a limit at or above it."),
	"ChangeTrustResultCodeChangeTrustLowReserve": bad(
		"The account cannot meet the reserve required for another trustline.",
		"Each trustline raises the account's minimum XLM reserve."),
	"ChangeTrustResultCodeChangeTrustSelfNotAllowed": bad(
		"An account cannot open a trustline to an asset it issues itself.", ""),
	"ChangeTrustResultCodeChangeTrustTrustLineMissing": bad(
		"The trustline to modify does not exist.", ""),
	"ChangeTrustResultCodeChangeTrustCannotDelete": bad(
		"The trustline cannot be removed while it still holds a balance or backs an offer.", ""),
	"ChangeTrustResultCodeChangeTrustNotAuthMaintainLiabilities": bad(
		"Removing the trustline is not allowed without authorisation to maintain liabilities.", ""),

	// ---- Allow trust ------------------------------------------------------
	"AllowTrustResultCodeAllowTrustSuccess": ok("The trustline authorisation was updated."),
	"AllowTrustResultCodeAllowTrustMalformed": bad(
		"The asset code given was not valid.", ""),
	"AllowTrustResultCodeAllowTrustNoTrustLine": bad(
		"The account being authorised has no trustline for this asset.", ""),
	"AllowTrustResultCodeAllowTrustTrustNotRequired": bad(
		"The issuer does not have the auth-required flag set, so authorisation is meaningless.", ""),
	"AllowTrustResultCodeAllowTrustCantRevoke": bad(
		"The issuer cannot revoke authorisation because auth-revocable is not set.", ""),
	"AllowTrustResultCodeAllowTrustSelfNotAllowed": bad(
		"An issuer cannot allow-trust itself.", ""),
	"AllowTrustResultCodeAllowTrustLowReserve": bad(
		"The operation would take an account below its minimum reserve.", ""),

	// ---- Account merge ----------------------------------------------------
	"AccountMergeResultCodeAccountMergeSuccess": ok("The account was merged into the destination."),
	"AccountMergeResultCodeAccountMergeMalformed": bad(
		"The merge was structurally invalid, such as merging an account into itself.", ""),
	"AccountMergeResultCodeAccountMergeNoAccount": bad(
		"The destination account does not exist.", ""),
	"AccountMergeResultCodeAccountMergeImmutableSet": bad(
		"The account has the auth-immutable flag set and cannot be merged.", ""),
	"AccountMergeResultCodeAccountMergeHasSubEntries": bad(
		"The account still holds subentries.",
		"Remove all trustlines, offers, signers and data entries before merging."),
	"AccountMergeResultCodeAccountMergeSeqnumTooFar": bad(
		"The account's sequence number is too far ahead for it to be merged safely.", ""),
	"AccountMergeResultCodeAccountMergeDestFull": bad(
		"The destination cannot receive the balance without overflowing.", ""),
	"AccountMergeResultCodeAccountMergeIsSponsor": bad(
		"The account is sponsoring other ledger entries and cannot be merged.", ""),

	// ---- Inflation --------------------------------------------------------
	"InflationResultCodeInflationSuccess": ok("Inflation ran."),
	"InflationResultCodeInflationNotTime": bad(
		"It is not yet time to run inflation.",
		"Inflation was disabled by protocol 12; this operation is obsolete."),

	// ---- Manage data ------------------------------------------------------
	"ManageDataResultCodeManageDataSuccess": ok("The data entry was set or removed."),
	"ManageDataResultCodeManageDataNotSupportedYet": bad(
		"This network version does not support data entries.", ""),
	"ManageDataResultCodeManageDataNameNotFound": bad(
		"The data entry to delete does not exist.", ""),
	"ManageDataResultCodeManageDataLowReserve": bad(
		"The account cannot meet the reserve required for another data entry.", ""),
	"ManageDataResultCodeManageDataInvalidName": bad(
		"The data entry name was not valid.", ""),

	// ---- Bump sequence ----------------------------------------------------
	"BumpSequenceResultCodeBumpSequenceSuccess": ok("The sequence number was bumped."),
	"BumpSequenceResultCodeBumpSequenceBadSeq": bad(
		"The target sequence number was not greater than the current one.", ""),

	// ---- Claimable balances -----------------------------------------------
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceSuccess": ok("The claimable balance was created."),
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceMalformed": bad(
		"The claimable balance was structurally invalid.", ""),
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceLowReserve": bad(
		"The account cannot meet the reserve required for the claimable balance.",
		"The reserve scales with the number of claimants."),
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceNoTrust": bad(
		"The account has no trustline for the asset.", ""),
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceNotAuthorized": bad(
		"The account is not authorised to transact this asset.", ""),
	"CreateClaimableBalanceResultCodeCreateClaimableBalanceUnderfunded": bad(
		"The account does not hold enough of the asset.", ""),

	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceSuccess": ok("The claimable balance was claimed."),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceDoesNotExist": bad(
		"No claimable balance exists with that id.",
		"It may already have been claimed."),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceCannotClaim": bad(
		"This account is not allowed to claim the balance right now.",
		"The claim predicate was not satisfied — often a time bound that has not yet opened, or has already closed."),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceLineFull": bad(
		"Claiming would push the account past its trustline limit.", ""),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceNoTrust": bad(
		"The claiming account has no trustline for the asset.", ""),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceNotAuthorized": bad(
		"The claiming account is not authorised to hold the asset.", ""),
	"ClaimClaimableBalanceResultCodeClaimClaimableBalanceTrustlineFrozen": bad(
		"The claiming account's trustline is frozen.", ""),

	// ---- Sponsorship ------------------------------------------------------
	"BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesSuccess": ok("Sponsorship began."),
	"BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesMalformed": bad(
		"The sponsorship request was structurally invalid.", ""),
	"BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesAlreadySponsored": bad(
		"That account is already being sponsored.", ""),
	"BeginSponsoringFutureReservesResultCodeBeginSponsoringFutureReservesRecursive": bad(
		"Sponsorship cannot be nested this way.", ""),

	"EndSponsoringFutureReservesResultCodeEndSponsoringFutureReservesSuccess": ok("Sponsorship ended."),
	"EndSponsoringFutureReservesResultCodeEndSponsoringFutureReservesNotSponsored": bad(
		"There was no active sponsorship to end.",
		"Every end-sponsoring must be preceded by a begin-sponsoring in the same transaction."),

	"RevokeSponsorshipResultCodeRevokeSponsorshipSuccess": ok("The sponsorship was revoked."),
	"RevokeSponsorshipResultCodeRevokeSponsorshipDoesNotExist": bad(
		"The ledger entry named is not sponsored, or does not exist.", ""),
	"RevokeSponsorshipResultCodeRevokeSponsorshipNotSponsor": bad(
		"This account is not the sponsor of that entry.", ""),
	"RevokeSponsorshipResultCodeRevokeSponsorshipLowReserve": bad(
		"The account taking over the reserve cannot cover it.", ""),
	"RevokeSponsorshipResultCodeRevokeSponsorshipOnlyTransferable": bad(
		"This sponsorship can only be transferred, not revoked outright.", ""),
	"RevokeSponsorshipResultCodeRevokeSponsorshipMalformed": bad(
		"The revoke request was structurally invalid.", ""),

	// ---- Clawback ---------------------------------------------------------
	"ClawbackResultCodeClawbackSuccess": ok("The asset was clawed back."),
	"ClawbackResultCodeClawbackMalformed": bad(
		"The clawback was structurally invalid.", ""),
	"ClawbackResultCodeClawbackNotClawbackEnabled": bad(
		"The trustline was not created with clawback enabled.",
		"Clawback only works on trustlines opened while the issuer had the clawback-enabled flag set."),
	"ClawbackResultCodeClawbackNoTrust": bad(
		"The target account has no trustline for this asset.", ""),
	"ClawbackResultCodeClawbackUnderfunded": bad(
		"The target account does not hold as much of the asset as the clawback asked for.", ""),

	"ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceSuccess": ok("The claimable balance was clawed back."),
	"ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceDoesNotExist": bad(
		"No claimable balance exists with that id.", ""),
	"ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotIssuer": bad(
		"Only the asset's issuer may claw back this balance.", ""),
	"ClawbackClaimableBalanceResultCodeClawbackClaimableBalanceNotClawbackEnabled": bad(
		"The claimable balance was not created with clawback enabled.", ""),

	// ---- Trustline flags --------------------------------------------------
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsSuccess": ok("The trustline flags were updated."),
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsMalformed": bad(
		"The flag change was structurally invalid.", ""),
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsNoTrustLine": bad(
		"The target account has no trustline for this asset.", ""),
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsCantRevoke": bad(
		"Authorisation cannot be revoked because the issuer is not auth-revocable.", ""),
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsInvalidState": bad(
		"The requested combination of trustline flags is not valid.", ""),
	"SetTrustLineFlagsResultCodeSetTrustLineFlagsLowReserve": bad(
		"The change would take an account below its minimum reserve.", ""),

	// ---- Liquidity pools --------------------------------------------------
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositSuccess": ok("The deposit was made."),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositMalformed": bad(
		"The deposit was structurally invalid.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositNoTrust": bad(
		"The account has no trustline for one of the pool's assets or for the pool shares.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositNotAuthorized": bad(
		"The account is not authorised for one of the pool's assets.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositUnderfunded": bad(
		"The account does not hold enough of one of the pool's assets.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositLineFull": bad(
		"The account cannot hold the pool shares this deposit would mint.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositBadPrice": bad(
		"The pool's price moved outside the bounds the deposit allowed.",
		"Widen minPrice and maxPrice, or retry with a fresh quote."),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositPoolFull": bad(
		"The pool has reached its maximum size.", ""),
	"LiquidityPoolDepositResultCodeLiquidityPoolDepositTrustlineFrozen": bad(
		"One of the account's trustlines is frozen.", ""),

	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawSuccess": ok("The withdrawal was made."),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawMalformed": bad(
		"The withdrawal was structurally invalid.", ""),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawNoTrust": bad(
		"The account has no trustline for one of the pool's assets.", ""),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderfunded": bad(
		"The account does not hold as many pool shares as it tried to withdraw.", ""),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawLineFull": bad(
		"Withdrawing would push the account past a trustline limit.", ""),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawUnderMinimum": bad(
		"The withdrawal would have returned less than the minimum requested.", ""),
	"LiquidityPoolWithdrawResultCodeLiquidityPoolWithdrawTrustlineFrozen": bad(
		"One of the account's trustlines is frozen.", ""),

	// ---- Soroban ----------------------------------------------------------
	"InvokeHostFunctionResultCodeInvokeHostFunctionSuccess": ok("The contract call succeeded."),
	"InvokeHostFunctionResultCodeInvokeHostFunctionMalformed": bad(
		"The host function invocation was structurally invalid.", ""),
	"InvokeHostFunctionResultCodeInvokeHostFunctionTrapped": bad(
		"The contract trapped: it panicked, or explicitly returned an error.",
		"This is a failure inside the contract itself, not a protocol-level rejection. Run the call through simulation and read the diagnostic events to find the contract-level cause."),
	"InvokeHostFunctionResultCodeInvokeHostFunctionResourceLimitExceeded": bad(
		"The call used more resources than the transaction declared.",
		"Re-run simulation and use the resource values it returns, with headroom."),
	"InvokeHostFunctionResultCodeInvokeHostFunctionEntryArchived": bad(
		"The call touched a ledger entry that has been archived.",
		"Restore the entry with a restore-footprint operation before calling again."),
	"InvokeHostFunctionResultCodeInvokeHostFunctionInsufficientRefundableFee": bad(
		"The refundable fee was too small to cover rent and events.",
		"Raise the resource fee. Simulation reports the amount needed."),

	"ExtendFootprintTtlResultCodeExtendFootprintTtlSuccess": ok("The entry lifetimes were extended."),
	"ExtendFootprintTtlResultCodeExtendFootprintTtlMalformed": bad(
		"The extend-footprint-TTL operation was structurally invalid.", ""),
	"ExtendFootprintTtlResultCodeExtendFootprintTtlResourceLimitExceeded": bad(
		"The operation exceeded its declared resource limits.", ""),
	"ExtendFootprintTtlResultCodeExtendFootprintTtlInsufficientRefundableFee": bad(
		"The refundable fee was too small to cover the rent this extension requires.", ""),

	"RestoreFootprintResultCodeRestoreFootprintSuccess": ok("The archived entries were restored."),
	"RestoreFootprintResultCodeRestoreFootprintMalformed": bad(
		"The restore-footprint operation was structurally invalid.", ""),
	"RestoreFootprintResultCodeRestoreFootprintResourceLimitExceeded": bad(
		"The operation exceeded its declared resource limits.", ""),
	"RestoreFootprintResultCodeRestoreFootprintInsufficientRefundableFee": bad(
		"The refundable fee was too small to cover the rent this restore requires.", ""),
}

// ExplainCode returns the plain-English Reason for an XDR result-code
// constant name, as produced by the constant's String() method.
//
// Unknown codes still yield a usable Reason: the short code is derived from
// the constant name and the summary says plainly that no description is on
// file. That keeps the tool useful against a network newer than this build.
func ExplainCode(constant string) Reason {
	short := shortCode(constant)
	text, found := reasons[constant]
	if !found {
		return Reason{
			Code:     short,
			Constant: constant,
			Summary:  "No description on file for this result code. It may come from a newer protocol version than this build of lens knows about.",
			Success:  strings.HasSuffix(constant, "Success"),
		}
	}
	return Reason{
		Code:     short,
		Constant: constant,
		Summary:  text.summary,
		Hint:     text.hint,
		Success:  text.success,
	}
}

// shortCode converts a generated constant name into the snake_case short form
// developers see in Horizon responses. The constant repeats its own type
// prefix — "PaymentResultCode" followed by "PaymentUnderfunded" — so trimming
// the "...ResultCode" prefix leaves the meaningful part.
func shortCode(constant string) string {
	name := constant
	if idx := strings.Index(name, "ResultCode"); idx >= 0 {
		name = name[idx+len("ResultCode"):]
	}
	if name == "" {
		name = constant
	}
	return toSnake(name)
}
