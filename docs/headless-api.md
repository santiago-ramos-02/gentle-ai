# Headless JSON API

← [Back to non-interactive mode](non-interactive.md)

`gentle-ai api <method>` gives GUIs, IDEs, and remote dashboards everything the TUI main menu offers, as JSON. Every method calls the same services and flow rules as the TUI, so a host and the TUI never disagree about what a choice means.

## Calling convention

- Pass parameters as one JSON object on stdin. Empty stdin, or a terminal on stdin, means `{}`.
- stdout carries newline-delimited JSON. Each line is complete and flushed as soon as it is written.
- Exit code is 0 when the final line is a result and 1 when it is an error.
- The API never prompts, never needs a terminal, and never self-updates. It never resolves paths against its own working directory: methods that act on a project take an absolute `cwd`.
- Anything else a method prints goes to stderr, so stdout stays parseable.

```bash
echo '{"agent":"claude-code"}' | gentle-ai api models.get
```

Start with `describe` to feature-detect instead of pinning a version:

```bash
gentle-ai api describe
```

## Stream format

Zero or more event lines come first:

```json
{"type":"progress","step":"agent:claude-code","stage":"apply","status":"running"}
{"type":"log","message":"  ✓ Upgrading engram"}
```

`status` is `running`, `succeeded`, `failed`, or `skipped`; a failed or skipped step carries `error`. Log events carry command output, one line per event.

Exactly one final line follows:

```json
{"type":"result","schema":"gentle-ai.api/v1","data":{}}
{"type":"error","schema":"gentle-ai.api/v1","error":{"code":"invalid_params","message":"selection is required"}}
```

| Error code | Meaning |
| --- | --- |
| `invalid_params` | The params are malformed, unknown, or leave an installer question unanswered. Nothing changed. |
| `not_found` | The named backup or plugin does not exist. |
| `unsupported` | The method is unknown, or the machine cannot do it (for example, no OpenCode or no builder engine). |
| `conflict` | A review store reset would remove reviews that are still open. |
| `failed` | The operation ran and failed. |

Field names are camelCase. Lists and objects are never `null`; an optional value that is not set is omitted.

## Methods

| Method | Params | Result |
| --- | --- | --- |
| `describe` | none | `version`, `apiVersion`, `methods` |
| `status` | none | System, agents, components, presets, personas, skills, persisted `state`, `openCodeDetected`, `builderEngines` |
| `plan` | `selection` | Resolved agents and components, pipeline `steps`, and the installer `questions` for that selection |
| `install` | `selection`, `modelPresets`, `models`, `communityTools`, `openCodePlugins`, `rdd`, `background`, `cwd` | Progress events, then `steps`, `manualActions`, `backupId`, `rddMode` |
| `sync` | `agents`, `models` | `files`, `manualActions` |
| `updates` | `force` | `checked` and one entry per managed tool |
| `upgrade` | `tools`, `backup`, `sync` | Log events, then `upgraded`, `failed`, `skipped`, `backupId`, `restartRequired`, `syncPending`, `files`. With `sync`, a sync follows, like the TUI's Upgrade + Sync |
| `models.get` | `agent`, `discover`, `cwd` | `presets`, `currentPreset`, role rows (`phases`), `current`, and the choices in `options` |
| `models.set` | `agent` with `preset` or `models` | Syncs that agent, then `files`, `manualActions` |
| `backups.list` | none | `backups` |
| `backups.restore`, `backups.delete` | `id` | `restoredFiles`, or `{}` |
| `backups.rename` | `id`, `description` | `{}` |
| `backups.pin` | `id`, `pinned` | `{}` |
| `plugins.list` | none | OpenCode community `plugins`, `supported`, `reason` |
| `plugins.install` | `ids` | Per-plugin `changed` and `files` |
| `plugins.uninstall` | `id` | The layers the uninstall touched |
| `tools.list` | `cwd` (optional) | Community tools with CLI and per-agent status |
| `tools.install` | `ids`, `cwd` | Log events, then per-tool `commandsRun` and `manualActions` |
| `builder.engines` | none | Agent builder `engines` and whether each is available |
| `builder.generate` | `engine`, `prompt` | The generated `agent`, install `targets`, and name `conflicts` |
| `builder.install` | `agent`, `engine` | Installed `files`, `renamedTo`, `warnings` |
| `review.status` | `cwd` | The `gentle-ai.review-mode/v1` report |
| `review.set` | `cwd`, `enabled`, `scope` | The `gentle-ai.review-mode/v1` report |
| `reviewStore.survey` | `cwd` | The `gentle-ai.review-store-reset-result/v1` report |
| `reviewStore.reset` | `cwd`, `includeInFlight`, `includeAdapterReviews` | The `gentle-ai.review-store-reset-result/v1` report |
| `uninstall.plan` | `mode`, `agents`, `components`, `engramScope`, `cwd` | What the mode removes and whether project Engram cleanup is available |
| `uninstall.run` | same as `uninstall.plan` | The uninstall report plus `binaryRemoved` or `synced` |
| `doctor` | none | `ok` and structured `checks` |
| `footprint` | `agent` | What removing Gentle AI from that agent would undo, without changing anything: `removed` paths, `rewritten` files with their content afterwards, and `unsimulated` paths outside the home it could not simulate. A host uses it to run the agent without Gentle AI. |

The review reports keep the field names of their existing schemas.

## Installing without prompts

The TUI asks optional questions depending on the selection. `plan` returns them in installer order, and `install` takes one param per question:

| Question | Param |
| --- | --- |
| `communityTools` | `communityTools` (omit it to keep the recorded community tools; `tools.install` manages them too) |
| `openCodePlugins` | `openCodePlugins` |
| `skills` | `selection.skills` (omit it to keep the picker's default: every skill) |
| `rdd` | `rdd` (omit it to leave the global review mode unchanged) |
| `openCodeBackground` | `background.opencode`: `on` or `off` |
| `piBackground` | `background.pi`: `on` or `off` |

The background questions appear only when the environment and your previous choice leave them open. An open question without an answer is `invalid_params`, never a prompt.

`selection` takes `agents`, and optionally `components`, `skills`, `persona`, and `preset`. Omitted components and skills come from the preset, exactly as in the installer.

## Model assignments

`models` mirrors the sync overrides the TUI's Configure Models screen produces. An absent field leaves that assignment alone, and an empty object clears it back to defaults:

```json
{"claudePhaseAssignments": {"odd-worker": {"model": "opus", "effort": "high"}}, "kiroModelAssignments": {}}
```

`models.set` with a `preset` resolves that preset exactly as the picker does, including the Codex preset's lane models and curated orchestrator. `install` accepts the same choices as `modelPresets` (`{"codex": "powerful"}`) and `models`; explicit `models` fields win. Choices for agents you do not touch are kept.

## Upgrades that replace gentle-ai

When `upgrade` updates gentle-ai itself, the running binary is stale and it reports `restartRequired`. With `sync`, it skips the sync, records it for the next launch, and also reports `syncPending`. Start a new `gentle-ai` process before calling anything else.

## Retired features

SDD, its profiles, the strict TDD choice, and the agent builder's SDD integration were retired from Gentle AI, so the API has no parameters for them.
