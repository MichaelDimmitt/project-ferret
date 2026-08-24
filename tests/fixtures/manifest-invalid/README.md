# Invalid manifest fixtures

One file per error class in `docs/design/MANIFEST_SCHEMA.md` §8. Each **must**
be rejected before any probe executes, and the loader must exit 3.

Each file is invalid for exactly one reason, named by its filename. A fixture
that fails for two reasons proves nothing about either — if a test passes
because the loader tripped on the wrong rule, the rule under test is untested.

| File | §8 rule | What it gets wrong |
|---|---|---|
| `unknown-field.json` | 1 | `timout_s` — the typo that would otherwise mean "10 seconds" forever |
| `duplicate-id.json` | 2 | Same id twice in one file |
| `dangling-taint.json` | 3 | `tainted_by` names a check that does not exist |
| `taint-cycle.json` | 4 | A depends on B depends on A; both ends must be named |
| `missing-remedy.json` | 5 | `blocker` with no remedy — a report with no action |
| `mutating-no-why.json` | 6 | Override with no justification |
| `mutating-true.json` | 6 | `mutating: true`, which has no legal meaning |
| `declared-unset.json` | 7 | `$declared` referenced; no `declared` field |
| `unknown-token.json` | 8 | `$skew_abs` — §9's deliberately-wrong example |
| `bad-expect-type.json` | 9 | `contains` is not a predicate |
| `expect-missing-field.json` | 9 | `one_of` with no `values` |
| `bad-regex.json` | 10 | Unclosed group in `matches` |
| `bad-timeout.json` | 11 | `timeout_s: 0` |
| `bad-severity.json` | 11 | `critical` is not a severity |
| `bad-layer.json` | 12 | `layer: 47` |
| `bad-id.json` | 12 | `Bad.Mixed-Case` fails the id pattern |

`unknown-token.json` is worth reading alongside MANIFEST_SCHEMA.md §11. It is
not a hypothetical typo — it is the clock-skew check written the way it reads
most naturally, which the schema cannot express. The fixture exists so that
limitation stays visible rather than being rediscovered.

## Cross-file duplicates

Rule 2 also applies across files, which a single directory cannot express.
`../manifest-dup-a/` and `../manifest-dup-b/` are loaded together and define
the same id in different files.
