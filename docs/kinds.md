# Kind registry

`kind` is the ecosystem's type system: it is how a file, a cloud payload, or an
in-memory document announces **who may read it, who may execute it, and how
strict validation is**. Historically each project invented its own kinds and its
own validation, so the same JSON could be accepted, silently mangled, or
rejected depending on which binary opened it. This file is the single registry
that fixes that.

Terminology used below is defined in [glossary.md](glossary.md).

## Registry

| Kind | Owner | Lives in | Validated by | May the engine execute it? | Unknown-field policy |
| --- | --- | --- | --- | --- | --- |
| `gluestick.environment-snapshot` | core | core (`snapshot/types.go`), cloud payload (stored opaquely by the API) | `snapshot.Validate` (+ device-id match on the API side) | Yes (diff/apply) | **Preserve** on round-trip; additive fields allowed within the same `schemaVersion` |
| `gluestick.recipe` | core (target state; today desktop-pro) | core `recipe` package (target), local recipe store | core `recipe.Validate` | Yes (plan + deploy) | **Reject unknown, except `x-*`**; never silently drop |
| `gluestick.template-bundle` | desktop / desktop-pro frontend | Bundled JSON + WebView `localStorage` | frontend `templates/loader.ts` | **No — by design** | Ignore (display artifact) |
| `gluestick.workspace` | *deprecated alias* of `gluestick.recipe` | legacy local store | same as recipe (legacy branch) | Yes (read-only, migrated) | Same as recipe |
| `gluestick.profile` | — | — | — | — | **Dead literal.** No writer, no reader, no validation; do not implement |

Notes:

- The **cloud never inspects `kind`** today: the API treats the snapshot payload
  as opaque and only checks `snapshot.device.deviceId`. Adding a kind therefore
  needs no cloud change — but it also means nothing outside the owning project
  can tell you what a document is. That is exactly why this registry exists.
- **`template-bundle` is not executable.** A bundle is a catalog (see
  [glossary.md](glossary.md)); the engine must never accept one as an input to
  install/deploy.

## Validation contract

Proposed single entry point so every consumer behaves identically:

```go
// Kind identifies a document type registered in docs/kinds.md.
type Kind string

const (
	KindEnvironmentSnapshot Kind = "gluestick.environment-snapshot"
	KindRecipe             Kind = "gluestick.recipe"
)

// DetectKind returns the registered kind of raw, or ErrUnknownKind.
// It does not validate the body.
func DetectKind(raw []byte) (Kind, error)

// ValidateByKind validates raw against the rules for its kind and returns the
// normalized document. Implementations:
//   - must reject unknown top-level fields unless prefixed with "x-"
//   - must reject a recognized kind whose schemaVersion they cannot handle
//     (never "best effort" parse it)
//   - must return stable, i18n-mappable errors (see below)
func ValidateByKind(raw []byte) (Kind, any, error)
```

Error semantics that consumers may depend on (stable strings, mapped to i18n by
the GUIs):

| Error | Meaning | Consumer behavior |
| --- | --- | --- |
| `err_unknown_kind` | Not a registered document | "Not a Gluestick file" |
| `err_unsupported_schema` | Known kind, `schemaVersion` newer than this build | "Created by a newer version — update Gluestick" |
| `err_invalid_format` | Structural/field validation failed | "Not a valid <kind> file" |
| `err_unknown_field` | Unknown field without `x-` prefix | Show field name; never drop silently |

**Anti-pattern to remove:** a type that carries executing intent must not use a
plain unmarshal that ignores unknown fields. Today desktop-pro's recipe reader
drops `env`/`hooks` silently *and* hard-fails when `packages` mixes strings and
objects — both behaviors must become explicit (`err_unknown_field` /
`err_unsupported_schema`).

## Per-kind rules

### `gluestick.environment-snapshot`

- Shape owned by core; see `core/snapshot/types.go`.
- `device.deviceId` is required; cloud rejects a payload whose device id does not
  match the URL device.
- Additive changes must not bump `schemaVersion`; removals/renames must.
- Always round-trip unknown fields (forward compatibility for sync).

### `gluestick.recipe`

- v1 (shipped, desktop-pro only): packages-only, `packages: [{ name, bucket?, version? }]`.
- v2 (planned): additive — `packages` accepts `string | object`, plus `env`,
  `hooks`, `requires`. Full field definition: [recipe-v2](design/recipe-v2.md).
- A v2 document must be **rejected with `err_unsupported_schema`** by a v1-only
  reader; it must never be parsed with `env`/`hooks` discarded.
- `x-*` keys are reserved for experimental extensions and must be preserved.

### `gluestick.template-bundle`

- Frontend-owned, display-oriented; validation lives in
  `templates/loader.ts` (kind + `schemaVersion` + `origin: official` + non-empty
  `templates[]`).
- core, cli, and the desktop Go layer must not import or validate it. Converting
  a bundle entry into installable intent is a frontend concern that ends in a
  package ref list or a new recipe.

## Adding a new kind

Checklist — a kind is not "added" until all of these exist:

1. A row in the registry above (owner + storage + validator + executable?).
2. A canonical term in [glossary.md](glossary.md).
3. A `schemaVersion` and an explicit unknown-field policy.
4. A stable error set (see contract) and its i18n keys.
5. Round-trip tests plus, for executable kinds, a rejection test proving a newer
   `schemaVersion` is refused rather than half-applied.
6. A migration path for anything it replaces (see the sunset policy in
   [glossary.md](glossary.md)).
