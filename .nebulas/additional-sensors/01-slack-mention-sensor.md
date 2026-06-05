+++
id = "slack-mention-sensor"
title = "Slack mention sensor: poll a channel for @quasar mentions; the mention message becomes a seed nebula"
type = "task"
priority = 2
depends_on = ["linear-sensor"]
scope = [
    "internal/sensors/slack/**",
]
+++

## Problem

The user's mental model includes someone in #engineering typing `@quasar can you add retries to the rate-limit handler?` and walking away — Quasar should ingest that as a seed nebula. This is a low-effort path to drafting nebulas without ever leaving Slack, and the message itself carries enough context to draft a reasonable problem statement.

The sensor polls (no Slack Events API webhook — we stay polling-only to keep parity with the github/linear sensors and to avoid running a public webhook server on the EC2). It uses `conversations.history` with a cursor.

## Solution

### Sensor

`internal/sensors/slack/slack.go`:

```go
type Sensor struct {
    name         string  // "slack_mention"
    channelID    string  // required (Cxxx ID, not channel name)
    botUserID    string  // required — the @quasar user id, used to detect mentions
    botToken     string  // resolved at Configure (xoxb-...)
    triggerEmoji string  // optional — e.g. "🤖" reacts to a message also fire
    httpClient   *http.Client
}

func (s *Sensor) Poll(ctx context.Context, cursor json.RawMessage) (events []sensors.Event, newCursor json.RawMessage, err error) {
    // cursor: { "oldest_ts": "1719000000.000000" } — Slack timestamp format
    // 1. conversations.history(channel=channelID, oldest=cursor.oldest_ts)
    // 2. Filter to messages whose text contains <@botUserID> OR whose reactions include triggerEmoji
    // 3. For each, fetch thread context via conversations.replies if message.thread_ts != message.ts
    // 4. Return one event per qualifying message; newCursor = max(ts seen)
}

func (s *Sensor) SeedNebula(event sensors.Event) (*sensors.SeedNebulaContent, error) {
    // SourceName: "slack"
    // SourceID: "<channelID>:<ts>"   (Slack ts is the stable message id)
    // SourceURL: https://<workspace>.slack.com/archives/<channelID>/p<ts-formatted>
    // Name: nebula-slack-<ts-formatted>-<first-5-words-slugified>
    // Goals: the mention message with the <@botUserID> stripped + thread context if present
    // Assignee: Slack username of the message author (best-effort, no GH mapping)
}
```

### Mention extraction

Slack messages with mentions use `<@U12345>` raw tokens; the bot user's ID is in config. The sensor strips that token before passing the body to `SeedNebula`. Thread context (replies prior to the mention) is included as a `## Context` section beneath the mention text.

### Reaction-based triggers (optional)

If `triggerEmoji` is configured, the sensor also fires on messages with a reaction matching that emoji from any user. This lets non-bot-mentioning users say "yes, do this" by reacting. Useful for converting an existing message into a nebula without re-pinging the bot.

### Per-repo sensor TOML

```toml
name = "slack-platform-mentions"
type = "slack_mention"
poll_interval = "1m"
max_inflight = 2

[config]
channel_id    = "C01ABCDE"
bot_user_id   = "U02QUASAR"
bot_token_env = "SLACK_BOT_TOKEN"
trigger_emoji = "rocket"      # optional

[[triggers]]
constellation = "architect"
when = "new_item"
```

`max_inflight = 2` is intentionally lower than GitHub — channel mentions arrive in bursts when a Slack thread heats up; we don't want one heated conversation to fork 8 nebulas.

### Limitations (documented in the authoring guide, Phase 3)

- No private-channel detection beyond "the bot is invited" — if the bot's not in the channel, Slack returns `not_in_channel` and Poll surfaces a Configure-time validation error
- No emoji reactions on threaded replies — only top-level messages can be reaction-triggered
- No file attachments — message text only; if an image/file is attached, the SeedNebula has a note "[Attachment elided — see Slack source URL]"

### Tests

- Mention extraction: regex correctness on edge cases (multiple mentions, mention in code block, mention at start vs middle)
- Reaction trigger: emoji match (case-insensitive) fires; non-matching emoji does not
- Thread context: replies above the trigger message are included; replies below are not
- Cursor: `oldest_ts` advances per poll; messages older than cursor are filtered
- Configure: missing bot_user_id or channel_id rejected with field path

## Files

- `internal/sensors/slack/slack.go` (new)
- `internal/sensors/slack/mentions.go` (new) — text manipulation
- `internal/sensors/slack/api.go` (new) — Slack web API client (conversations.history, conversations.replies)
- `internal/sensors/slack/seed_nebula.go` (new)
- `internal/sensors/slack/slack_test.go` (new)
- `internal/sensors/slack/testdata/*.json` (new) — fixture Slack API responses

## Acceptance Criteria

- [ ] `slack.Sensor` implements `sensors.Sensor`
- [ ] `Configure` requires `channel_id`, `bot_user_id`, and bot token resolution
- [ ] Poll detects mentions via `<@botUserID>` token in message.text
- [ ] Poll detects reaction triggers if `trigger_emoji` is set
- [ ] Mention token is stripped from the seed nebula body
- [ ] Thread context (prior replies) is included as a `## Context` section when triggering on a threaded message
- [ ] `external_id` = `<channelID>:<ts>`
- [ ] `source_url` uses the canonical archive URL format
- [ ] No bot-channel-not-invited surprises: Configure validates by issuing a test `conversations.info` call
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
