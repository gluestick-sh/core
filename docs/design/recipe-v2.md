# Environment recipe schema v2 (draft)

Status: **draft for review — no engine code implements this yet.**
Kind: `gluestick.recipe` · `schemaVersion: 2` · Owner: **core**
Related: [glossary.md](glossary.md) (terms), [kinds.md](kinds.md) (kind rules).

## 1. Goal

One file that describes a **target environment** for a single machine, so that
`glue install --recipe <file-or-id>` can reproduce it in one command:

- packages to install (with optional version constraints),
- environment variables the tools need,
- a small, auditable set of lifecycle actions.

Non-goals: replacing the manifest/bucket system, replacing environment snapshots
(rollback), or shipping a recipe store in the cloud (that is a later, separate
axis).

## 2. Document

```json
{
  "kind": "gluestick.recipe",
  "schemaVersion": 2,
  "id": "claude-code",
  "name": "claude-code",
  "description": "Claude Code CLI runtime (no desktop app / editor integration)",
  "packages": [
    "main/git",
    { "name": "claude-code", "constraint": ">=2.1.0" }
  ],
  "env": {
    "CLAUDE_CODE_GIT_BASH_PATH": "{apps}/git/current/bin/bash.exe"
  },
  "hooks": {
    "pre": [
      { "unblock_motw": true }
    ],
    "post": [
      { "run": "claude --version", "timeout": "30s", "on_fail": "stop" },
      { "print": "Environment ready: run `claude` to sign in" }
    ]
  },
  "requires": { "glue": ">=0.2.0" },
  "x-vendor": {}
}
```

## 3. Top-level fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `kind` | string | **yes** | Must be `gluestick.recipe` (v1 allowed an empty kind for legacy files; v2 requires it) |
| `schemaVersion` | int | **yes** | `2` |
| `id` | string | no on import | Stable id, `[a-z0-9][a-z0-9._-]*`; assigned on import when missing; never interpreted as a path segment |
| `name` | string | **yes** | Human label, trimmed, non-empty |
| `description` | string | no | Trimmed. **`desc` is accepted as a read-only legacy spelling and rewritten to `description`** |
| `version` | string | no | **Recipe revision** (this document's own version). Must not be confused with package versions; a reader that does not understand it must not drop it |
| `packages` | array | **yes** | See §4 |
| `env` | object | no | See §5 |
| `hooks` | object | no | See §6 |
| `requires` | object | no | Toolchain gate, e.g. `{ "glue": ">=0.2.0" }`; unsupported requirement → `err_unsupported_version`, never a partial deploy |
| `x-*` | any | no | Reserved extension namespace; must be preserved on round-trip |

## 4. `packages`

Each entry is either a string or an object:

| Form | Meaning |
| --- | --- |
| `"main/git"` | Package ref: `name` or `bucket/name`; `@version` is **not** allowed here (use the object form) |
| `{ "name": "claude-code", "bucket": "main", "constraint": ">=2.1.0" }` | Explicit form |

| Field | Type | Notes |
| --- | --- | --- |
| `name` | string | Required, trimmed |
| `bucket` | string | Optional; when absent, resolve through the normal engine path (ambiguity is an error, not a guess) |
| `constraint` | string | Optional version constraint, see §4.1 |
| `optional` | bool | When true, a missing package is a warning; the deploy still fails if the *required* set fails |

Normalization (must be identical in every implementation):

1. Trim; drop entries with an empty name.
2. Dedupe case-insensitively on `bucket/name`; the first occurrence wins.
3. Sort into bucket order for reporting only; deploy order is the resolver's.

**v1 incompatibility (this is the shipped blocker):** v1 model is
`[]RecipePackage` (objects only). Reading a v2 document with mixed
string/object entries today fails inside `json.Unmarshal` and surfaces as a
generic "invalid recipe format". The v2 reader must implement a custom
`UnmarshalJSON` for the union; a v1-only reader must reject a v2 document with
`err_unsupported_schema`.

### 4.1 `constraint` grammar

Minimum viable grammar (nothing like this exists in core today):

```text
constraint := "*" | op version ("," op version)*
op         := ">=" | "<=" | ">" | "<" | "=" | "^"
version    := <tag as published by the bucket>
```

Rules:

- `*` means "whatever the bucket publishes".
- Comparisons use a tolerant tag comparison (numeric segments compared
  numerically, pre-release suffixes ordered below the release). A tag that is
  not parseable as a version can only be matched by `=` or `*`.
- **An unsatisfiable constraint is an error**, not a fallback to latest
  (`err_constraint_unsatisfied`), and it must be reported at plan time.
- Range constraints are advisory for `glue update`: a pinned/constrained recipe
  must not silently upgrade past its constraint.

## 5. `env`

Semantics: this maps to the existing **`env_set`** behavior — variables written
at **user scope** and removed when the owning package/recipe is removed
(`manifest.env_set` → `envset.ApplyUser` / `RemoveUser`). It does **not** map to
manifest `env` (per-shim injection only).

| Field | Type | Notes |
| --- | --- | --- |
| `<NAME>` | string | Variable name; `[A-Za-z_][A-Za-z0-9_]*`; `PATH` itself is **not** allowed (use shims) |

Placeholders (whitelist — anything else is `err_invalid_format`, because an
unrecognized placeholder would otherwise be written to the registry verbatim):

| Placeholder | Expands to | Note |
| --- | --- | --- |
| `{apps}` | `<root>/apps` | e.g. `{apps}/git/current/bin/bash.exe` |
| `{root}` | Glue data root | |
| `{shims}` | `<root>/shims` | the shim directory that goes on PATH |
| `{version}` | Resolved version of the named package in this entry | only inside that package's scope |
| `$dir` | Install directory of the named package | legacy spelling, kept for parity with manifest `env_set` |

Two requirements that decide whether the "no terminal restart" promise holds:

1. **User scope is not enough for the current session.** `envset.ApplyUser`
   writes the registry; an already-open shell will not see it. A deploy must
   therefore also export the resolved variables into the process environment used
   for the post hooks, and print an explicit `$env:NAME=...` line so the user can
   adopt them without restarting.
2. **Ownership must be recorded.** Today nothing remembers "this variable came
   from a recipe", so uninstalling leaves it behind. v2 requires recording
   `(name, owner-package-or-recipe, previous-value)` so removal can restore or
   delete it.

## 6. `hooks`

**No arbitrary scripts.** A manifest may contain raw PowerShell because buckets
are a trusted, reviewable source; a recipe is often *hand-written or pasted from
the internet*, so v2 only allows **named actions**.

```json
"hooks": {
  "pre":  [ { "unblock_motw": true } ],
  "post": [ { "run": "claude --version", "timeout": "30s", "on_fail": "stop" } ]
}
```

| Action | Fields | Mutates host? | Status |
| --- | --- | --- | --- |
| `run` | `run` (argv-style string), `timeout`, `cwd`, `on_fail` | runs a process | planned |
| `print` | `print` | no | planned |
| `unblock_motw` | `unblock_motw: true` | clears mark-of-the-web | **needs measurement**: Glue downloads over Go HTTP and does not set MOTW itself, so this may be a no-op step. Prove it is needed before shipping |
| `ensure_execution_policy` | `ensure_execution_policy: "RemoteSigned"` | **yes — host security setting** | **needs design review**: core already runs its own hooks with `-ExecutionPolicy Bypass`, so Glue does not need this. Changing host policy is a higher-privilege action than installing a package: whitelist values, `CurrentUser` scope only, record the previous value, require explicit consent, log it (see `docs/security.md`) |

Execution rules:

- Hooks run **non-interactively** (no stdin) with a default timeout; exceeding it
  fails the step.
- `pre` runs before package installation; `post` runs after the package set is
  finalized — the runner must inject the shim directory into the child process
  `PATH`, otherwise a shim installed by this same run is invisible.
- Captured output must be shown on failure; secrets must not be echoed.

### 6.1 `on_fail`

| Value | Meaning | Status |
| --- | --- | --- |
| `stop` | Abort the deploy; already-installed packages stay | planned (default) |
| `continue` | Warn and keep going | planned |
| `rollback` | Undo everything this deploy did | **not implementable yet** |

`rollback` requires a transaction: a record of packages added, env variables
written, and (for host-level actions) previous values. Neither core nor the CLI
has such a primitive today — the closest is desktop-pro's pre-mutation snapshot,
which is manual and not a rollback of one recipe. Until that exists, a v2 reader
must **reject `on_fail: "rollback"` with a clear error** instead of promising it.
(Interim behavior, if accepted: uninstall the packages added by this run, remove
the env vars it wrote, then report what could not be undone.)

## 7. Deploy order (and idempotency)

```text
detect kind/schemaVersion
  → check requires
  → plan (dry-run): resolve packages + constraints against the index
  → report the plan (installs needed, env to write, actions to run); confirm mutations
  → install missing packages (parallel where the edition allows)
  → write env (user scope + current process)
  → run post hooks
  → print a machine-readable result (installed / already-present / skipped / failed)
```

Re-running the same recipe must be a no-op except for hooks explicitly marked
repeatable. `--dry-run` must produce the plan without touching anything. A recipe
deploy must be attributable in activity/snapshot history as "deployed recipe X"
by recording `snapshot.SourceRecipe` (`"recipe"`, defined in `core/snapshot/types.go`).
No code writes that source yet — the deploy path in §7 is the first producer.

## 8. Compatibility matrix

| Reader \ Document | `recipe` v1 | `recipe` v2 | `template-bundle` | `environment-snapshot` |
| --- | --- | --- | --- | --- |
| core (target) | read, migrate on write | read/write | **reject** (`err_unknown_kind`) | read/write |
| cli (`glue install --recipe`) | read | read/write | **reject** | reject |
| desktop-pro (shipped v1 reader) | read/write | **reject** with `err_unsupported_schema` — today it fails on the mixed array and would silently drop `env`/`hooks` | reject | read/write |
| desktop (free) | n/a today | n/a today | read (frontend only) | n/a |

Hard rules:

- **Unknown fields must never be dropped** for `recipe`. A document this build
  cannot fully honor must be refused, not partially applied.
- v1 keeps working: object-only `packages`, the `gluestick.workspace` alias, the
  legacy store directory. Writes always emit v2 with `kind: gluestick.recipe`.
- A bundle is not a recipe: converting a bundle entry ends in either a package
  ref list or a newly created recipe — never a direct pipe into the engine.

## 9. Error set

`err_unknown_kind`, `err_unsupported_schema`, `err_invalid_format`,
`err_unknown_field`, `err_unsupported_version` (from `requires`),
`err_constraint_unsatisfied`, `err_package_not_found`, `err_hook_failed`,
`err_hook_unsupported` (e.g. `rollback` before transactions exist),
`err_host_action_denied` (host policy change without consent).

Each must map to an i18n key in the GUI/CLI; never match on message strings (use
the classification approach already used for resolve notices, see
`docs/security.md`).

## 10. Open questions

1. **Constraint semantics for non-semver buckets**: how tolerant should
   comparison be? (Scoop tags are frequently dates or mixed strings.)
2. **Host-level actions**: is `ensure_execution_policy` in scope for a recipe, or
   does it belong to a separately consented "system setup" step?
3. **`unblock_motw`**: measure on Windows 10 and 11 whether MOTW is ever present
   on Glue-downloaded artifacts before keeping the action.
4. **Ephemeral env**: should a recipe be able to declare session-only variables
   (no registry write) to avoid permanent machine mutation?
5. **Where recipes live**: local store only, or core-managed so CLI and Desktop
   share one truth source (recommended by the layer split in
   `desktop-pro/design_pro.md` §0.2.1)?
6. **Recipe-level uninstall**: should removing a recipe offer to uninstall the
   packages it installed (needs the same transaction record as `rollback`)?
