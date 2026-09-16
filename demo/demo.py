#!/usr/bin/env python3
"""Minimal executable proof for Liminal Rail Metro v0.1.

No external dependencies are required.
"""

from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from typing import Any


ROUTES = {
    "code.implement": "code-agent",
    "qa.verify": "qa-agent",
}


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def canonical_json(value: Any) -> str:
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )


def sha256_json(value: Any) -> str:
    return hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()


def make_packet() -> dict[str, Any]:
    return {
        "protocol": "metro.packet.v0.1",
        "action_id": "action-demo-001",
        "source_agent": "research-agent",
        "created_at": now_iso(),
        "goal": "Implement one bounded route adapter.",
        "action": {
            "kind": "code.implement",
            "inputs": {
                "research_summary": "Fast router candidate identified",
                "task": "implement bounded route adapter",
            },
        },
        "allowed_targets": ["code-agent", "qa-agent"],
        "context_refs": ["evidence://demo/research-note"],
        "constraints": {
            "timeout_ms": 5000,
            "side_effect": False,
        },
    }


def route_packet(packet: dict[str, Any]) -> dict[str, Any]:
    kind = packet["action"]["kind"]
    selected_target = ROUTES.get(kind)

    if selected_target is None:
        raise ValueError(f"No route for action kind: {kind}")

    if selected_target not in packet["allowed_targets"]:
        raise ValueError(
            f"Router selected disallowed target {selected_target!r} "
            f"for action {packet['action_id']}"
        )

    return {
        "protocol": "metro.route.v0.1",
        "route_id": f"route-{packet['action_id']}",
        "action_id": packet["action_id"],
        "router_id": "metro-router-local",
        "decision_mode": "deterministic",
        "selected_target": selected_target,
        "candidates": [{"target": selected_target, "score": 1.0}],
        "confidence": 1.0,
        "policy_ref": "policy://demo/route-by-action-kind",
        "decided_at": now_iso(),
    }


def execute(
    packet: dict[str, Any],
    route: dict[str, Any],
) -> tuple[dict[str, Any], dict[str, Any]]:
    if route["action_id"] != packet["action_id"]:
        raise ValueError("Route is not bound to this packet")

    if route["selected_target"] not in packet["allowed_targets"]:
        raise ValueError("Route target is not allowed by the packet")

    started_at = now_iso()

    # Toy execution boundary. Replace this with a real agent/tool adapter later.
    result = {
        "artifact_ref": "memory://demo/code-adapter.py",
        "status": "implemented",
    }

    receipt = {
        "protocol": "metro.receipt.v0.1",
        "receipt_id": f"receipt-{packet['action_id']}",
        "action_id": packet["action_id"],
        "route_id": route["route_id"],
        "executor_id": route["selected_target"],
        "status": "SUCCEEDED",
        "hash_algorithm": "sha256",
        "input_hash": sha256_json(packet["action"]["inputs"]),
        "result_hash": sha256_json(result),
        "result_ref": result["artifact_ref"],
        "started_at": started_at,
        "completed_at": now_iso(),
    }
    return result, receipt


def verify(
    packet: dict[str, Any],
    route: dict[str, Any],
    result: dict[str, Any],
    receipt: dict[str, Any],
) -> list[str]:
    checks = {
        "action identity": receipt["action_id"] == packet["action_id"],
        "route identity": receipt["route_id"] == route["route_id"],
        "executor binding": receipt["executor_id"] == route["selected_target"],
        "allowed target": route["selected_target"] in packet["allowed_targets"],
        "input hash": receipt["input_hash"]
        == sha256_json(packet["action"]["inputs"]),
        "result hash": receipt["result_hash"] == sha256_json(result),
    }

    failed = [name for name, passed in checks.items() if not passed]
    if failed:
        raise AssertionError("Verification failed: " + ", ".join(failed))

    return list(checks)


def main() -> None:
    packet = make_packet()
    route = route_packet(packet)
    result, receipt = execute(packet, route)
    verified = verify(packet, route, result, receipt)

    print(
        json.dumps(
            {
                "packet": packet,
                "route": route,
                "result": result,
                "receipt": receipt,
                "verification": {
                    "status": "PASS",
                    "checks": verified,
                },
            },
            indent=2,
            ensure_ascii=False,
        )
    )


if __name__ == "__main__":
    main()
