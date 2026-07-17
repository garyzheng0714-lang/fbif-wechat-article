#!/usr/bin/env python3
"""外部探针：从 GitHub runner 检查三套服务，并对状态变化发飞书通知。"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import pathlib
import re
import tempfile
import time
import urllib.error
import urllib.request
from typing import Any


class _NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: ANN001
        return None


NO_REDIRECT_OPENER = urllib.request.build_opener(_NoRedirectHandler())
FEISHU_TOKEN_URL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
FEISHU_MESSAGE_URL = "https://open.feishu.cn/open-apis/im/v1/messages"
DEFAULT_ALERT_RELAY_URL = "https://fbifmp-layout.foodtalks.cn/api/publish/monitor-alert"
VOLATILE_NUMBER = re.compile(r"\d+")


def fetch_json(name: str, url: str, headers: dict[str, str], timeout: float) -> dict[str, Any]:
    request = urllib.request.Request(url, headers=headers, method="GET")
    try:
        with NO_REDIRECT_OPENER.open(request, timeout=timeout) as response:
            raw = response.read(1 << 20)
    except urllib.error.HTTPError as error:
        detail = error.read(4096).decode("utf-8", "replace").strip()
        raise RuntimeError(f"{name}:HTTP_{error.code}:{detail[:300]}") from error
    except Exception as error:  # 网络、DNS、SSH 隧道失败统一成为外部可见故障
        raise RuntimeError(f"{name}:unreachable:{type(error).__name__}") from error
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as error:
        raise RuntimeError(f"{name}:invalid_json") from error
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name}:invalid_payload")
    return payload


def evaluate(name: str, payload: dict[str, Any]) -> list[str]:
    if payload.get("ready") is True:
        return []
    issues = payload.get("issues")
    if not isinstance(issues, list) or not issues:
        return [f"{name}:not_ready"]
    return [f"{name}:{str(issue)[:200]}" for issue in issues]


def feishu_sign(timestamp: int, secret: str) -> str:
    key = f"{timestamp}\n{secret}".encode()
    return base64.b64encode(hmac.new(key, digestmod=hashlib.sha256).digest()).decode()


def send_feishu(webhook_url: str, secret: str, message: str, timeout: float) -> None:
    if not webhook_url.startswith("https://"):
        raise RuntimeError("Feishu webhook is not configured with HTTPS")
    payload: dict[str, Any] = {
        "msg_type": "text",
        "content": {"text": message[:15000]},
    }
    if secret:
        timestamp = int(time.time())
        payload["timestamp"] = str(timestamp)
        payload["sign"] = feishu_sign(timestamp, secret)
    body = json.dumps(payload, ensure_ascii=False).encode()
    request = urllib.request.Request(
        webhook_url,
        data=body,
        headers={"Content-Type": "application/json; charset=utf-8"},
        method="POST",
    )
    with NO_REDIRECT_OPENER.open(request, timeout=timeout) as response:
        raw = response.read(8192)
        if response.status < 200 or response.status >= 300:
            raise RuntimeError(f"Feishu webhook HTTP {response.status}")
    if raw:
        result = json.loads(raw)
        if isinstance(result, dict) and int(result.get("code", 0)) != 0:
            raise RuntimeError(f"Feishu webhook code {result.get('code')}")


def _post_json(url: str, payload: dict[str, Any], headers: dict[str, str], timeout: float, name: str) -> dict[str, Any]:
    body = json.dumps(payload, ensure_ascii=False).encode()
    request = urllib.request.Request(
        url,
        data=body,
        headers={"Content-Type": "application/json; charset=utf-8", **headers},
        method="POST",
    )
    try:
        with NO_REDIRECT_OPENER.open(request, timeout=timeout) as response:
            raw = response.read(8192)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f"{name} HTTP {error.code}") from error
    try:
        result = json.loads(raw)
    except json.JSONDecodeError as error:
        raise RuntimeError(f"{name} invalid JSON") from error
    if not isinstance(result, dict):
        raise RuntimeError(f"{name} invalid payload")
    return result


def send_feishu_app(
    app_id: str,
    app_secret: str,
    chat_id: str,
    message: str,
    timeout: float,
    token_url: str = FEISHU_TOKEN_URL,
    message_url: str = FEISHU_MESSAGE_URL,
) -> None:
    if not app_id or not app_secret or not chat_id:
        raise RuntimeError("Feishu app reporter is not configured")
    token_result = _post_json(
        token_url,
        {"app_id": app_id, "app_secret": app_secret},
        {},
        timeout,
        "Feishu app token",
    )
    token = str(token_result.get("tenant_access_token", ""))
    if int(token_result.get("code", 0)) != 0 or not token:
        raise RuntimeError(f"Feishu app token code {token_result.get('code')}")
    separator = "&" if "?" in message_url else "?"
    message_result = _post_json(
        f"{message_url}{separator}receive_id_type=chat_id",
        {
            "receive_id": chat_id,
            "msg_type": "text",
            "content": json.dumps({"text": message[:15000]}, ensure_ascii=False),
        },
        {"Authorization": f"Bearer {token}"},
        timeout,
        "Feishu app report",
    )
    if int(message_result.get("code", 0)) != 0:
        raise RuntimeError(f"Feishu app report code {message_result.get('code')}")


def send_alert_relay(relay_url: str, service_token: str, status: str, message: str, timeout: float) -> None:
    if not relay_url.startswith("https://") or not service_token:
        raise RuntimeError("authenticated alert relay is not configured")
    lines = [" ".join(line.split()) for line in message.splitlines() if line.strip()]
    result = _post_json(
        relay_url,
        {
            "source": "github-watchdog",
            "status": status,
            "summary": (lines[0] if lines else "GitHub 外部看门狗状态更新")[:500],
            "details": [line[:300] for line in lines[1:21]],
        },
        {"X-Publish-Sync-Token": service_token},
        timeout,
        "alert relay",
    )
    if result.get("delivered") is not True:
        raise RuntimeError("alert relay did not confirm delivery")


def load_state(path: pathlib.Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
        return value if isinstance(value, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def save_state(path: pathlib.Path, state: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as handle:
        json.dump(state, handle, ensure_ascii=False, sort_keys=True)
        handle.write("\n")
        temporary = pathlib.Path(handle.name)
    temporary.replace(path)


def fingerprint(issues: list[str]) -> str:
    stable = [VOLATILE_NUMBER.sub("#", issue) for issue in sorted(issues)]
    encoded = json.dumps(stable, ensure_ascii=False, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def should_notify(previous: dict[str, Any], healthy: bool, current_fingerprint: str) -> bool:
    if not previous:
        return not healthy
    if bool(previous.get("healthy")) != healthy:
        return True
    if not healthy and previous.get("fingerprint") != current_fingerprint:
        return True
    return not healthy and previous.get("notified") is not True


def write_github_output(healthy: bool, issue_fingerprint: str) -> None:
    target = os.getenv("GITHUB_OUTPUT", "").strip()
    if not target:
        return
    with open(target, "a", encoding="utf-8") as handle:
        handle.write(f"healthy={'true' if healthy else 'false'}\n")
        handle.write(f"fingerprint={issue_fingerprint}\n")


def run(args: argparse.Namespace) -> int:
    service_token = os.getenv("PUBLISH_SYNC_SERVICE_TOKEN", "").strip()
    issue_only = bool(getattr(args, "issue_only", False))
    checks = [
        ("layout", args.layout_url, {"X-Publish-Sync-Token": service_token}),
        ("feed", args.feed_url, {"X-Publish-Sync-Token": service_token}),
        ("official", args.official_url, {"X-Publish-Sync-Token": service_token}),
    ]
    issues: list[str] = []
    if not service_token:
        issues.append("watchdog:PUBLISH_SYNC_SERVICE_TOKEN_missing")
    for name, url, headers in checks:
        try:
            issues.extend(evaluate(name, fetch_json(name, url, headers, args.timeout)))
        except RuntimeError as error:
            issues.append(str(error))
    issues = sorted(set(issues))
    healthy = not issues
    current_fingerprint = fingerprint(issues)
    state_path = pathlib.Path(args.state)
    previous = load_state(state_path)
    notify = should_notify(previous, healthy, current_fingerprint)
    notified = not notify
    if notify:
        if issue_only:
            notified = True
        else:
            if healthy:
                message = "[FBIF 同步链路外部看门狗] 已恢复\n三套服务与持久化队列均恢复正常。"
            else:
                message = "[FBIF 同步链路外部看门狗] 异常\n" + "\n".join(f"- {issue}" for issue in issues)
            try:
                webhook_url = os.getenv("OFFICIAL_FEISHU_WEBHOOK_URL", "").strip()
                if webhook_url:
                    send_feishu(
                        webhook_url,
                        os.getenv("OFFICIAL_FEISHU_WEBHOOK_SECRET", "").strip(),
                        message,
                        args.timeout,
                    )
                elif all(
                    [
                        os.getenv("FEISHU_APP_ID", "").strip(),
                        os.getenv("FEISHU_APP_SECRET", "").strip(),
                        os.getenv("OFFICIAL_FEISHU_CHAT_ID", "").strip(),
                    ]
                ):
                    send_feishu_app(
                        os.getenv("FEISHU_APP_ID", "").strip(),
                        os.getenv("FEISHU_APP_SECRET", "").strip(),
                        os.getenv("OFFICIAL_FEISHU_CHAT_ID", "").strip(),
                        message,
                        args.timeout,
                    )
                else:
                    send_alert_relay(
                        os.getenv("OFFICIAL_ALERT_RELAY_URL", DEFAULT_ALERT_RELAY_URL).strip(),
                        service_token,
                        "recovery" if healthy else "critical",
                        message,
                        args.timeout,
                    )
                notified = True
            except Exception as error:
                issues.append(f"watchdog:feishu_notification_failed:{type(error).__name__}")
                healthy = False
                notified = False
                current_fingerprint = fingerprint(issues)
    save_state(
        state_path,
        {
            "checked_at": int(time.time()),
            "fingerprint": current_fingerprint,
            "healthy": healthy,
            "issues": issues,
            "notified": notified,
            "notification_channel": "github-issue" if issue_only else "feishu",
        },
    )
    write_github_output(healthy, current_fingerprint)
    if healthy:
        print("external watchdog: healthy")
        return 0
    print("external watchdog: unhealthy: " + "; ".join(issues))
    return 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--layout-url",
        default=os.getenv("LAYOUT_MONITOR_URL", "https://fbifmp-layout.foodtalks.cn/api/publish/monitoring"),
    )
    parser.add_argument(
        "--feed-url",
        default=os.getenv("FEED_MONITOR_URL", "https://feed.foodtalks.cn/health/sync"),
    )
    parser.add_argument(
        "--official-url",
        default=os.getenv("OFFICIAL_MONITOR_URL", "http://127.0.0.1:13002/api/wechat/official/monitoring"),
    )
    parser.add_argument("--state", default=".watchdog-state.json")
    parser.add_argument("--timeout", type=float, default=12.0)
    parser.add_argument("--issue-only", action="store_true")
    return parser.parse_args()


if __name__ == "__main__":
    raise SystemExit(run(parse_args()))
