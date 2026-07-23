package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/specgate/specgate/app/cli/internal/client"
)

type checkoutReceiptComparison struct {
	Checked bool
	Matches bool
	Stale   bool
	Message string
	Reason  string
}

func compareCheckoutReceipt(ctx context.Context, deps *Deps, stored gitReceipt) checkoutReceiptComparison {
	if stored.Availability != "" && stored.Availability != "available" {
		return checkoutReceiptComparison{Message: "No stored receipt was checked against the current checkout because its Git metadata is unavailable."}
	}
	if strings.TrimSpace(stored.HeadRevision) == "" {
		return checkoutReceiptComparison{Message: "No stored receipt was checked against the current checkout."}
	}

	current := collectGitReceiptWithPriorBase(
		ctx,
		deps.DeployRunner,
		deliveryWorkingDir(deps),
		nil,
		stored.BaseRevision,
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

	var differences []string
	compareReceiptField := func(label, before, after string) {
		if strings.TrimSpace(before) != "" && strings.TrimSpace(before) != strings.TrimSpace(after) {
			differences = append(differences, label)
		}
	}
	compareReceiptField("repository", stored.Repository, current.Repository)
	compareReceiptField("branch", stored.Branch, current.Branch)
	compareReceiptField("base revision", stored.BaseRevision, current.BaseRevision)
	compareReceiptField("HEAD", stored.HeadRevision, current.HeadRevision)
	compareReceiptField("working tree digest", stored.DiffDigest, current.DiffDigest)

	if len(differences) == 0 {
		return checkoutReceiptComparison{
			Checked: true,
			Matches: true,
			Message: "Stored receipt matches the current checkout.",
		}
	}
	return checkoutReceiptComparison{
		Checked: true,
		Stale:   true,
		Message: "Current checkout differs from the stored receipt: " + strings.Join(differences, ", ") + ".",
		Reason:  "Checkout differs from stored Git receipt",
	}
}

func clientGitReceipt(receipt *client.GitReceipt) gitReceipt {
	if receipt == nil {
		return gitReceipt{}
	}
	return gitReceipt{
		Repository: receipt.Repository, Availability: receipt.Availability,
		Branch: receipt.Branch, BaseRevision: receipt.BaseRevision,
		HeadRevision: receipt.HeadRevision, ChangedFiles: receipt.ChangedFiles,
		DiffDigest: receipt.DiffDigest, Warnings: receipt.Warnings,
	}
}

func mapGitReceipt(body map[string]any) gitReceipt {
	raw, _ := body["git_receipt"].(map[string]any)
	if raw == nil {
		return gitReceipt{}
	}
	return gitReceipt{
		Repository:   strings.TrimSpace(fmt.Sprint(raw["repository"])),
		Availability: strings.TrimSpace(fmt.Sprint(raw["availability"])),
		Branch:       strings.TrimSpace(fmt.Sprint(raw["branch"])),
		BaseRevision: strings.TrimSpace(fmt.Sprint(raw["base_revision"])),
		HeadRevision: strings.TrimSpace(fmt.Sprint(raw["head_revision"])),
		ChangedFiles: stringSlice(raw["changed_files"]),
		DiffDigest:   strings.TrimSpace(fmt.Sprint(raw["diff_digest"])),
		Warnings:     stringSlice(raw["warnings"]),
	}
}

func applyCheckoutFreshness(
	ctx context.Context,
	deps *Deps,
	result changeStatusResult,
	stored gitReceipt,
) changeStatusResult {
	comparison := compareCheckoutReceipt(ctx, deps, stored)
	result.Freshness = comparison.Message
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
