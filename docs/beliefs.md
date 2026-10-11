# Beliefs

A belief is one statement the system holds about one routine. Beliefs come from three sources.
The sources are not equal, and the design rests on that difference.

- **parser.** The offline parse derives it. Deterministic. The system rebuilds it from scratch on
  every load and never stores it on disk.
- **enrich.** A local model proposes it. The system stores it and checks it against what the
  parser observed before it can go anywhere near generation.
- **human.** You corrected it. The system stores it. This is the only source that steers
  generation.

Source: `generator/internal/belief` (`belief.go`, `validate.go`, `summarize.go`). The package
imports only `ast`, `flow`, and `symbols`, so both halves of the system can depend on it.

## The record

`belief.go:113` defines `Belief`. These are the fields this page relies on:

| Field | JSON | Meaning |
|---|---|---|
| `ID` | `id` | `<kind>:<symbolID>` for parser, `enrich:<kind>:<symbolID>` for enrich, `summary:<scope>:<name>` for summaries |
| `Kind` | `kind` | `inputs`, `calls`, `effect`, `data-change`, `action`, `computation`, `precondition`, `summary` |
| `Object` | `object` | What the machine derived |
| `CorrectedTo` | `corrected_to` | What the human said instead |
| `Source` | `source` | `parser`, `enrich`, or `human` |
| `HumanVerified` | `human_verified` | A person signed off on this |
| `Validated` | `validated` | `consistent`, `contradicts`, or `needs-human`. Empty for parser and human beliefs |
| `ValidationNote` | `validation_note` | Why the validator chose that mark, in English |
| `Stale` | `stale` | The source changed after the human corrected it |
| `SourceNow` | `source_now` | What the machine derives *now*, stored next to the human's value |

`Object` and `CorrectedTo` coexist on purpose. The human's value never overwrites the machine's
answer. Both stay on the record, and that makes the staleness check below possible.

`Confidence` (`belief.go:126`) has a JSON tag, and no shipping code assigns it. It always
serializes as `0`. The struct keeps it for wire compatibility. Do not read it as a probability.

## What is on disk

`<project-root>/.gen/beliefs.json`. It holds **three** maps, and parser beliefs are not among them
(`belief.go:1034`):

```json
{
  "corrections":    { "<beliefID>": { "belief_id": "...", "corrected_to": "...", "derived_at": "..." } },
  "enrich_beliefs": { "<beliefID>": { ... } },
  "summaries":      { "<beliefID>": { ... } }
}
```

Parser beliefs never outlive a load (`belief.go:267`: `RebuildFromIndex` wipes the map on every
load). If the source changed, the derived belief changes with it. No stale cache can disagree with
the code.

## A worked example

Take `OrderService.Checkout`, a method that writes an order and decrements stock.

**Step 1. The parser derives, deterministically.** `deriveParserBeliefs` (`belief.go:504`) emits up
to five beliefs per callable. Two of them here:

```
effect:internal/order/service.go#method:OrderService.Checkout
  touches  →  db

action:internal/order/service.go#method:OrderService.Checkout
  appears_to  →  "checkout"
```

The action is a shallow guess from the name. `shallowAction` (`belief.go:705`) splits the leading
verb: `Checkout` becomes `checkout`, `AdjustStock` becomes `adjust stock`. The predicate
`appears_to` marks it as a guess. The parser infers the effect conservatively from callee names.

**Step 2. The model proposes more, and gen-system checks it.** Run `gen-enrich`, and a local model
reads the body and proposes richer claims. Before it stores each one, `validateEnrich`
(`validate.go:58`) checks it against the parser's facts. Three rules:

```go
case KindComputation, KindPrecondition:
    b.Validated = "needs-human"
    b.ValidationNote = "semantic claim: verify against the source by hand"
```

No mechanical check can test a claim about what a computation *means*, so the validator never
marks it consistent. It waits for a person.

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

The persistence detector (`validate.go:97`) is conservative on purpose. It vetoes on
`in-memory`, `cache`, `temporary` before it starts. It requires a whole-word verb plus a
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

If the parser had seen no db effect, the first claim would read `contradicts` instead. Its note
would name the observation it failed against.

**Step 3. Contradicting claims never steer generation.** A guard method exists,
`Belief.IsBlockedForSteering()` (`validate.go:180`). It returns true for enrich beliefs marked
`contradicts`. It has no caller in the shipping code, and its own comment says so.

It needs no caller. Only `CorrectionContext` (`belief.go:821`) ever feeds beliefs into a
generation prompt, and it filters on a stricter condition:

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

`Correct` (`belief.go:960`) records your value, flips `Source` to `human`, sets `HumanVerified`,
and stores `DerivedAt`. `DerivedAt` is a snapshot of what the machine derived at the moment you
disagreed with it. The whole staleness mechanism rests on it.

From now on the planner sees your sentence. A header above it tells the model that a person
reviewed these lines and that they are authoritative.

**Step 5. The source changes, and the belief goes stale.** Someone renames the method, or changes
what it calls, and the parser now derives something different. On the next load,
`applyCorrectionLocked` (`belief.go:770`) compares:

```go
case c.DerivedAt != freshDerived:
    b.Stale = true
    b.SourceNow = freshDerived
```

The system keeps your answer and stores the fresh derived value in `SourceNow`. It flags the
belief and shows **both values**. `Warnings()` (`belief.go:913`) lists them as:

```
you said:    place an order and reserve stock
source now:  check out
```

The Studio shows this as an "out of date" badge with a reconcile button. The generation prompt
still uses the correction, with a note (`belief.go:845`). The note tells the model that the source
now suggests something different. It also tells the model to prefer the human's statement unless
an instruction says otherwise.

Correcting it again re-baselines `DerivedAt` to the new derived value and clears the flag.

Enrich beliefs have a second, independent staleness check. Each one stores a hash of the
routine's body (`SourceHash`, via `hashSource`, `belief.go:1093`). When the body changes, the
system marks the claim stale with the note "the routine's source changed since this was written,
re-enrich" (`belief.go:322`).

## Staleness cascading up

Summaries roll beliefs up a scope: service, then module, then project (`summarize.go:30`). Each
summary stores a `ChildFingerprint`, a hash over its members' `id=value;` pairs
(`childFingerprintLocked`, `summarize.go:284`). The hash uses the corrected value where a
correction exists. Adding, removing or changing a member moves the hash.

`reattachSummariesLocked` (`summarize.go:171`) walks the scopes bottom-up, in that order on
purpose. A module's fingerprint covers its service summaries, so those must be current first. A
moved fingerprint marks the summary stale with "a member changed since this summary was written,
re-summarize".

**Current limitation:** the code produces only service-level summaries. The sole caller of
`UpsertSummary` is `SummarizeServices` (`enrich/enrich.go:250`), which hardcodes the service scope
and does not set `Parent`. The code implements and tests the module and project cascade, but the
shipping CLI cannot reach it. Do not describe it as a working feature until a `SummarizeModules`
exists.

## Outside support for this design

A 2026 review of 87 graph based vulnerability studies (Information 17(8):730) reports that graph
methods work best when they represent structure faithfully and explicitly. It also reports that
graph construction errors, incomplete repository context and weak explanation protocols weaken
published results. It asks for verifiable explanations as a minimum requirement.

A belief with a `consistent`, `contradicts`, or `needs-human` status carries the parser
observation that gen-system checked it against. That makes it a verifiable explanation in that
sense. The parser tier is the faithful structure. The gating turns a model's claim into something
a person can check. That review covers vulnerability detection and not code generation. The
overlap is the design principle and not the task.

## Reading it yourself

```bash
gen-enrich --root ./your-repo          # propose enrich claims, checked on write
understand-server --root ./your-repo   # Studio: read, correct, reconcile
GET  /api/present/beliefs              # beliefs, warnings, stale count, summaries
POST /api/present/beliefs/correct      # record a correction
```
