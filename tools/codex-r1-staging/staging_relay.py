#!/usr/bin/env python3
"""Loopback-only byte-transparent relay for the frozen synthetic staging app.

No credentials are read. Docker operations are read-only and name one container.
A connection is retried only before any application bytes are forwarded.
"""
from __future__ import annotations

import argparse
import asyncio
import contextlib
import ipaddress
import json
import logging
import signal
import time

APP = "sub2api-r1-staging-app-1"
PROJECT = "sub2api-r1-staging"
NETWORK = "sub2api-r1-staging-net"
IMAGE = "sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85"
PORT = 18081
INSPECT_FORMAT = "[{\"Name\":{{json .Name}},\"Image\":{{json .Image}},\"State\":{\"Running\":{{json .State.Running}}},\"Config\":{\"Labels\":{\"com.docker.compose.project\":{{json (index .Config.Labels \"com.docker.compose.project\")}},\"com.docker.compose.service\":{{json (index .Config.Labels \"com.docker.compose.service\")}}}},\"NetworkSettings\":{\"Networks\":{{json .NetworkSettings.Networks}}}}]"
LOG = logging.getLogger("staging_relay")


def validate_target(document: object) -> str:
    if not isinstance(document, list) or len(document) != 1:
        raise ValueError("unexpected_inspect_document")
    item = document[0]
    if item.get("Name") != "/" + APP or item.get("Image") != IMAGE:
        raise ValueError("unexpected_app_identity")
    if not item.get("State", {}).get("Running"):
        raise ValueError("app_not_running")
    labels = item.get("Config", {}).get("Labels", {})
    if labels.get("com.docker.compose.project") != PROJECT:
        raise ValueError("unexpected_project")
    if labels.get("com.docker.compose.service") != "app":
        raise ValueError("unexpected_service")
    networks = item.get("NetworkSettings", {}).get("Networks", {})
    if set(networks) != {NETWORK}:
        raise ValueError("unexpected_networks")
    address = ipaddress.ip_address(networks[NETWORK].get("IPAddress", ""))
    if (
        address.version != 4
        or not address.is_private
        or address.is_loopback
        or address.is_unspecified
        or address.is_link_local
    ):
        raise ValueError("unexpected_app_address")
    return str(address)


class DockerResolver:
    def __init__(self) -> None:
        self._target: str | None = None
        self._until = 0.0
        self._lock = asyncio.Lock()

    async def resolve(self, *, force: bool = False) -> str:
        async with self._lock:
            if not force and self._target and time.monotonic() < self._until:
                return self._target
            proc = await asyncio.create_subprocess_exec(
                "/usr/bin/docker",
                "inspect",
                APP,
                "--format",
                INSPECT_FORMAT,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            try:
                stdout, _ = await asyncio.wait_for(proc.communicate(), timeout=5)
            except BaseException:
                with contextlib.suppress(ProcessLookupError):
                    proc.kill()
                await proc.wait()
                raise
            if proc.returncode:
                self._target = None
                self._until = 0.0
                raise RuntimeError("staging_app_inspect_failed")
            address = validate_target(json.loads(stdout))
            self._target, self._until = address, time.monotonic() + 0.5
            return address


class Relay:
    def __init__(
        self,
        resolver: object,
        *,
        target_port: int = 8080,
        idle_timeout: float = 600,
        max_connections: int = 128,
    ) -> None:
        self.resolver = resolver
        self.target_port = target_port
        self.idle_timeout = idle_timeout
        self.max_connections = max_connections
        self.active: set[asyncio.StreamWriter] = set()
        self.tasks: set[asyncio.Task] = set()

    async def _pump(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        while True:
            data = await asyncio.wait_for(reader.read(65536), self.idle_timeout)
            if not data:
                if writer.can_write_eof():
                    with contextlib.suppress(OSError, RuntimeError):
                        writer.write_eof()
                        await writer.drain()
                return
            writer.write(data)
            await writer.drain()

    async def handle(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        if len(self.active) >= self.max_connections:
            writer.close()
            with contextlib.suppress(OSError):
                await writer.wait_closed()
            return
        self.active.add(writer)
        current = asyncio.current_task()
        if current:
            self.tasks.add(current)
        upstream = None
        pumps: list[asyncio.Task] = []
        try:
            for attempt in range(2):
                address = await self.resolver.resolve(force=bool(attempt))
                try:
                    up_reader, upstream = await asyncio.wait_for(
                        asyncio.open_connection(address, self.target_port), timeout=5
                    )
                    break
                except (OSError, TimeoutError):
                    if attempt:
                        raise
            pumps = [
                asyncio.create_task(self._pump(reader, upstream)),
                asyncio.create_task(self._pump(up_reader, writer)),
            ]
            await asyncio.gather(*pumps)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            LOG.warning("staging_relay_connection_failed kind=%s", type(exc).__name__)
        finally:
            for pump in pumps:
                if not pump.done():
                    pump.cancel()
            if pumps:
                await asyncio.gather(*pumps, return_exceptions=True)
            for stream in (upstream, writer):
                if stream is not None:
                    stream.close()
                    with contextlib.suppress(OSError, RuntimeError):
                        await stream.wait_closed()
            self.active.discard(writer)
            if current:
                self.tasks.discard(current)

    async def close(self) -> None:
        pending = list(self.tasks)
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*pending, return_exceptions=True)


async def main(check_only: bool) -> None:
    resolver = DockerResolver()
    if check_only:
        print(
            json.dumps(
                {
                    "target_container": APP,
                    "address": await resolver.resolve(force=True),
                    "image_checked": True,
                }
            )
        )
        return
    relay = Relay(resolver)
    server = await asyncio.start_server(relay.handle, "127.0.0.1", PORT, limit=131072)
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for signum in (signal.SIGTERM, signal.SIGINT):
        loop.add_signal_handler(signum, stop.set)
    LOG.info("staging_relay_ready bind=127.0.0.1:%d target=%s", PORT, APP)
    async with server:
        await stop.wait()
    await relay.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check-target", action="store_true")
    arguments = parser.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    asyncio.run(main(arguments.check_target))
