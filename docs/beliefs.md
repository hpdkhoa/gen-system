# Beliefs

A belief is one statement the system holds about one routine. Beliefs come from three places,
they are not equal, and the difference is the whole point of the design.

- **parser.** Derived from the offline parse. Deterministic. Rebuilt from scratch on every load
  and never persisted.
- **enrich.** Proposed by a local model. Persisted, and checked against what the parser observed
  before it is allowed anywhere near generation.
- **human.** You corrected it. Persisted. This is the only source that steers generation.

Source: `generator/internal/belief` (`belief.go`, `validate.go`, `summarize.go`). The package
imports only `ast`, `flow`, and `symbols`, so both halves of the system can depend on it.

## The record

`Belief` is defined at `belief.go:110`. The fields that carry the argument:

| Field | JSON | Meaning |
|---|---|---|
| `ID` | `id` | `<kind>:<symbolID>` for parser, `enrich:<kind>:<symbolID>` for enrich, `summary:<scope>:<name>` for summaries |
| `Kind` | `kind` | `inputs`, `calls`, `effect`, `data-change`, `action`, `computation`, `precondition`, `summary` |
| `Object` | `object` | What the machine derived |
| `CorrectedTo` | `corrected_to` | What the human said instead |
| `Source` | `source` | `parser`, `enrich`, or `human` |
| `HumanVerified` | `human_verified` | A person signed off on this |
| `Validated` | `validated` | `consistent`, `contradicts`, or `needs-human`. Empty for parser and human beliefs |
| `ValidationNote` | `validation_note` | Why it was marked that way, in English |
| `Stale` | `stale` | The source changed after the human corrected it |
| `SourceNow` | `source_now` | What the machine derives *now*, kept alongside the human's value |

`Object` and `CorrectedTo` coexist on purpose. The machine's answer is never overwritten by the
human's; both are on the record, which is what makes the staleness check below possible.

`Confidence` (`belief.go:117`) has a JSON tag and is never assigned anywhere in
the shipping code. It always serializes as `0`. It is retained for wire compatibility and should
not be read as a probability.

## What is on disk

`<project-root>/.gen/beliefs.json`. It holds **three** maps, and parser beliefs are not among them
(`belief.go:1007`):

```json
{
  "corrections":    { "<beliefID>": { "belief_id": "...", "corrected_to": "...", "derived_at": "..." } },
  "enrich_beliefs": { "<beliefID>": { ... } },
  "summaries":      { "<beliefID>": { ... } }
}
```

Parser beliefs are transient by design (`belief.go:264`, `RebuildFromIndex` wipes the map on every
load). If the source changed, the derived belief changes with it, and there is no stale cache to
disagree with the code.

## A worked example

Take `OrderService.Checkout`, a method that writes an order and decrements stock.

**Step 1. The parser derives, deterministically.** `deriveParserBeliefs` (`belief.go:485`) emits up
to five beliefs per callable. Two of them here:

```
effect:internal/order/service.go#method:OrderService.Checkout
  touches  →  db

action:internal/order/service.go#method:OrderService.Checkout
  appears_to  →  "checkout"
```

The action is a shallow guess from the name (`shallowAction`, `belief.go:686`, splits the leading
verb: `Checkout` becomes `checkout`, `AdjustStock` becomes `adjust stock`). The predicate
`appears_to` marks it as a guess. The parser infers the effect conservatively from callee names.

**Step 2. The model proposes more, and gets checked.** Run `gen-enrich` and a local model reads the
body and proposes richer claims. Each one is validated against the parser's facts before it is
stored, by `validateEnrich` (`validate.go:58`). Three rules:

```go
case KindComputation, KindPrecondition:
    b.Validated = "needs-human"
    b.ValidationNote = "semantic claim: verify against the source by hand"
```

A claim about what a computation *means* cannot be checked mechanically, so it is never marked
consistent. It waits for a person.

```go
if assertsPersistence(text) && !f.hasDBWrite && !f.hasFSWrite {
    b.Validated = "contradicts"
    b.ValidationNote = "claims it writes/saves data, but the parser observed no db/fs effect"
}
if assertsPurity(text) && (f.hasDBWrite || f.hasFSWrite) {
    b.Validated = "contradicts"
    b.ValidationNote = "claims no state change, but the parser observed: " + f.effects
}
```

Say the model claims "persists the order to the database" about a routine with no db or fs effect.
`validateEnrich` marks that claim `contradicts` with the reason. The parser's facts decide every
such conflict.

The persistence detector (`validate.go:96`) is deliberately conservative. It vetoes on
`in-memory`, `cache`, `temporary` before it starts, and it requires a whole-word verb plus a
whole-word durable target, so `restore` does not match `store`.

For our example the model proposes:

```
enrich:action:...OrderService.Checkout
  "writes the order to the database and decrements stock"
  validated: consistent          ← parser saw a db effect, so this holds up

enrich:precondition:...OrderService.Checkout
  "rejects if the requested quantity exceeds stock on hand"
  validated: needs-human         ← semantic, no mechanical check exists
```

Had the parser seen no db effect, the first claim would read `contradicts` instead, with the note
saying exactly which observation it failed against.

**Step 3. Contradicting claims never steer generation.** This is stronger than it sounds.
There is a guard method,
`Belief.IsBlockedForSteering()` (`validate.go:179`), which returns true for enrich beliefs marked
`contradicts`. It has no caller in the shipping code, and its own comment says so.

The reason it needs none is that the only thing that ever feeds beliefs into a generation prompt
is `CorrectionContext` (`belief.go:802`), and it filters on a stricter condition:

```go
for id, b := range bs.beliefs {
    if b.HumanVerified && b.Source == SourceHuman {
        ids = append(ids, id)
    }
}
```

Only human verified beliefs steer. The filter excludes every parser belief and every enrich
belief, contradicting or not. The exclusion lives in that filter condition, so there is no
separate rule that someone could forget.

**Step 4. The human corrects.** You read the action belief, and it is wrong. `Checkout` also
reserves stock, and that matters. You correct it through the Studio, or:

```
POST /api/present/beliefs/correct
{"belief_id": "action:internal/order/service.go#method:OrderService.Checkout",
 "corrected_to": "place an order and reserve stock",
 "human_note": "the name says checkout but it also reserves"}
```

`Correct` (`belief.go:937`) records your value, flips `Source` to `human`, sets `HumanVerified`,
and stores `DerivedAt`. That is a snapshot of what the machine derived at the moment you
disagreed with it, and it is the entire staleness mechanism.

From now on your sentence is what the planner sees, under a header that tells the model these were
reviewed by a person and are authoritative.

**Step 5. The source changes, and the belief goes stale.** Someone renames the method, or changes
what it calls, and the parser now derives something different. On the next load,
`applyCorrectionLocked` (`belief.go:751`) compares:

```go
case c.DerivedAt != freshDerived:
    b.Stale = true
    b.SourceNow = freshDerived
```

The system keeps your answer and stores the fresh derived value in `SourceNow`. It flags the
belief and shows **both values**. `Warnings()` (`belief.go:894`) surfaces them as:

```
you said:    place an order and reserve stock
source now:  check out
```

In the Studio that renders as an "out of date" badge with a reconcile button. In the generation
prompt, the correction is still used but annotated (`belief.go:825`): the model is told the source
now suggests something different and to prefer the human's statement unless instructed otherwise.

Correcting it again re-baselines `DerivedAt` to the new derived value and clears the flag.

There is a second, independent staleness check for enrich beliefs: each stores a hash of the
routine's body (`SourceHash`, via `hashSource`, `belief.go:1063`). When the body changes, the
claim is marked stale with the note "the routine's source changed since this was written, re-enrich"
(`belief.go:334`).

## Staleness cascading up

Summaries roll beliefs up a scope: service, then module, then project (`summarize.go:26`). Each
summary stores a `ChildFingerprint`, a hash over its members' `id=value;` pairs
(`childFingerprintLocked`, `summarize.go:259`). Because the value used is the corrected one where a
correction exists, a member being added, removed, or changed all move the hash.

`reattachSummariesLocked` (`summarize.go:165`) walks the scopes bottom-up, in that order
deliberately, since a module's fingerprint is computed over its service summaries and those must
be current first. A moved fingerprint marks the summary stale with "a member changed since this
summary was written, re-summarize".

**Current limitation:** only service-level summaries are ever produced. The sole
caller of `UpsertSummary` is `SummarizeServices` (`enrich/enrich.go:243`), which hardcodes the
service scope and does not set `Parent`. The module and project cascade is implemented and tested
but unreachable from the shipping CLI. Do not describe it as a working feature until a
`SummarizeModules` exists.

## Outside support for this design

A 2026 review of 87 graph based vulnerability studies (Information 17(8):730) reports that graph
methods work best when structure is represented faithfully and explicitly. It also reports that
published results are weakened by graph construction errors, incomplete repository context, and
weak explanation protocols. It asks for verifiable explanations as a minimum requirement.

A belief with a `consistent`, `contradicts`, or `needs-human` status, carrying the parser
observation it was checked against, is a verifiable explanation in that sense. The parser tier is
the faithful structure. The gating is what turns a model's claim into something a person can
check. That review is about vulnerability detection, not code generation. The overlap is in the
design principle, not the task.

## Reading it yourself

```bash
gen-enrich --root ./your-repo          # propose enrich claims, validated on write
understand-server --root ./your-repo   # Studio: read, correct, reconcile
GET  /api/present/beliefs              # beliefs, warnings, stale count, summaries
POST /api/present/beliefs/correct      # record a correction
```

The parser is the oracle. The model proposes claims, and gen-system checks them against it.
Only a human's judgment steers what gets generated.
