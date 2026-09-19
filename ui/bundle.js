// kandev-plugin-slack UI bundle (no-build ES module, see PLUGIN-API.md).
//
// Renders one card in the "plugin-settings" slot — inline on
// Settings > Plugins > Slack, directly above the schema-driven credential
// form. Credentials themselves are edited in that form; this card exists to
// answer the questions the form cannot: are the pasted credentials actually
// valid, which auth mode did they select, and is anything being triaged.
//
// Data flows: card -> host.api.fetch("webhooks/status") -> kandev relays over
// gRPC HandleWebhook -> plugin backend -> JSON back. The slot is owner-scoped,
// so this never renders on another plugin's settings page.

const PLUGIN_ID = "kandev-plugin-slack";

// Slack context the operator needs while filling in the form above this card.
const MODE_HELP = {
  app: "Events arrive over Slack's Socket Mode WebSocket — nothing is polled, and no public URL is needed. Invite the bot to a channel, then mention @Kandev or run /kandev.",
  session:
    "Fallback for workspaces that forbid app installs. It uses your browser session, which Slack does not support: it stops working when you sign out, the d cookie rotates regularly, and it polls instead of receiving events.",
};

function makeStatusCard(host) {
  const { React, jsx: h, ui } = host;
  const { Card, CardHeader, CardTitle, CardContent, Badge, Button, Alert, AlertDescription, Separator, Skeleton } = ui;

  // useStatus owns every read of the backend. `reload` is stable so the two
  // action buttons can refresh without re-creating their own callbacks.
  function useStatus() {
    const [state, setState] = React.useState({ loading: true, error: null, data: null });

    const reload = React.useCallback(() => {
      return host.api
        .fetch("webhooks/status")
        .then(async (res) => {
          const body = await res.json();
          if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
          setState({ loading: false, error: null, data: body });
        })
        .catch((err) => setState({ loading: false, error: String(err.message || err), data: null }));
    }, []);

    React.useEffect(() => {
      reload();
      // The backend polls on its own cadence; re-read periodically so the card
      // reflects a probe or a triage that happened while it was open.
      const timer = setInterval(reload, 15000);
      return () => clearInterval(timer);
    }, [reload]);

    return { ...state, reload };
  }

  function ConnectionBadge({ data }) {
    if (!data || !data.configured) {
      return h(Badge, { variant: "outline" }, "Not configured");
    }
    if (data.ok) {
      // In Socket Mode "connected" means the WebSocket is actually open, so
      // say so rather than implying only that a credential validated.
      return h(Badge, { variant: "default" }, data.realtime ? "Listening" : "Connected");
    }
    return h(Badge, { variant: "destructive" }, "Not connected");
  }

  function Identity({ data }) {
    if (!data || !data.ok) return null;
    const parts = [data.teamName, data.userName ? "@" + data.userName : null].filter(Boolean);
    if (parts.length === 0) return null;
    return h("span", { style: { color: "var(--muted-foreground)" } }, parts.join(" · "));
  }

  // MetaRow renders the settings the operator cannot see in the form above
  // (defaults that were never typed, and the derived auth mode).
  function MetaRow({ data }) {
    if (!data || !data.configured) return null;
    const bits = [];
    if (data.modeLabel) bits.push(data.modeLabel);
    if (data.realtime) {
      bits.push("Trigger: @Kandev or /kandev");
      bits.push("Real-time");
    } else {
      if (data.commandPrefix) bits.push("Trigger: " + data.commandPrefix);
      if (data.pollIntervalSeconds) bits.push("Polls every " + data.pollIntervalSeconds + "s");
      if (Array.isArray(data.channels) && data.channels.length > 0) {
        bits.push(data.channels.length === 1 ? "1 channel" : data.channels.length + " channels");
      }
    }
    if (data.startAgent) bits.push("Starts an agent");
    return h(
      "div",
      { style: { fontSize: "0.8125rem", color: "var(--muted-foreground)" } },
      bits.join(" · "),
    );
  }

  function ScopeHint({ data }) {
    const help = data && data.mode ? MODE_HELP[data.mode] : null;
    if (!help) return null;
    return h("div", { style: { fontSize: "0.8125rem", color: "var(--muted-foreground)" } }, help);
  }

  // Existing installations need this guidance even when Socket Mode is healthy.
  function NotificationHint() {
    return h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "0.375rem", fontSize: "0.8125rem" } },
      h("div", { style: { fontWeight: 500 } }, "Task notifications"),
      h(
        "div",
        { style: { color: "var(--muted-foreground)" } },
        "Task agents can send direct messages through your existing bot token. Browser-session fallback does not support task notifications.",
      ),
      h(
        "div",
        { style: { color: "var(--muted-foreground)" } },
        "In the Slack app, open OAuth & Permissions and add the bot scopes chat:write and im:write. After adding a scope, reinstall the Slack app into your workspace to grant it.",
      ),
    );
  }

  // Setup is the one thing the schema-driven form cannot explain: where the
  // two tokens come from. Shown only until the app path is working.
  function SetupHint({ data }) {
    if (data && data.realtime && data.ok) return null;
    if (data && data.mode === "session") return null;
    return h(
      "div",
      { style: { fontSize: "0.8125rem", color: "var(--muted-foreground)" } },
      "Create the Slack app from ",
      h(
        "a",
        { href: "https://api.slack.com/apps", target: "_blank", rel: "noreferrer noopener" },
        "api.slack.com/apps",
      ),
      " → Create New App → From a manifest, paste the plugin's slack-app-manifest.yaml, then copy the app-level token (Basic Information) and the bot token (OAuth & Permissions) into the fields below.",
    );
  }

  function Timestamps({ data }) {
    if (!data || !data.configured) return null;
    const bits = [];
    if (data.checkedAt) bits.push((data.realtime ? "Connected " : "Checked ") + formatWhen(data.checkedAt));
    if (data.scannedAt && !data.realtime) bits.push("Scanned " + formatWhen(data.scannedAt));
    bits.push(
      data.triaged === 1 ? "1 task created" : (data.triaged || 0) + " tasks created",
    );
    return h(
      "div",
      { style: { fontSize: "0.8125rem", color: "var(--muted-foreground)" } },
      bits.join(" · "),
    );
  }

  function Activity({ data }) {
    const recent = data && Array.isArray(data.recent) ? data.recent : [];
    if (recent.length === 0) return null;
    return h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "0.5rem" } },
      h(Separator, null),
      h("div", { style: { fontSize: "0.8125rem", fontWeight: 500 } }, "Recent activity"),
      h(
        "ul",
        { style: { display: "flex", flexDirection: "column", gap: "0.375rem", margin: 0, padding: 0, listStyle: "none" } },
        recent.slice(0, 8).map((entry, index) => h(ActivityRow, { key: index, entry })),
      ),
    );
  }

  function ActivityRow({ entry }) {
    const failed = Boolean(entry.error);
    return h(
      "li",
      { style: { display: "flex", gap: "0.5rem", alignItems: "baseline", fontSize: "0.8125rem" } },
      h(
        "span",
        { style: { color: "var(--muted-foreground)", whiteSpace: "nowrap" } },
        formatWhen(entry.at),
      ),
      h(
        "span",
        { style: { color: failed ? "var(--destructive)" : "inherit", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" } },
        failed ? entry.error : entry.taskTitle || entry.text,
      ),
      entry.permalink
        ? h(
            "a",
            {
              href: entry.permalink,
              target: "_blank",
              rel: "noreferrer noopener",
              style: { color: "var(--muted-foreground)", whiteSpace: "nowrap" },
            },
            "Slack",
          )
        : null,
    );
  }

  // Actions owns the two relay buttons. `busy` is a single string rather than
  // one flag per button so a second click cannot fire while either is running.
  function Actions({ reload, realtime }) {
    const [busy, setBusy] = React.useState(null);
    const [result, setResult] = React.useState(null);

    const run = React.useCallback(
      (key) => {
        setBusy(key);
        setResult(null);
        host.api
          .fetch("webhooks/" + key, { method: "POST" })
          .then(async (res) => {
            const body = await res.json().catch(() => ({}));
            if (key === "test") {
              setResult(
                body.ok
                  ? { ok: true, text: "Authenticated as " + describeIdentity(body) + ". This test does not check DM delivery." }
                  : { ok: false, text: body.error || `HTTP ${res.status}` },
              );
            } else {
              setResult({ ok: res.ok, text: res.ok ? "Scan queued." : body.error || `HTTP ${res.status}` });
            }
          })
          .catch((err) => setResult({ ok: false, text: String(err.message || err) }))
          .finally(() => {
            setBusy(null);
            reload();
          });
      },
      [reload],
    );

    return h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "0.5rem" } },
      h(
        "div",
        { style: { display: "flex", gap: "0.5rem", flexWrap: "wrap" } },
        h(
          Button,
          { variant: "outline", size: "sm", disabled: busy !== null, onClick: () => run("test") },
          busy === "test" ? "Testing…" : "Test authentication",
        ),
        realtime
          ? null
          : h(
              Button,
              { variant: "outline", size: "sm", disabled: busy !== null, onClick: () => run("scan") },
              busy === "scan" ? "Scanning…" : "Scan now",
            ),
      ),
      h(
        "div",
        { style: { fontSize: "0.8125rem", color: "var(--muted-foreground)" } },
        "Test authentication checks saved credentials only. It does not check DM permissions or send a message.",
      ),
      result
        ? h(
            "div",
            { style: { fontSize: "0.8125rem", color: result.ok ? "var(--muted-foreground)" : "var(--destructive)" } },
            result.text,
          )
        : null,
    );
  }

  function StatusCard() {
    const { loading, error, data, reload } = useStatus();

    return h(
      Card,
      null,
      h(
        CardHeader,
        null,
        h(
          CardTitle,
          { style: { display: "flex", alignItems: "center", gap: "0.5rem" } },
          "Slack connection",
          h(ConnectionBadge, { data }),
          h(Identity, { data }),
        ),
      ),
      h(
        CardContent,
        { style: { display: "flex", flexDirection: "column", gap: "0.75rem" } },
        loading ? h(Skeleton, { style: { height: "1rem", width: "12rem" } }) : null,
        error ? h(Alert, { variant: "destructive" }, h(AlertDescription, null, error)) : null,
        !loading && data && data.error
          ? h(Alert, { variant: "destructive" }, h(AlertDescription, null, data.error))
          : null,
        h(MetaRow, { data }),
        h(ScopeHint, { data }),
        h(SetupHint, { data }),
        h(NotificationHint),
        h(Timestamps, { data }),
        h(Actions, { reload, realtime: Boolean(data && data.realtime) }),
        h(Activity, { data }),
      ),
    );
  }

  return StatusCard;
}

function describeIdentity(body) {
  const parts = [body.teamName, body.userName ? "@" + body.userName : null].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : "Slack";
}

// formatWhen renders an ISO timestamp as a short relative age. The card
// refreshes every 15s, so anything more precise than this would just churn.
function formatWhen(iso) {
  if (!iso) return "never";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "never";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return minutes + "m ago";
  const hours = Math.round(minutes / 60);
  if (hours < 24) return hours + "h ago";
  return Math.round(hours / 24) + "d ago";
}

window.registerKandevPlugin(PLUGIN_ID, {
  // initialize may run more than once in a tab as the plugin is disabled and
  // re-enabled. Registration is idempotent and the card owns its own interval
  // via useEffect cleanup, so there is nothing to tear down in destroy.
  initialize(registry, host) {
    registry.registerComponent("plugin-settings", makeStatusCard(host));
  },
});
