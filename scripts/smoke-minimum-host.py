#!/usr/bin/env python3
"""Install a Slack archive on an isolated released Linux host.

Checks package admission, Host gRPC state/config/webhook calls, bundle serving,
config restart, and disable/re-enable. It does not contact Slack or start a task
agent; notification MCP routing and browser rendering require separate checks.
Uses only a deliberately incomplete synthetic token, rejected before Slack I/O.
"""
import argparse
import json
import os
import re
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--host", required=True, help="Released Kandev executable")
parser.add_argument("--package", required=True, type=Path)
args = parser.parse_args()
host = shutil.which(args.host)
if not host:
    parser.error("host executable not found: " + args.host)
host = str(Path(host).resolve())
manifest = Path("manifest.yaml").read_text()
plugin_version = re.search(r'^version: "?([0-9]+\.[0-9]+\.[0-9]+)"?$', manifest, re.MULTILINE).group(1)
archive = args.package.resolve()
if not archive.is_file():
    parser.error(f"package not found: {archive}; run make verify-package-host first")
if not Path("/proc/self/stat").is_file():
    parser.error("this smoke requires Linux /proc for subprocess restart evidence")
assert subprocess.check_output([host, "--version"], text=True).strip() == "v0.99.0"
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
base = f"http://127.0.0.1:{port}"
plugin = "/api/plugins/kandev-plugin-slack"


def request(path, method="GET", data=None, headers=None, expected=200):
    if isinstance(data, dict):
        data = json.dumps(data).encode()
        headers = {"Content-Type": "application/json", **(headers or {})}
    req = urllib.request.Request(base + path, data=data, headers=headers or {}, method=method)
    try:
        response = urllib.request.urlopen(req, timeout=20)
    except urllib.error.HTTPError as err:
        response = err
    with response:
        body = response.read()
        assert response.status == expected, (path, response.status, body.decode())
        return json.loads(body) if "json" in response.headers.get("Content-Type", "") else body


def wait_for(check, seconds=180):
    deadline = time.monotonic() + seconds
    last = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except (OSError, AssertionError) as err:
            last = err
        time.sleep(0.25)
    raise AssertionError(f"host check timed out: {last}")


def plugin_process_identity(executable):
    """Require one live installed executable and include its kernel start time."""
    identities = []
    for link in Path("/proc").glob("[0-9]*/exe"):
        try:
            if link.resolve(strict=True) == executable:
                fields = (link.parent / "stat").read_text().rsplit(")", 1)[1].split()
                identities.append((int(link.parent.name), fields[19]))
        except (OSError, ValueError):
            continue
    return identities[0] if len(identities) == 1 else None


def persisted_error():
    status = request(plugin + "/webhooks/status")
    if status.get("checkedAt") and "app-level token" in status.get("error", ""):
        return status
    return None


with tempfile.TemporaryDirectory(prefix="slack-minimum-host-") as tmp:
    # Do not inherit a developer instance's config, feature overrides, or data.
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(("KANDEV_", "AGENTCTL_"))}
    env.update(KANDEV_HOME_DIR=tmp, KANDEV_DATABASE_PATH=str(Path(tmp) / "data/kandev.db"),
               KANDEV_FEATURES_AUTH="false", KANDEV_SERVER_HOST="127.0.0.1",
               KANDEV_HEALTH_TIMEOUT_MS="180000")
    log_path = Path(tmp) / "host.log"
    with log_path.open("w+") as log:
        process = subprocess.Popen([host, "run", "--headless", "--port", str(port)],
                                   cwd=tmp, env=env, stdout=log, stderr=log,
                                   start_new_session=True)
        try:
            health = wait_for(lambda: request("/health"))
            assert health["version"] == "v0.99.0", health
            wait_for(lambda: isinstance(request("/api/plugins"), dict))
            print("Released minimum host: v0.99.0, isolated home/database", flush=True)
            boundary = "slack-sdk-migration-upload"
            body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"package\"; "
                    f"filename=\"{archive.name}\"\r\nContent-Type: application/gzip\r\n\r\n").encode()
            body += archive.read_bytes() + f"\r\n--{boundary}--\r\n".encode()
            installed = request("/api/plugins/install", "POST", body,
                                {"Content-Type": "multipart/form-data; boundary=" + boundary}, 201)
            assert not installed.get("warning"), installed
            wait_for(lambda: request(plugin)["status"] == "active")
            record = request(plugin)
            assert record["api_version"] == 1 and record["min_kandev_version"] == "0.99.0", record
            assert record["version"] == plugin_version, record
            print("Package admitted; managed API v1 subprocess active", flush=True)
            status = wait_for(lambda: request(plugin + "/webhooks/status"))
            assert status["configured"] is False, status
            request(plugin + "/webhooks/test", "POST", {}, expected=400)
            assert request(plugin + "/webhooks/scan", "POST", {}, expected=202)["scheduled"]
            request(plugin + "/webhooks/test", expected=405)
            request(plugin + "/webhooks/not-declared", expected=404)
            bundle = request(plugin + "/bundle")
            assert b"registerComponent" in bundle and b"plugin-settings" in bundle
            assert request(plugin + "/config")["config"] == {}, "unexpected initial credentials"
            print("All declared webhooks, method/route rejection, UI bundle served", flush=True)
            executable = (Path(record["install_path"]) / record["runtime"]["executables"]["linux-amd64"]).resolve()
            original_process = wait_for(lambda: plugin_process_identity(executable))
            # A missing app token makes validation fail before any Slack I/O.
            # recordUnconfigured must write a non-default error and checkedAt.
            config = {"utility_agent": "migration-smoke", "command_prefix": "!sdk-smoke",
                      "bot_token": "deliberately-invalid-smoke-token"}
            request(plugin, "PATCH", {"config": config})
            wait_for(lambda: request(plugin)["status"] == "active")
            def replacement_process():
                identity = plugin_process_identity(executable)
                return identity if identity and identity != original_process else None

            restarted_process = wait_for(replacement_process)
            saved_state = wait_for(persisted_error)
            assert request(plugin + "/config")["config"]["command_prefix"] == "!sdk-smoke"
            print(f"Config PATCH replaced subprocess {original_process} with {restarted_process}", flush=True)
            request(plugin + "/disable", "POST", {})
            wait_for(lambda: request(plugin)["status"] == "disabled")
            wait_for(lambda: plugin_process_identity(executable) is None)
            assert request(plugin + "/config")["config"]["command_prefix"] == "!sdk-smoke"
            request(plugin + "/enable", "POST", {})
            wait_for(lambda: request(plugin)["status"] == "active")
            enabled_process = wait_for(lambda: plugin_process_identity(executable))
            assert enabled_process != restarted_process, (enabled_process, restarted_process)
            restored_state = wait_for(persisted_error)
            assert restored_state["checkedAt"] == saved_state["checkedAt"], (restored_state, saved_state)
            assert request(plugin + "/config")["config"]["command_prefix"] == "!sdk-smoke"
            print("Host state checkedAt survives subprocess replacement; config and webhooks restored", flush=True)
            request(plugin, "DELETE")
            request(plugin, expected=404)
            print("Disposable plugin uninstalled; no valid Slack credentials or external messages used", flush=True)
        except BaseException:
            log.flush()
            print(log_path.read_text()[-8000:])
            for backend_log in (Path(tmp) / "logs").glob("*.log"):
                print(backend_log.read_text()[-8000:])
            raise
        finally:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
