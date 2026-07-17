import argparse
import json
import os
import pathlib
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from unittest import mock

from tools.external_watchdog import (
    evaluate,
    feishu_sign,
    fetch_json,
    load_state,
    run,
    save_state,
    send_feishu_app,
    should_notify,
)


class ExternalWatchdogTest(unittest.TestCase):
    def test_evaluate_uses_only_ready_and_issue_codes(self) -> None:
        self.assertEqual(evaluate("feed", {"ready": True, "issues": ["ignored"]}), [])
        self.assertEqual(
            evaluate("feed", {"ready": False, "issues": ["sync_outbox_stale:901s"]}),
            ["feed:sync_outbox_stale:901s"],
        )

    def test_transition_notifies_once_and_again_on_recovery(self) -> None:
        self.assertTrue(should_notify({}, False, "a"))
        self.assertFalse(should_notify({"healthy": False, "fingerprint": "a", "notified": True}, False, "a"))
        self.assertTrue(should_notify({"healthy": False, "fingerprint": "a", "notified": True}, False, "b"))
        self.assertTrue(should_notify({"healthy": False, "fingerprint": "a", "notified": True}, True, "c"))

    def test_state_round_trip_is_atomic_and_signature_is_stable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "state.json"
            save_state(path, {"healthy": False, "fingerprint": "abc"})
            self.assertEqual(load_state(path)["fingerprint"], "abc")
        self.assertEqual(feishu_sign(1, "secret"), "6o+SjynWLFd+QtSzfgy9uvrayMJ+/S8z4k5MmO7xW68=")

    def test_monitor_token_is_not_forwarded_across_redirects(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            target_calls = 0

            def do_GET(self) -> None:
                if self.path == "/redirect":
                    self.send_response(307)
                    self.send_header("Location", "/target")
                    self.end_headers()
                    return
                Handler.target_calls += 1
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"ready":true}')

            def log_message(self, _format: str, *_args: object) -> None:
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = f"http://127.0.0.1:{server.server_port}/redirect"
            with self.assertRaisesRegex(RuntimeError, "HTTP_307"):
                fetch_json("layout", url, {"X-Publish-Sync-Token": "secret"}, 2)
            self.assertEqual(Handler.target_calls, 0)
        finally:
            server.shutdown()
            thread.join(timeout=2)
            server.server_close()

    def test_feishu_app_fallback_sends_to_configured_chat(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            message_payload: dict[str, str] = {}
            authorization = ""

            def do_POST(self) -> None:
                length = int(self.headers.get("Content-Length", "0"))
                payload = self.rfile.read(length)
                if self.path == "/token":
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(b'{"code":0,"tenant_access_token":"tenant-token"}')
                    return
                if self.path == "/messages?receive_id_type=chat_id":
                    Handler.authorization = self.headers.get("Authorization", "")
                    Handler.message_payload = json.loads(payload)
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(b'{"code":0,"msg":"ok"}')
                    return
                self.send_response(404)
                self.end_headers()

            def log_message(self, _format: str, *_args: object) -> None:
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base_url = f"http://127.0.0.1:{server.server_port}"
        try:
            send_feishu_app(
                "cli_test",
                "app-secret",
                "oc_review",
                "测试告警",
                2,
                token_url=f"{base_url}/token",
                message_url=f"{base_url}/messages",
            )
            self.assertEqual(Handler.authorization, "Bearer tenant-token")
            self.assertEqual(Handler.message_payload["receive_id"], "oc_review")
            self.assertEqual(Handler.message_payload["msg_type"], "text")
        finally:
            server.shutdown()
            thread.join(timeout=2)
            server.server_close()

    def test_all_monitoring_requests_use_the_least_privilege_service_token(self) -> None:
        class Handler(BaseHTTPRequestHandler):
            headers_by_path: dict[str, tuple[str, str]] = {}

            def do_GET(self) -> None:
                Handler.headers_by_path[self.path] = (
                    self.headers.get("X-Publish-Sync-Token", ""),
                    self.headers.get("X-API-Key", ""),
                )
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(b'{"ready":true}')

            def log_message(self, _format: str, *_args: object) -> None:
                return

        server = HTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base_url = f"http://127.0.0.1:{server.server_port}"
        try:
            with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
                os.environ,
                {"PUBLISH_SYNC_SERVICE_TOKEN": "service-token"},
                clear=True,
            ):
                result = run(
                    argparse.Namespace(
                        layout_url=f"{base_url}/layout",
                        feed_url=f"{base_url}/feed",
                        official_url=f"{base_url}/official",
                        state=str(pathlib.Path(directory) / "state.json"),
                        timeout=2.0,
                    )
                )
            self.assertEqual(result, 0)
            self.assertEqual(
                Handler.headers_by_path,
                {
                    "/layout": ("service-token", ""),
                    "/feed": ("service-token", ""),
                    "/official": ("service-token", ""),
                },
            )
        finally:
            server.shutdown()
            thread.join(timeout=2)
            server.server_close()


if __name__ == "__main__":
    unittest.main()
