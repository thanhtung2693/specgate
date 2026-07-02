"""Governance contract eval harness.

Responsibilities split into small composable pieces:

- ``load_dataset`` / ``parse_jsonl`` — read versioned JSONL datasets.
- ``Plan`` / ``plan_row`` — describe (without running) what scorers a row
  will trip; used by ``--dry-run`` and by the test suite.
- ``run_row`` — run the contract judge for a row and score the
  structured output.
- ``aggregate`` — fold per-row verdicts into a target summary.
- ``maybe_push_langsmith`` — opt-in mirror of the run results into
  LangSmith when ``LANGSMITH_API_KEY`` is set.

The model + provider-key build paths live in ``_factories.py`` so this
module stays free of the production graph wiring.
"""

from __future__ import annotations

import json
import logging
import os
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from langchain_core.language_models.chat_models import BaseChatModel

from ._factories import (
    PROVIDER_KEYS_READY_EXIT_CODE,
    EvalTarget,
    SubAgentBuildError,
    hydrate_provider_keys,
    provider_keys_available,
)
from .eval_contract import EvalCase, FakeJudge, LiveJudge

logger = logging.getLogger(__name__)


DATASETS_DIR = Path(__file__).parent / "datasets"
THRESHOLDS_PATH = Path(__file__).parent / "thresholds.toml"


# ---------- dataset loading ----------


def parse_jsonl(text: str) -> list[dict[str, Any]]:
    """Parse a JSONL blob into a list of row dicts.

    Empty lines and lines starting with ``#`` are skipped so editors can
    leave inline comments in the source files. Bad rows surface as
    ``ValueError`` with the offending line number — datasets are
    checked-in artifacts; a parse error is a PR problem, not a runtime
    one.
    """
    rows: list[dict[str, Any]] = []
    for lineno, raw in enumerate(text.splitlines(), start=1):
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        try:
            rows.append(json.loads(stripped))
        except json.JSONDecodeError as exc:
            raise ValueError(f"dataset parse error at line {lineno}: {exc}") from exc
    return rows


def load_dataset(target: EvalTarget, *, datasets_dir: Path | None = None) -> list[dict[str, Any]]:
    """Load the JSONL dataset for one eval target."""
    base = datasets_dir or DATASETS_DIR
    path = base / f"{target}.jsonl"
    if not path.exists():
        raise FileNotFoundError(f"dataset not found: {path}")
    return parse_jsonl(path.read_text(encoding="utf-8"))


def load_thresholds(path: Path | None = None) -> dict[str, float]:
    """Load per-target pass-rate gates from ``thresholds.toml``."""
    import tomllib

    src = path or THRESHOLDS_PATH
    if not src.exists():
        return {}
    data = tomllib.loads(src.read_text(encoding="utf-8"))
    return dict(data.get("targets", {}))


# ---------- plan (dry-run preview) ----------


@dataclass
class Plan:
    """What scorers a row will run and what each one expects.

    Built without invoking the contract judge; the harness uses it to
    render the ``--dry-run`` preview and the test suite asserts the
    dispatch table is wired correctly without spending tokens.
    """

    row_id: str
    target: EvalTarget
    scorer_calls: list[str]


def plan_row(target: EvalTarget, row: dict[str, Any]) -> Plan:
    """Return the static scorer plan for a single row."""
    calls: list[str] = []
    if target in {"quality_gate_contract", "ac_satisfaction_contract"}:
        calls.append("contract_judge(verdict + confidence_min)")
    return Plan(row_id=str(row.get("id", "?")), target=target, scorer_calls=calls)


# ---------- verdict shape ----------


@dataclass
class RowVerdict:
    row_id: str
    target: EvalTarget
    passed: bool
    duration_ms: int
    failed_scorers: list[tuple[str, str]] = field(default_factory=list)
    passed_scorers: list[tuple[str, str]] = field(default_factory=list)
    error: str | None = None
    actual_excerpt: dict[str, Any] | None = None


@dataclass
class TargetSummary:
    target: EvalTarget
    total: int
    passed: int
    failed: int
    pass_rate: float
    threshold: float
    threshold_met: bool
    verdicts: list[RowVerdict]


# ---------- run dispatcher ----------


async def run_row(
    target: EvalTarget,
    row: dict[str, Any],
    *,
    main_model: BaseChatModel | None,
    mini_model: BaseChatModel | None,
    contract_judge_mode: str = "fake",
) -> RowVerdict:
    """Run the contract judge for one row and apply the contract scorers."""
    started = time.monotonic()
    actual: dict[str, Any] = {}
    error: str | None = None
    try:
        if target in {
            "quality_gate_contract",
            "ac_satisfaction_contract",
        }:
            case = EvalCase.model_validate(
                {
                    "case_id": row.get("id", ""),
                    "suite": row.get("input", {}).get("suite", row.get("suite", "")),
                    "input": row.get("input", {}),
                    "expected": row.get("expected", {}),
                }
            )
            if contract_judge_mode == "live":
                if main_model is None:
                    raise ValueError("main_model is required for live contract judge mode")
                verdict, confidence, diagnostics = await LiveJudge(main_model).evaluate(case)
            else:
                verdict, confidence, diagnostics = FakeJudge().evaluate(case)
            actual = {
                "verdict": verdict,
                "confidence": confidence,
                "diagnostics": diagnostics,
            }
        else:
            raise ValueError(f"unknown target: {target}")
    except SubAgentBuildError:
        raise
    except Exception as exc:  # pragma: no cover - exercised by the failure tests
        error = f"{type(exc).__name__}: {exc}"

    duration_ms = int((time.monotonic() - started) * 1000)
    if error is not None:
        return RowVerdict(
            row_id=str(row.get("id", "?")),
            target=target,
            passed=False,
            duration_ms=duration_ms,
            error=error,
        )

    expected = row.get("expected", {})
    triples: list[tuple[str, bool, str]] = []
    if "verdict" in expected:
        observed = actual.get("verdict")
        ok = observed == expected["verdict"]
        triples.append(
            (
                "verdict",
                ok,
                f"verdict={observed!r} expected {expected['verdict']!r}",
            )
        )
    if "confidence_min" in expected:
        floor = float(expected["confidence_min"])
        observed_conf = float(actual.get("confidence", 0.0))
        ok = observed_conf >= floor
        triples.append(
            (
                "confidence_at_least(confidence)",
                ok,
                f"confidence={observed_conf:.2f} (floor {floor:.2f})",
            )
        )

    passed = all(t[1] for t in triples) if triples else False
    failed = [(name, reason) for name, ok, reason in triples if not ok]
    passed_list = [(name, reason) for name, ok, reason in triples if ok]
    return RowVerdict(
        row_id=str(row.get("id", "?")),
        target=target,
        passed=passed,
        duration_ms=duration_ms,
        failed_scorers=failed,
        passed_scorers=passed_list,
        actual_excerpt=_excerpt(actual),
    )


def _excerpt(actual: dict[str, Any]) -> dict[str, Any]:
    """Keep the verdict JSON small in CI logs."""
    out: dict[str, Any] = {}
    for k, v in actual.items():
        if isinstance(v, str) and len(v) > 280:
            out[k] = v[:280] + "..."
        else:
            out[k] = v
    return out


# ---------- aggregate ----------


def aggregate(
    target: EvalTarget,
    verdicts: list[RowVerdict],
    *,
    thresholds: dict[str, float],
) -> TargetSummary:
    total = len(verdicts)
    passed = sum(1 for v in verdicts if v.passed)
    failed = total - passed
    pass_rate = (passed / total) if total else 0.0
    threshold = float(thresholds.get(target, 0.0))
    return TargetSummary(
        target=target,
        total=total,
        passed=passed,
        failed=failed,
        pass_rate=pass_rate,
        threshold=threshold,
        threshold_met=pass_rate >= threshold,
        verdicts=verdicts,
    )


# ---------- LangSmith mirror ----------


def maybe_push_langsmith(
    summaries: list[TargetSummary],
    *,
    project: str = "governance-evals",
) -> bool:
    """Mirror summaries into LangSmith via the official client.

    Opt-in: returns ``False`` and skips when ``LANGSMITH_API_KEY`` is
    unset. Each per-row verdict becomes one run under the dataset's
    project; the function tolerates a missing langsmith install so
    offline contributors can still run the harness.
    """
    if not os.getenv("LANGSMITH_API_KEY"):
        return False
    try:
        from langsmith import Client
    except Exception as exc:  # pragma: no cover - langsmith always installed in CI
        logger.warning("langsmith import failed: %s", exc)
        return False
    client = Client()
    for summary in summaries:
        dataset_name = f"governance-{summary.target.replace('_', '-')}"
        try:
            ds = client.read_dataset(dataset_name=dataset_name)
        except Exception:
            ds = client.create_dataset(
                dataset_name=dataset_name,
                description=f"Governance contract eval — {summary.target}",
            )
        for verdict in summary.verdicts:
            try:
                client.create_example(
                    inputs={"row_id": verdict.row_id},
                    outputs={
                        "passed": verdict.passed,
                        "failed_scorers": verdict.failed_scorers,
                        "duration_ms": verdict.duration_ms,
                    },
                    dataset_id=ds.id,
                )
            except Exception as exc:  # pragma: no cover - network surface
                logger.warning("langsmith example push failed: %s", exc)
    return True


__all__ = [
    "PROVIDER_KEYS_READY_EXIT_CODE",
    "Plan",
    "RowVerdict",
    "TargetSummary",
    "EvalTarget",
    "DATASETS_DIR",
    "THRESHOLDS_PATH",
    "aggregate",
    "hydrate_provider_keys",
    "load_dataset",
    "load_thresholds",
    "maybe_push_langsmith",
    "parse_jsonl",
    "plan_row",
    "provider_keys_available",
    "run_row",
]
