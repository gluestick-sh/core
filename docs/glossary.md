# Glossary and terminology ownership

**Rule: one word → one concept → one owner.** This file is the only sanctioned
word list for core, cli, desktop, desktop-pro, web, and all product docs. If a
feature needs a new artifact, add a **kind** to [kinds.md](kinds.md) instead of
inventing a new synonym for an existing concept.

**Budget:** at most 8 canonical rows and 3 banned words — adding a term requires
removing one. Every definition must fit one line (≤ 15 words).

```
 one package:   bucket -> manifest -> package
 one machine:   recipe --deploy--> machine
 history:       machine --record--> snapshot --sync--> cloud
```

## Canonical terms

### Our words (choosable — the budget above applies)

| Term | What it is | Kind | Owner (source of truth) | Stored where | Status |
| --- | --- | --- | --- | --- | --- |
| **Recipe** | Declarative **target** environment for one machine: packages, env vars, lifecycle hooks | `gluestick.recipe` | **core** (parse / plan / apply); Desktop & CLI are shells | Local recipe store (`recipes/*.json`) | v1 = desktop-pro only; v2 = planned, see [recipe-v2](design/recipe-v2.md) |
| **Bundle** | Read-only **catalog** of recipes plus display metadata; **never executed by the engine** | `gluestick.template-bundle` | desktop / desktop-pro **frontend only** | Bundled JSON + WebView `localStorage` overrides | Shipped |
| **Snapshot** | Recorded **current** state (device + packages + buckets + config), used for sync and rollback | `gluestick.environment-snapshot` | **core** | Exported/imported file, or cloud per-device payload | Shipped (only end-to-end kind today) |
| **Package ref** | Install reference: `name`, `bucket/name`, with optional `@version` | — | **core** (`engine.ParsePkgRef`) | — | Shipped |

UI labels are not terms (frontend i18n only): a bundle entry is shown as
*official recipe*, a local recipe as *my recipe*. Map every locale string to a
term from this table.

### Contract words (Scoop-locked — explain, never rename)

**bucket / manifest / shim / cache / store / device** as defined in the
[README](../README.md), plus two spellings people confuse: `env` = per-package
variables injected when running the package's shims; `env_set` = persistent
**user-scope** environment variables written by the package. Never write
"environment variables" without naming which one.

### The two pairs people keep confusing

| Pair | Rule |
| --- | --- |
| **bundle vs recipe** | A bundle is a **catalog** (many, read-only, display-oriented). A recipe is **intent** for one machine (deployable, versioned, executable). Pick a bundle entry → convert it into a recipe, or install its packages directly. Never call a bundle a recipe. |
| **recipe vs snapshot** | A recipe says where you **want** to be (declarative, hand-written). A snapshot says where you **were** (recorded, used for rollback/sync). Deploying a recipe is *not* creating a snapshot, although a deployment should be attributable in the snapshot/activity trail. |

## Banned and deprecated terms

**Never use:** `profile` (timing = `--timings` / `Options["timings"]` /
`GLUE_TIMINGS`; no profile kind or snapshot source exists; Windows spellings
`-NoProfile`, `%USERPROFILE%`, `profile.default` stay), `template` (`templates[]`
payload key only; say *bundle entry*), `workspace` (legacy alias of *recipe*,
read-only).

## Rules for new work

1. **Adding a concept** means adding a row here *and* a kind row in
   [kinds.md](kinds.md). A concept without an owner and a kind does not ship.
2. **Anything the installer can execute** must have a kind registered in core.
   UI-only artifacts (bundles) must stay UI-only and must be marked as such.
3. **Unknown fields must never be silently dropped** by a type that carries
   executing intent (recipe, snapshot). See the per-kind strictness table in
   [kinds.md](kinds.md).
4. **User-facing strings** must map to a canonical term. If a locale needs a
   word that the canonical term cannot cover, fix the concept, not the locale.
5. **Granularity must be stated** whenever the word *recipe* is used in UI or
   docs: bundle entry (one of many) vs deployable recipe (one environment).

## Sunset policy for deprecated terms

Aliases follow `read-old → write-new → remove`, tested at each step. The only
debt left is `workspace`: add the migration test (zero coverage today), then
delete the legacy read branch in desktop-pro `recipe.go`, the `workspaces/`
directory, the `ListWorkspaces` / `SaveWorkspace` / … Wails bindings (frontend no
longer calls them; confirm no third-party automation first), and their i18n keys.
