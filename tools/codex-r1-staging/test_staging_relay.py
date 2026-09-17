import asyncio
import unittest

from staging_relay import APP, IMAGE, NETWORK, PROJECT, Relay, validate_target


def fixture(ip="172.24.0.4"):
    return [
        {
            "Name": "/" + APP,
            "Image": IMAGE,
            "State": {"Running": True},
            "Config": {
                "Labels": {
                    "com.docker.compose.project": PROJECT,
                    "com.docker.compose.service": "app",
                }
            },
            "NetworkSettings": {"Networks": {NETWORK: {"IPAddress": ip}}},
        }
    ]


class IdentityTests(unittest.TestCase):
    def test_dynamic_ip(self):
        for ip in ("172.24.0.4", "172.24.0.33", "10.77.1.9"):
            self.assertEqual(validate_target(fixture(ip)), ip)

    def test_reject_public_and_special_ips(self):
        for ip in (
            "1.1.1.1",
            "127.0.0.1",
            "0.0.0.0",
            "169.254.1.2",
            "::1",
            "",
            "example.com",
        ):
            with self.subTest(ip=ip), self.assertRaises(ValueError):
                validate_target(fixture(ip))

    def test_reject_wrong_container(self):
        item = fixture()
        item[0]["Name"] = "/sub2api"
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_wrong_image(self):
        item = fixture()
        item[0]["Image"] = "sha256:other"
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_wrong_project(self):
        item = fixture()
        item[0]["Config"]["Labels"]["com.docker.compose.project"] = "production"
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_wrong_service(self):
        item = fixture()
        item[0]["Config"]["Labels"]["com.docker.compose.service"] = "postgres"
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_stopped(self):
        item = fixture()
        item[0]["State"]["Running"] = False
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_extra_network(self):
        item = fixture()
        item[0]["NetworkSettings"]["Networks"]["production"] = {
            "IPAddress": "172.21.0.3"
        }
        with self.assertRaises(ValueError):
            validate_target(item)

    def test_reject_bad_document(self):
        for value in ({}, [], fixture() + fixture()):
            with self.assertRaises(ValueError):
                validate_target(value)


class LocalResolver:
    async def resolve(self, *, force=False):
        return "127.0.0.1"


class RelayTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        async def echo(reader, writer):
            try:
                while data := await reader.read(65536):
                    writer.write(data)
                    await writer.drain()
            finally:
                writer.close()
                await writer.wait_closed()

        self.echo = await asyncio.start_server(echo, "127.0.0.1", 0)
        self.relay = Relay(
            LocalResolver(),
            target_port=self.echo.sockets[0].getsockname()[1],
            idle_timeout=2,
        )
        self.server = await asyncio.start_server(self.relay.handle, "127.0.0.1", 0)
        self.port = self.server.sockets[0].getsockname()[1]

    async def asyncTearDown(self):
        self.server.close()
        await self.server.wait_closed()
        await self.relay.close()
        self.echo.close()
        await self.echo.wait_closed()

    async def exchange(self, payload):
        reader, writer = await asyncio.open_connection("127.0.0.1", self.port)
        writer.write(payload)
        await writer.drain()
        writer.write_eof()
        actual = await asyncio.wait_for(reader.read(), timeout=3)
        self.assertEqual(actual, payload)
        writer.close()
        await writer.wait_closed()

    async def test_binary_transparent_half_close(self):
        await self.exchange(bytes(range(256)) * 4096)

    async def test_header_and_sse_bytes_preserved(self):
        await self.exchange(
            b"GET /fixture HTTP/1.1\r\nsession_id: fixture-only\r\n"
            b"x-test: one\r\nx-test: two\r\n\r\ndata: example\n\n"
        )

    async def test_websocket_frame_bytes_preserved(self):
        await self.exchange(b"\x81\x84\x00\x01\x02\x03abcd\x89\x00")

    async def test_concurrent_connections(self):
        await asyncio.gather(
            *(self.exchange(("fixture-%d" % i).encode() * 100) for i in range(8))
        )

    async def test_graceful_active_shutdown(self):
        reader, writer = await asyncio.open_connection("127.0.0.1", self.port)
        writer.write(b"probe")
        await writer.drain()
        self.assertEqual(await reader.readexactly(5), b"probe")
        await self.relay.close()
        self.assertEqual(await asyncio.wait_for(reader.read(), 1), b"")
        self.assertFalse(self.relay.tasks)
        writer.close()
        await writer.wait_closed()


class ReconnectTests(unittest.IsolatedAsyncioTestCase):
    async def round_trip(self, fail_first):
        received = []

        class Resolver:
            def __init__(self):
                self.calls = []

            async def resolve(self, *, force=False):
                self.calls.append(force)
                return (
                    "127.0.0.2"
                    if fail_first and len(self.calls) == 1
                    else "127.0.0.1"
                )

        resolver = Resolver()

        async def upstream(reader, writer):
            data = await reader.readexactly(12)
            received.append(data)
            writer.write(b"fixture-done")
            await writer.drain()
            writer.close()
            await writer.wait_closed()

        echo = await asyncio.start_server(upstream, "127.0.0.1", 0)
        relay = Relay(
            resolver,
            target_port=echo.sockets[0].getsockname()[1],
            idle_timeout=2,
        )
        server = await asyncio.start_server(relay.handle, "127.0.0.1", 0)
        try:
            reader, writer = await asyncio.open_connection(
                "127.0.0.1", server.sockets[0].getsockname()[1]
            )
            writer.write(b"fixture-once")
            await writer.drain()
            writer.write_eof()
            self.assertEqual(await asyncio.wait_for(reader.read(), 3), b"fixture-done")
            writer.close()
            await writer.wait_closed()
            self.assertEqual(received, [b"fixture-once"])
            self.assertEqual(resolver.calls, [False, True] if fail_first else [False])
        finally:
            server.close()
            await server.wait_closed()
            await relay.close()
            echo.close()
            await echo.wait_closed()

    async def test_refresh_only_before_first_forward(self):
        await self.round_trip(True)

    async def test_no_replay_after_upstream_closed(self):
        await self.round_trip(False)


if __name__ == "__main__":
    unittest.main(verbosity=2)
