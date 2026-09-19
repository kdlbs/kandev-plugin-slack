#!/usr/bin/env python3
"""Check a running task's MCP endpoint on a disposable Kandev instance.

No Slack message is sent: all test arguments are invalid. The endpoint must
belong to a started task agent; prepare-only sessions have no backend stream.
"""
import argparse
import json
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("mcp_url", help="Task agentctl MCP URL, including an SSH forward")
args = parser.parse_args()
headers = {
    "Content-Type": "application/json",
    "Accept": "application/json, text/event-stream",
}


def rpc(number, method, params):
    body = json.dumps({"jsonrpc": "2.0", "id": number, "method": method, "params": params})
    request = urllib.request.Request(args.mcp_url, body.encode(), headers)
    with urllib.request.urlopen(request, timeout=35) as response:
        if response.headers.get("Mcp-Session-Id"):
            headers["Mcp-Session-Id"] = response.headers["Mcp-Session-Id"]
        return json.load(response)


rpc(1, "initialize", {
    "protocolVersion": "2025-03-26",
    "capabilities": {},
    "clientInfo": {"name": "slack-notification-smoke", "version": "1"},
})
tools = rpc(2, "tools/list", {})["result"]["tools"]
name = "kandev_kandev_plugin_slack_notify_user"
tool = next(tool for tool in tools if tool["name"] == name)
assert set(tool["inputSchema"]["required"]) == {"recipient", "message", "idempotency_key"}
print("Discovered:", name)

# A blank message passes the host's length check but fails plugin validation.
# It proves invocation reached the managed plugin without contacting Slack.
base = {"recipient": "U12345678", "message": " ", "idempotency_key": "smoke"}
result = rpc(3, "tools/call", {"name": name, "arguments": base})["result"]
assert result["isError"]
payload = json.loads(result["content"][0]["text"])
assert payload["code"] == "invalid_arguments", payload
assert payload["status"] == "rejected", payload
print("Managed plugin invocation: invalid_arguments (no Slack request)")

for number, override in enumerate([
    {"recipient": "C12345678"},
    {"workspace_id": "forged-workspace"},
], 4):
    result = rpc(number, "tools/call", {
        "name": name, "arguments": {**base, **override},
    })["result"]
    assert result["isError"], result
    assert "invalid arguments" in result["content"][0]["text"], result
print("Host schema rejected channel recipients and workspace overrides")
