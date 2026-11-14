package command

import (
	"context"
	"fmt"
	"strings"
)

type checkoutReceiptComparison struct {
	Checked bool
	Matches bool
	Stale   bool
	Message string
	Reason  string
}

func compareCheckoutReceipt(ctx context.Context, deps *Deps, stored gitReceipt, generation string) checkoutReceiptComparison {
	historical := stored.CheckoutGeneration != generation
	scope := receiptFreshnessScope(stored)
	if historical && scope == "local_checkout" {
		return checkoutReceiptComparison{Message: "Stored checkout receipt belongs to a different Local store generation and cannot be compared with this checkout."}
	}
	if stored.Availability != "" && stored.Availability != "available" {
		return checkoutReceiptComparison{Message: "No stored receipt was checked against the current checkout because its Git metadata is unavailable."}
	}
	if strings.TrimSpace(stored.HeadRevision) == "" {
		return checkoutReceiptComparison{Message: "No stored receipt was checked against the current checkout."}
	}

	current := collectGitReceiptWithPriorBase(
		ctx,
		deps.CommandRunner,
		deliveryWorkingDir(deps),
		nil,
		stored.BaseRevision,
		generation,
	)
	if current.Availability != "available" || strings.TrimSpace(current.HeadRevision) == "" {
		detail := "Git metadata is unavailable"
		if len(current.Warnings) > 0 {
			detail = strings.TrimSpace(current.Warnings[0])
		}
		return checkoutReceiptComparison{
			Message: fmt.Sprintf("Could not compare the stored receipt with the current checkout: %s.", strings.TrimSuffix(detail, ".")),
		}
	}

	if scope == "local_checkout" && (stored.CheckoutID == "" || current.CheckoutID == "" || stored.CheckoutID != current.CheckoutID) {
		return checkoutReceiptComparison{Message: "Could not compare the stored receipt with this local checkout: checkout identity is missing or different."}
	}
	var differences []string
	compareReceiptField := func(label, before, after string) {
		if strings.TrimSpace(before) != "" && strings.TrimSpace(before) != strings.TrimSpace(after) {
			differences = append(differences, label)
		}
	}
	if scope != "local_checkout" {
		compareReceiptField("repository", stored.Repository, current.Repository)
		compareReceiptField("base revision", stored.BaseRevision, current.BaseRevision)
	}
	if historical && (stored.Repository == "" || current.Repository == "" || stored.BaseRevision == "" || current.BaseRevision == "") {
		return checkoutReceiptComparison{Message: "Shared-repository Git endpoint comparison is unavailable for the historical receipt."}
	}
	if historical && stored.Branch == "" && current.Branch != "" {
		differences = append(differences, "branch")
	} else {
		compareReceiptField("branch", stored.Branch, current.Branch)
	}
	compareReceiptField("HEAD", stored.HeadRevision, current.HeadRevision)
	if len(differences) == 0 {
		if historical && (current.Dirty || stored.DiffDigest == "" || current.DiffDigest == "" || stored.DiffDigest != current.DiffDigest) {
			return checkoutReceiptComparison{Message: "Shared-repository Git endpoints match the historical receipt. Working-tree comparability is unverified."}
		}
		if stored.DiffDigest == "" || current.DiffDigest == "" {
			return checkoutReceiptComparison{Message: "Checkout freshness is unverified because a working-tree digest is unavailable."}
		}
	}
	if stored.DiffDigest != "" && current.DiffDigest != "" {
		compareReceiptField("working tree digest", stored.DiffDigest, current.DiffDigest)
	}
	matchMessage := "Stored receipt matches the current checkout."
	differenceMessage := "Current checkout differs from the stored receipt: "
	if scope == "local_checkout" {
		matchMessage = "Stored receipt matches this local checkout."
		differenceMessage = "This local checkout differs from the stored receipt: "
	}

	if historical {
		matchMessage = "Shared-repository Git endpoints and the working-tree digest match the historical receipt."
		differenceMessage = "Shared-repository Git endpoints differ from the historical receipt: "
	}
	if len(differences) == 0 {
		return checkoutReceiptComparison{
			Checked: true,
			Matches: true,
			Message: matchMessage,
		}
	}
	return checkoutReceiptComparison{
		Checked: true,
		Stale:   true,
		Message: differenceMessage + strings.Join(differences, ", ") + ".",
		Reason:  "Checkout differs from stored Git receipt",
	}
}

func receiptFreshnessScope(receipt gitReceipt) string {
	if scope := strings.TrimSpace(receipt.FreshnessScope); scope != "" {
		return scope
	}
	return "shared_repository"
}

func mapGitReceipt(body map[string]any) gitReceipt {
	raw, _ := body["git_receipt"].(map[string]any)
	if raw == nil {
		return gitReceipt{}
	}
	text := func(field string) string {
		value, _ := raw[field].(string)
		return strings.TrimSpace(value)
	}
	return gitReceipt{
		Repository:         text("repository"),
		CheckoutID:         text("checkout_id"),
		CheckoutGeneration: text("checkout_generation"),
		Availability:       text("availability"),
		FreshnessScope:     text("freshness_scope"),
		Branch:             text("branch"),
		BaseRevision:       text("base_revision"),
		HeadRevision:       text("head_revision"),
		ChangedFiles:       stringSlice(raw["changed_files"]),
		DiffDigest:         text("diff_digest"),
		Warnings:           stringSlice(raw["warnings"]),
	}
}

func applyCheckoutFreshness(
	ctx context.Context,
	deps *Deps,
	result changeStatusResult,
	stored gitReceipt,
	generation string,
) changeStatusResult {
	comparison := compareCheckoutReceipt(ctx, deps, stored, generation)
	result.FreshnessUnchecked = !comparison.Checked
	result.Freshness = comparison.Message
	result.HistoricalVerification = stored.CheckoutGeneration != generation
	if result.HistoricalVerification {
		result.Freshness += " Recorded verification is historical and belongs to another Local store generation."
		result.Assurance = strings.Replace(result.Assurance, "locally reproduced", "reproduced by the source SpecGate CLI", 1)
	}
	if comparison.Stale {
		if result.StaleReason == "" {
			result.StaleReason = comparison.Reason
		} else if !strings.Contains(result.StaleReason, comparison.Reason) {
			result.StaleReason += "; " + comparison.Reason
		}
		result.Stale = true
	}
	if result.StaleReason == "Peer review is stale" {
		result.Freshness += " Peer review is stale."
	}
	return result
}
