package bootstrap

// ChromeSnippet reads xoxc from Slack's web localStorage
// The d cookie is HttpOnly so JS cannot see it
const ChromeSnippet = `(() => {
  if (!location.host.endsWith("slack.com")) {
    return "Open https://app.slack.com first, then run this again";
  }
  const raw = localStorage.localConfig_v2;
  if (!raw) {
    return "No localConfig_v2. Sign in to Slack in this tab";
  }
  const teams = JSON.parse(raw).teams || {};
  const rows = Object.entries(teams).map(([id, t]) => ({
    name: t.name || id,
    team: id,
    xoxc: t.token,
  }));
  console.table(rows);
  copy(rows.map((r) => r.xoxc).filter(Boolean).join("\n"));
  return rows.length + " xoxc token(s) copied to the clipboard";
})()`
