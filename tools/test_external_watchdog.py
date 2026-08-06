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
    fingerprint,
    load_state,
    run,
    save_state,
    send_alert_relay,
    send_feishu_app,
    should_notify,
)


class ExternalWatchdogTest(unittest.TestCase):
    def test_workflow_loads_monitor_token_from_server_with_validated_key(self) -> None:
        workflow = (
            pathlib.Path(__file__).parents[1] / ".github" / "workflows" / "watchdog.yml"
        ).read_text(encoding="utf-8")
        self.assertIn("ssh-keygen -y -f ~/.ssh/id_watchdog", workflow)
        self.assertIn("/opt/fbif-wechat-article-dashboard/.env", workflow)
        self.assertIn("PUBLISH_SYNC_SERVICE_TOKEN=", workflow)
        self.assertNotIn("APP_ENV_B64: ${{ secrets.APP_ENV_B64 }}", workflow)

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

    def test_fingerprint_ignores_volatile_age_and_count_values(self) -> None:
        self.assertEqual(
            fingerprint(["feed:poller_stale:181s", "layout:failed_jobs_recent:1"]),
            fingerprint(["feed:poller_stale:301s", "layout:failed_jobs_recent:2"]),
        )

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

    def test_alert_relay_uses_service_token_and_structured_payload(self) -> None:
        with mock.patch("tools.external_watchdog._post_json", return_value={"delivered": True}) as post:
            send_alert_relay(
                "https://layout.example/api/publish/monitor-alert",
                "service-token",
                "critical",
                "外部看门狗异常\nfeed:gap:1",
                2,
            )
        args = post.call_args.args
        self.assertEqual(args[1]["source"], "github-watchdog")
        self.assertEqual(args[1]["status"], "critical")
        self.assertEqual(args[2]["X-Publish-Sync-Token"], "service-token")

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

    def test_issue_only_mode_does_not_duplicate_feishu_alert(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {"PUBLISH_SYNC_SERVICE_TOKEN": "service-token"},
            clear=True,
        ), mock.patch(
            "tools.external_watchdog.fetch_json",
            side_effect=[
                {"ready": True},
                {"ready": False, "issues": ["poller_stale"]},
                {"ready": True},
            ],
        ), mock.patch("tools.external_watchdog.send_alert_relay") as relay:
            state_path = pathlib.Path(directory) / "state.json"
            result = run(
                argparse.Namespace(
                    layout_url="https://layout.example/monitoring",
                    feed_url="https://feed.example/monitoring",
                    official_url="http://127.0.0.1:13002/monitoring",
                    state=str(state_path),
                    timeout=2.0,
                    issue_only=True,
                )
            )
            state = load_state(state_path)
        self.assertEqual(result, 1)
        relay.assert_not_called()
        self.assertEqual(state["notification_channel"], "github-issue")
        self.assertTrue(state["notified"])


if __name__ == "__main__":
    unittest.main()
