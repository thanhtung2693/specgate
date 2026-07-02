"""CLI entry point for the governance contract eval harness.

Usage::

    uv run python -m evals.run --target quality_gate_contract
    uv run python -m evals.run --target all --dry-run
    LANGSMITH_API_KEY=... uv run python -m evals.run --target quality_gate_contract --push

Exit codes:

- ``0`` — all selected targets met their threshold.
- ``1`` — one or more targets fell below their threshold.
- ``2`` — CLI argument error.
- ``78`` — provider keys not configured; the harness could not invoke
  real models.
- ``3`` — pre-run cost estimate exceeded ``--cost-cap``.

Output is human-readable summaries on stderr and a single structured
JSON payload on stdout when ``--json`` is set (handy for CI). Pass
``--json-out PATH`` to also write the summary list to a file (suits CI
runners that consume the JSON in a follow-up step).
"""

from __future__ import annotations

import argparse
import asyncio
import dataclasses
import json
import sys
from pathlib import Path
from typing import Any

from . import harness
from ._factories import (
    PROVIDER_KEYS_READY_EXIT_CODE,
    SubAgentBuildError,
    build_main_model,
    build_mini_model,
    hydrate_provider_keys,
    provider_keys_available,
)

ALL_TARGETS: list[harness.EvalTarget] = [
    "quality_gate_contract",
    "ac_satisfaction_contract",
]

CONTRACT_ONLY_TARGETS: set[harness.EvalTarget] = {
    "quality_gate_contract",
    "ac_satisfaction_contract",
}

COST_CAP_EXCEEDED_EXIT_CODE = 3

# Per-row USD cost estimate for each target. The contract targets default
# to the deterministic FakeJudge (zero cost); the live judge mode calls the
# main model once per row. The cost guard multiplies these by the row count
# selected and aborts before tokens are spent when the total exceeds
# --cost-cap.
PER_ROW_COST_USD: dict[str, float] = {
    "quality_gate_contract": 0.0,
    "ac_satisfaction_contract": 0.0,
}


def estimate_cost_usd(
    targets: list[harness.EvalTarget],
    *,
    row_counts: dict[str, int],
) -> float:
    """Pre-run cost estimate in USD across the selected targets."""
    total = 0.0
    for target in targets:
        rows = row_counts.get(target, 0)
        per_row = PER_ROW_COST_USD.get(target, 0.0)
        total += rows * per_row
    return total


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="evals.run",
        description="Run the governance contract eval harness.",
    )
    parser.add_argument(
        "--target",
        choices=[*ALL_TARGETS, "all"],
        required=True,
        help="Which governance contract dataset to run.",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print dataset + scorer plan without invoking the LLM.",
    )
    parser.add_argument(
        "--push",
        action="store_true",
        help=(
            "Mirror results into LangSmith. Requires LANGSMITH_API_KEY; silently skips when unset."
        ),
    )
    parser.add_argument(
        "--datasets-dir",
        type=Path,
        default=None,
        help="Override the dataset directory (defaults to evals/datasets).",
    )
    parser.add_argument(
        "--thresholds",
        type=Path,
        default=None,
        help="Override the thresholds.toml path.",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Emit a single structured JSON summary on stdout.",
    )
    parser.add_argument(
        "--json-out",
        type=Path,
        default=None,
        help=(
            "Write the per-target summary list as JSON to PATH. Suits CI runners "
            "that consume the JSON in a follow-up step (step summary, artifact "
            "upload). Independent of --json (stdout)."
        ),
    )
    parser.add_argument(
        "--cost-cap",
        type=float,
        default=None,
        help=(
            "Pre-run USD cost cap. The harness estimates total cost from row "
            "counts and a per-target rate; if the estimate exceeds the cap the "
            "run aborts with exit code 3 before any tokens are spent."
        ),
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=None,
        help="Run at most N rows per target. Handy for spot-checks.",
    )
    parser.add_argument(
        "--contract-judge",
        choices=["fake", "live"],
        default="fake",
        help=(
            "Judge mode for contract targets. fake=deterministic CI baseline, "
            "live=model calibration."
        ),
    )
    return parser.parse_args(argv)


def _resolve_targets(arg: str) -> list[harness.EvalTarget]:
    if arg == "all":
        return list(ALL_TARGETS)
    return [arg]  # type: ignore[list-item]


def _print_dry_run(target: harness.EvalTarget, rows: list[dict[str, Any]]) -> None:
    print(f"\n=== {target}: {len(rows)} rows ===", file=sys.stderr)
    for row in rows:
        plan = harness.plan_row(target, row)
        print(f"  {plan.row_id}: {plan.scorer_calls}", file=sys.stderr)


def _print_summary(summary: harness.TargetSummary) -> None:
    status = "PASS" if summary.threshold_met else "FAIL"
    print(
        f"\n[{status}] {summary.target}: "
        f"{summary.passed}/{summary.total} passed "
        f"({summary.pass_rate:.0%}, threshold {summary.threshold:.0%})",
        file=sys.stderr,
    )
    for verdict in summary.verdicts:
        marker = "ok" if verdict.passed else "FAIL"
        line = f"  [{marker}] {verdict.row_id} ({verdict.duration_ms}ms)"
        if verdict.error:
            line += f" — error: {verdict.error}"
        elif verdict.failed_scorers:
            details = "; ".join(f"{n}: {r}" for n, r in verdict.failed_scorers)
            line += f" — {details}"
        print(line, file=sys.stderr)


async def _run_async(args: argparse.Namespace) -> int:
    targets = _resolve_targets(args.target)
    thresholds = harness.load_thresholds(args.thresholds)

    # Dry-run path: load datasets, print scorer plans, do not touch the LLM.
    if args.dry_run:
        for target in targets:
            rows = harness.load_dataset(target, datasets_dir=args.datasets_dir)
            if args.limit:
                rows = rows[: args.limit]
            _print_dry_run(target, rows)
        payload = {
            "dry_run": True,
            "targets": targets,
            "row_counts": {
                t: len(harness.load_dataset(t, datasets_dir=args.datasets_dir)) for t in targets
            },
            "thresholds": thresholds,
        }
        if args.json:
            print(json.dumps(payload, indent=2))
        if args.json_out is not None:
            args.json_out.parent.mkdir(parents=True, exist_ok=True)
            args.json_out.write_text(json.dumps(payload, indent=2), encoding="utf-8")
        return 0

    # Cost guard: estimate before any LLM contact and bail early when over cap.
    row_counts: dict[str, int] = {}
    for target in targets:
        rows = harness.load_dataset(target, datasets_dir=args.datasets_dir)
        if args.limit:
            rows = rows[: args.limit]
        row_counts[target] = len(rows)
    estimated_cost = estimate_cost_usd(targets, row_counts=row_counts)
    if args.cost_cap is not None and estimated_cost > args.cost_cap:
        print(
            f"cost guard: estimated USD {estimated_cost:.2f} exceeds cap "
            f"USD {args.cost_cap:.2f} for targets={targets} rows={row_counts}",
            file=sys.stderr,
        )
        return COST_CAP_EXCEEDED_EXIT_CODE

    needs_llm_models = any(target not in CONTRACT_ONLY_TARGETS for target in targets) or (
        args.contract_judge == "live" and any(target in CONTRACT_ONLY_TARGETS for target in targets)
    )
    main_model = None
    mini_model = None
    if needs_llm_models:
        # Real run: hydrate provider keys then build the shared chat models.
        hydrate_provider_keys()
        ready, missing = provider_keys_available()
        if not ready:
            msg = (
                "provider keys not configured for: "
                f"{missing}. Set the provider's env var (OPENAI_API_KEY / "
                "GOOGLE_API_KEY / ANTHROPIC_API_KEY) or seed Doc Registry "
                "model settings, then retry."
            )
            print(msg, file=sys.stderr)
            return PROVIDER_KEYS_READY_EXIT_CODE

        try:
            main_model = build_main_model()
            mini_model = build_mini_model()
        except SubAgentBuildError as exc:
            print(f"failed to build models: {exc}", file=sys.stderr)
            return PROVIDER_KEYS_READY_EXIT_CODE

    summaries: list[harness.TargetSummary] = []
    overall_ok = True
    for target in targets:
        rows = harness.load_dataset(target, datasets_dir=args.datasets_dir)
        if args.limit:
            rows = rows[: args.limit]
        verdicts: list[harness.RowVerdict] = []
        for row in rows:
            verdict = await harness.run_row(
                target,
                row,
                main_model=main_model,
                mini_model=mini_model,
                contract_judge_mode=args.contract_judge,
            )
            verdicts.append(verdict)
        summary = harness.aggregate(target, verdicts, thresholds=thresholds)
        summaries.append(summary)
        _print_summary(summary)
        if not summary.threshold_met:
            overall_ok = False

    if args.push:
        pushed = harness.maybe_push_langsmith(summaries)
        print(f"\nlangsmith push: {'sent' if pushed else 'skipped (no key)'}", file=sys.stderr)

    summary_payload = [dataclasses.asdict(s) for s in summaries]
    if args.json:
        print(json.dumps(summary_payload, indent=2, default=str))
    if args.json_out is not None:
        args.json_out.parent.mkdir(parents=True, exist_ok=True)
        args.json_out.write_text(
            json.dumps(summary_payload, indent=2, default=str),
            encoding="utf-8",
        )

    return 0 if overall_ok else 1


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    return asyncio.run(_run_async(args))


if __name__ == "__main__":
    raise SystemExit(main())
