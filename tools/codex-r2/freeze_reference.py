#!/usr/bin/env python3
"""Fetch fixed public source inputs, verify known Git blobs, and hash them.

Downloaded files are never executed. No credentials or model endpoint are used.
The immutable cache is within the current task checkout, not a runtime directory.
"""
from __future__ import annotations

import hashlib
import json
import subprocess
import urllib.request
from pathlib import Path

BASE = "434112e816951080f3e9619f30d5f5bc1165c8a0"
REFERENCE = "6b9826e3aa83b1a5947db50f4332cb9c65f1b340"
SOURCES = {
    "codex-rs/core/src/responses_metadata.rs": "3c1fd729fb7792a2b49e3347749eaff76eb1e6e6",
    "codex-rs/core/tests/suite/prompt_cache_key.rs": "5e258a936f542c5342612c11bf76d4471472f390",
    "codex-rs/login/src/auth/default_client.rs": "cd4cbd5431a785b0a1f2a9963b1536d2edf135ec",
    "codex-rs/protocol/src/thread_id.rs": None,
    "codex-rs/codex-api/src/requests/headers.rs": None,
    "codex-rs/core/src/client.rs": None,
    "codex-rs/codex-api/tests/clients.rs": None,
    "LICENSE": None,
}
BASELINE_PATHS = [
    "backend/internal/service/openai_codex_account_identity.go",
    "backend/internal/service/openai_codex_identity.go",
    "backend/internal/service/openai_r1_canary.go",
    "backend/internal/service/openai_gateway_forward.go",
    "backend/internal/service/openai_gateway_passthrough.go",
    "backend/internal/service/openai_ws_pool.go",
    "backend/internal/service/openai_ws_execution_scope.go",
    "backend/go.mod", "backend/go.sum", "Dockerfile",
    ".github/workflows/backend-ci.yml", ".github/workflows/security-scan.yml",
]


def git_blob(data: bytes) -> str:
    return hashlib.sha1(b"blob " + str(len(data)).encode("ascii") + b"\0" + data).hexdigest()


def main() -> None:
    root = Path(__file__).resolve().parents[2]
    subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], cwd=root, check=True)
    cache = root / ".cache" / "codex-r2" / "reference" / REFERENCE
    manifest = {
        "schema_version": 1,
        "baseline_commit": BASE,
        "reference_repository": "openai/codex",
        "reference_commit": REFERENCE,
        "reference_tag": "rust-v0.154.0",
        "reference_files": [],
        "baseline_files": [],
        "runtime_rules": "No live fetching. OAuth runtime equivalence requires independent synthetic fixtures; API-key tests are not OAuth evidence.",
    }
    for relative, expected_blob in SOURCES.items():
        target = cache / relative
        url = f"https://raw.githubusercontent.com/openai/codex/{REFERENCE}/{relative}"
        if target.exists():
            data = target.read_bytes()
        else:
            request = urllib.request.Request(url, headers={"User-Agent": "r2-reference-freezer"})
            with urllib.request.urlopen(request, timeout=30) as response:
                if response.geturl() != url:
                    raise RuntimeError("Unexpected source redirect")
                data = response.read(2_000_001)
            if len(data) > 2_000_000:
                raise RuntimeError("Reference file exceeds bounded fetch")
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        blob = git_blob(data)
        if expected_blob is not None and blob != expected_blob:
            raise RuntimeError(f"Pinned blob mismatch: {relative}")
        manifest["reference_files"].append({
            "path": relative, "url": url, "bytes": len(data),
            "git_blob_sha1": blob, "sha256": hashlib.sha256(data).hexdigest(),
        })
    for relative in BASELINE_PATHS:
        data = subprocess.check_output(["git", "show", f"{BASE}:{relative}"], cwd=root)
        manifest["baseline_files"].append({
            "path": relative, "bytes": len(data),
            "git_blob_sha1": git_blob(data), "sha256": hashlib.sha256(data).hexdigest(),
        })
    out = root / "docs" / "r2" / "reference-manifest.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    encoded = (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode()
    if out.exists() and out.read_bytes() != encoded:
        raise RuntimeError("Existing reference manifest differs; inspect drift before replacing")
    out.write_bytes(encoded)
    print(json.dumps({
        "reference_freeze": "passed", "reference_files": len(SOURCES),
        "baseline_files": len(BASELINE_PATHS),
        "manifest_sha256": hashlib.sha256(encoded).hexdigest(),
        "downloaded_source_executed": False,
        "real_model_requests": 0,
    }))


if __name__ == "__main__":
    main()
