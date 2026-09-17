#!/usr/bin/env python3
"""Manual-only disposable B9 lifecycle rehearsal.

Default execution is plan-only. Docker mutation requires both --execute and
--ack DISPOSABLE_ONLY. The assistant-side execution of this lifecycle was
safety-blocked, so this file is not acceptance evidence until a human executes
it on the VPS and its JSON result is independently read back.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
from typing import Any


PREFIX = "sub2api-r1-rehearsal-b9"
CANDIDATE_IMAGE = "sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85"
PREVIOUS_IMAGE = "sha256:5d5c2cdd45e8c944fec9aa5d379b5361f704dfcdf6d3be4f3446242d2e133cc2"
POSTGRES_IMAGE = "sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7"
REDIS_IMAGE = "sha256:d146f83b1e0f02fc27c26a50cee39338c736674c5959db84363e6ae3cd9e02d2"
NETWORK = PREFIX + "-net"
APP = PREFIX + "-app"
POSTGRES = PREFIX + "-postgres"
REDIS = PREFIX + "-redis"
PG_VOLUME = PREFIX + "-pgdata"
APP_VOLUME = PREFIX + "-appdata"
DEFAULT_EVIDENCE = Path("/opt/sub2api-r1-staging/tests/b9-r3/lifecycle-rehearsal.json")
CORE_CONTAINERS = (
    "sub2api",
    "sub2api-postgres",
    "sub2api-redis",
    "sub2api-r1-staging-app-1",
    "sub2api-r1-staging-postgres-1",
    "sub2api-r1-staging-redis-1",
)


def rehearsal_plan() -> dict[str, Any]:
    return {
        "mode": "plan_only",
        "prefix": PREFIX,
        "network": {"name": NETWORK, "internal": True},
        "published_ports": [],
        "images": {
            "candidate": CANDIDATE_IMAGE,
            "previous": PREVIOUS_IMAGE,
            "postgres": POSTGRES_IMAGE,
            "redis": REDIS_IMAGE,
        },
        "sequence": ["candidate_initial", "candidate_cold_recreate", "previous_image", "candidate_restored"],
        "guards": [
            "refuse_preexisting_rehearsal_resources",
            "verify_exact_image_ids",
            "snapshot_production_and_existing_staging_core_ids",
            "generate_synthetic_secrets_in_process_only",
            "never_publish_host_ports",
            "never_join_production_or_existing_staging_networks",
            "cleanup_only_resources_created_by_this_invocation",
        ],
        "real_model_requests": 0,
    }


class Docker:
    def __init__(self) -> None:
        self.containers: list[str] = []
        self.volumes: list[str] = []
        self.network = False

    @staticmethod
    def run(args: list[str], timeout: int = 120, check: bool = True) -> subprocess.CompletedProcess[str]:
        proc = subprocess.run(args, capture_output=True, text=True, timeout=timeout, check=False)
        if check and proc.returncode:
            raise RuntimeError(f"{args[0]} exited {proc.returncode}: {proc.stderr[-1200:]}")
        return proc

    def inspect(self, name: str) -> dict[str, Any]:
        return json.loads(self.run(["docker", "inspect", name]).stdout)[0]

    def container_exists(self, name: str) -> bool:
        return self.run(["docker", "inspect", name], check=False).returncode == 0

    def volume_exists(self, name: str) -> bool:
        return self.run(["docker", "volume", "inspect", name], check=False).returncode == 0

    def network_exists(self, name: str) -> bool:
        return self.run(["docker", "network", "inspect", name], check=False).returncode == 0

    def image_id(self, image: str) -> str:
        return json.loads(self.run(["docker", "image", "inspect", image]).stdout)[0]["Id"]

    def remove_app(self) -> None:
        if APP in self.containers and self.container_exists(APP):
            self.run(["docker", "rm", "-f", APP], timeout=30)
            self.containers.remove(APP)

    def cleanup(self) -> dict[str, bool]:
        for name in list(reversed(self.containers)):
            if self.container_exists(name):
                self.run(["docker", "rm", "-f", name], timeout=30, check=False)
            self.containers.remove(name)
        for name in list(reversed(self.volumes)):
            self.run(["docker", "volume", "rm", name], timeout=30, check=False)
            self.volumes.remove(name)
        if self.network:
            self.run(["docker", "network", "rm", NETWORK], timeout=30, check=False)
            self.network = False
        return {
            "app_absent": not self.container_exists(APP),
            "postgres_absent": not self.container_exists(POSTGRES),
            "redis_absent": not self.container_exists(REDIS),
            "network_absent": not self.network_exists(NETWORK),
            "volumes_absent": not self.volume_exists(PG_VOLUME) and not self.volume_exists(APP_VOLUME),
        }


def core_snapshot(docker: Docker) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for name in CORE_CONTAINERS:
        data = docker.inspect(name)
        result[name] = {
            "id": data["Id"],
            "image_id": data["Image"],
            "started_at": data["State"]["StartedAt"],
            "restart_count": data["RestartCount"],
        }
    return result


def wait_until(test, attempts: int, message: str) -> None:
    for _ in range(attempts):
        if test():
            return
        time.sleep(1)
    raise RuntimeError(message)


def wait_app(docker: Docker, image: str) -> dict[str, Any]:
    started = time.monotonic()

    def ready() -> bool:
        probe = docker.run(
            ["docker", "exec", APP, "wget", "-q", "-T", "3", "-O", "-", "http://127.0.0.1:8080/health"],
            timeout=10,
            check=False,
        )
        return probe.returncode == 0

    wait_until(ready, 90, "rehearsal app did not become healthy")
    data = docker.inspect(APP)
    if data["Image"] != image:
        raise RuntimeError("rehearsal app image identity mismatch")
    return {
        "container_id": data["Id"],
        "image_id": data["Image"],
        "startup_seconds": round(time.monotonic() - started, 3),
        # NetworkSettings.Ports can contain entries such as {"8080/tcp": null}
        # for an EXPOSEd container port even when no host port is published.
        # HostConfig.PortBindings is the authoritative host-publish signal for
        # this rehearsal gate.
        "ports": data["NetworkSettings"].get("Ports", {}),
        "host_port_bindings": data["HostConfig"].get("PortBindings") or {},
        "networks": sorted(data["NetworkSettings"]["Networks"]),
    }


def psql(docker: Docker, sql: str) -> str:
    return docker.run([
        "docker", "exec", POSTGRES, "psql", "-X", "-q", "-U", "rehearsal", "-d", "rehearsal",
        "-At", "-v", "ON_ERROR_STOP=1", "-c", sql,
    ], timeout=60).stdout.strip()


def db_state(docker: Docker) -> dict[str, Any]:
    migrations = psql(docker, "SELECT filename,checksum FROM schema_migrations ORDER BY filename;")
    marker = psql(docker, "SELECT count(*) FROM r1_lifecycle_marker WHERE id=1;")
    return {
        "migration_count": len(migrations.splitlines()) if migrations else 0,
        "migration_sha256": hashlib.sha256((migrations + "\n").encode()).hexdigest(),
        "marker_count": int(marker),
    }


def env_hash(docker: Docker) -> str:
    values = sorted(docker.inspect(APP)["Config"]["Env"])
    return hashlib.sha256(("\n".join(values) + "\n").encode()).hexdigest()


def marker_file(docker: Docker) -> str:
    return docker.run([
        "docker", "exec", APP, "sh", "-c",
        "test -f /app/data/r1-lifecycle-marker && cat /app/data/r1-lifecycle-marker",
    ]).stdout.strip()


def start_app(docker: Docker, image: str, env: dict[str, str]) -> None:
    args = [
        "docker", "run", "-d", "--name", APP, "--network", NETWORK, "--network-alias", "app",
        "--restart", "no", "-v", APP_VOLUME + ":/app/data",
    ]
    for key, value in env.items():
        args.extend(["-e", f"{key}={value}"])
    args.append(image)
    docker.run(args, timeout=60)
    docker.containers.append(APP)


def execute(evidence: Path) -> dict[str, Any]:
    docker = Docker()
    report: dict[str, Any] = {
        "started_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
        "scope": "disposable isolated lifecycle rehearsal",
        "real_model_requests": 0,
        "credentials_printed": False,
        "production_or_existing_staging_mutated": False,
        "steps": [],
        "passed": False,
    }

    try:
        for name in (APP, POSTGRES, REDIS):
            if docker.container_exists(name):
                raise RuntimeError(f"refusing pre-existing rehearsal container: {name}")
        for name in (PG_VOLUME, APP_VOLUME):
            if docker.volume_exists(name):
                raise RuntimeError(f"refusing pre-existing rehearsal volume: {name}")
        if docker.network_exists(NETWORK):
            raise RuntimeError(f"refusing pre-existing rehearsal network: {NETWORK}")
        for image in (CANDIDATE_IMAGE, PREVIOUS_IMAGE, POSTGRES_IMAGE, REDIS_IMAGE):
            if docker.image_id(image) != image:
                raise RuntimeError(f"image identity mismatch: {image}")

        before = core_snapshot(docker)
        synthetic = {
            "db": secrets.token_hex(24), "admin": secrets.token_hex(24),
            "jwt": secrets.token_hex(32), "totp": secrets.token_hex(32),
        }
        env = {
            "AUTO_SETUP": "true", "SERVER_HOST": "0.0.0.0", "SERVER_PORT": "8080", "SERVER_MODE": "release",
            "TZ": "UTC", "DATABASE_HOST": "postgres", "DATABASE_PORT": "5432", "DATABASE_USER": "rehearsal",
            "DATABASE_PASSWORD": synthetic["db"], "DATABASE_DBNAME": "rehearsal", "DATABASE_SSLMODE": "disable",
            "DATABASE_MAX_OPEN_CONNS": "16", "DATABASE_MAX_IDLE_CONNS": "4", "REDIS_HOST": "redis",
            "REDIS_PORT": "6379", "REDIS_POOL_SIZE": "16", "REDIS_MIN_IDLE_CONNS": "2",
            "ADMIN_EMAIL": "rehearsal-admin@example.invalid", "ADMIN_PASSWORD": synthetic["admin"],
            "JWT_SECRET": synthetic["jwt"], "TOTP_ENCRYPTION_KEY": synthetic["totp"],
        }

        docker.run(["docker", "network", "create", "--internal", NETWORK])
        docker.network = True
        for volume in (PG_VOLUME, APP_VOLUME):
            docker.run(["docker", "volume", "create", volume])
            docker.volumes.append(volume)
        docker.run([
            "docker", "run", "-d", "--name", POSTGRES, "--network", NETWORK, "--network-alias", "postgres",
            "--restart", "no", "-v", PG_VOLUME + ":/var/lib/postgresql", "-e", "POSTGRES_USER=rehearsal",
            "-e", "POSTGRES_PASSWORD=" + synthetic["db"], "-e", "POSTGRES_DB=rehearsal", POSTGRES_IMAGE,
        ])
        docker.containers.append(POSTGRES)
        wait_until(
            lambda: docker.run(["docker", "exec", POSTGRES, "pg_isready", "-U", "rehearsal", "-d", "rehearsal"], check=False).returncode == 0,
            60,
            "rehearsal PostgreSQL did not become ready",
        )
        docker.run([
            "docker", "run", "-d", "--name", REDIS, "--network", NETWORK, "--network-alias", "redis",
            "--restart", "no", REDIS_IMAGE,
        ])
        docker.containers.append(REDIS)
        wait_until(lambda: docker.run(["docker", "exec", REDIS, "redis-cli", "ping"], check=False).stdout.strip() == "PONG", 30, "rehearsal Redis did not become ready")
        network_data = json.loads(docker.run(["docker", "network", "inspect", NETWORK]).stdout)[0]
        if network_data["Internal"] is not True:
            raise RuntimeError("rehearsal network is not internal")
        dependency_ids = {"postgres": docker.inspect(POSTGRES)["Id"], "redis": docker.inspect(REDIS)["Id"]}

        start_app(docker, CANDIDATE_IMAGE, env)
        first = wait_app(docker, CANDIDATE_IMAGE)
        psql(docker, "CREATE TABLE IF NOT EXISTS r1_lifecycle_marker(id integer primary key,note text not null); "
                     "INSERT INTO r1_lifecycle_marker(id,note) VALUES (1,$$synthetic-marker$$) ON CONFLICT (id) DO NOTHING;")
        docker.run(["docker", "exec", APP, "sh", "-c", "printf %s synthetic-marker > /app/data/r1-lifecycle-marker"])
        baseline_db = db_state(docker)
        baseline_env = env_hash(docker)
        if marker_file(docker) != "synthetic-marker":
            raise RuntimeError("initial app marker missing")
        first.update({"name": "candidate_initial", "db": baseline_db, "env_sha256": baseline_env, "marker_file": True})
        report["steps"].append(first)

        for step_name, image in (("candidate_cold_recreate", CANDIDATE_IMAGE), ("previous_image", PREVIOUS_IMAGE), ("candidate_restored", CANDIDATE_IMAGE)):
            docker.remove_app()
            start_app(docker, image, env)
            step = wait_app(docker, image)
            dependencies_unchanged = docker.inspect(POSTGRES)["Id"] == dependency_ids["postgres"] and docker.inspect(REDIS)["Id"] == dependency_ids["redis"]
            state = db_state(docker)
            if not dependencies_unchanged or state != baseline_db or marker_file(docker) != "synthetic-marker" or env_hash(docker) != baseline_env:
                raise RuntimeError(f"lifecycle continuity failed at {step_name}")
            step.update({"name": step_name, "db": state, "env_sha256": baseline_env, "marker_file": True, "dependencies_unchanged": True})
            report["steps"].append(step)

        after = core_snapshot(docker)
        report.update({
            "finished_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
            "network_internal": True,
            "published_ports_zero": all(not step["host_port_bindings"] for step in report["steps"]),
            "production_and_existing_staging_unchanged": before == after,
            "rehearsal_migration_count": baseline_db["migration_count"],
            "rehearsal_migration_sha256": baseline_db["migration_sha256"],
            "config_hash_continuity": len({step["env_sha256"] for step in report["steps"]}) == 1,
            "persistence_continuity": all(step["db"] == baseline_db and step["marker_file"] for step in report["steps"]),
        })
        report["passed"] = len(report["steps"]) == 4 and report["published_ports_zero"] and report["production_and_existing_staging_unchanged"] and report["config_hash_continuity"] and report["persistence_continuity"]
    except Exception as exc:  # evidence is intentionally non-sensitive
        report["error_type"] = type(exc).__name__
        report["error"] = str(exc)
    finally:
        cleanup = docker.cleanup()
        report["cleanup"] = cleanup
        report["cleanup_complete"] = all(cleanup.values())
        report["passed"] = bool(report.get("passed")) and report["cleanup_complete"]
        evidence.parent.mkdir(parents=True, exist_ok=True)
        evidence.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        os.chmod(evidence, 0o600)
    return report


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--plan", action="store_true")
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--ack", default="")
    parser.add_argument("--evidence", type=Path, default=DEFAULT_EVIDENCE)
    args = parser.parse_args()
    if not args.execute:
        print(json.dumps(rehearsal_plan(), indent=2))
        return 0
    if args.plan or args.ack != "DISPOSABLE_ONLY":
        parser.error("--execute requires --ack DISPOSABLE_ONLY and cannot be combined with --plan")
    report = execute(args.evidence)
    safe_keys = (
        "started_utc", "finished_utc", "network_internal", "published_ports_zero",
        "production_and_existing_staging_unchanged", "rehearsal_migration_count", "rehearsal_migration_sha256",
        "config_hash_continuity", "persistence_continuity", "cleanup", "cleanup_complete", "passed",
        "real_model_requests", "credentials_printed", "error_type", "error",
    )
    print(json.dumps({key: report.get(key) for key in safe_keys}, indent=2))
    print(f"EVIDENCE_FILE={args.evidence}")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
