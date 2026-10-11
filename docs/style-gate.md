# Team style profiles and the style gate

v26.33 added this feature. This document says what a style profile is, how it reaches the model,
how the gate checks the result, and what the gate cannot do.

## The idea in one paragraph

A team already has a way of writing code. It has folder names, type suffixes, a size it
tolerates for a function, a list of things it never does. Today that knowledge lives in review
comments. gen-system turns it into a profile: a JSON file with rules. The rules go into every
prompt the coder model sees. After generation, the Understand engine checks the same rules
against the code, with no model involved. The gate checks the rules the model saw, and nothing
else. gen-system builds the prompt and the gate from one list.

## Where the rules come from

Two roads, same file.

**A human writes it.** Start from the default and edit:

```bash
gen style print > gen-style.json
```

**The tool measures an existing repository.** It reads the Understand index and derives each rule
from counts. How many files follow a case style. How many exported types in the service layer end
with `Service`. How many `fmt.Errorf` calls wrap with `%w`. The largest function that exists.
Every derived rule carries its evidence.

```bash
gen style derive ~/code/team-repo --out gen-style.json
```

The tool marks the file `"source": "derived:<path>"`. A team should read it, change what it
disagrees with, and set the source to `human`. A derived profile is a starting point and not a
decision. The beliefs system draws the same line between a parser belief and a human belief, for
the same reason. The gate should enforce what somebody stated, and it should say where each
statement came from.

## What a profile can say

| Section | Rules | Checked by the gate |
|---|---|---|
| `naming` | file case, exported and local function case, type case, short receivers, type suffix per layer | yes |
| `structure` | required directories, forbidden directories, file name pattern per layer | yes |
| `patterns.require` | `constructor`, `repository`, `service-layer`, `error-wrapping` | yes |
| `patterns.forbid` | `panic`, `global-state`, `init-func`, `reflection` | yes |
| `code` | max function lines, max branch points, max parameters, error wrapping, iteration style, allowed and forbidden imports | yes |
| `code.paradigm`, `structure.layout` | struct methods or procedural, layered or flat | prompt only |
| `prompt.notes` | free text lines for the model | prompt only |
| `prompt.exemplars` | reference functions pulled from a team repository | prompt only |

"Prompt only" means the prompt tells the model and nothing checks it. Paradigm is one of those.
There is no reliable static test for "this code is object oriented enough", so the profile does
not pretend to have one.

Every rule has a severity. Bans, structure rules and import rules default to `error`. Naming
and size rules default to `warn`. A profile can override any of them in `severity`.

## Reference code from the graph

`prompt.exemplars` is retrieval over the AST graph and not over embeddings. When gen-system asks
the coder model for the operations of an entity, it loads the profile's exemplar repository
through the Understand index once. It then ranks functions by three keys. First, words shared
between the function name and the operation verbs. Second, the preferred layer. Third, length.
Ties break on the symbol id. The top N functions go into the prompt as reference code. The pick
is the same on every run, and it needs no model.

## How the rules reach the model

Three prompts carry them.

1. The logic prompt for each entity (the operations request).
2. The operation repair prompt, when a body failed the compile gate.
3. The task repair prompt in the healer.

gen-system renders the text block from the profile's active rule list. It marks rules with error
severity `[error]` in the prompt, so the model knows which ones block.

## The gate

Two levels, in this order.

**Operation level.** The compile gate in `engine/service_logic.go` checks one model written
method body at a time. When a body compiles and a profile is active in `gate` or `autofix`
mode, the gate checks the same body against the profile. A violation goes back to the model with
the rule named, the same way a build error does. It has its own cap (`gate.max_repairs`, default
1, never above 2). The gate accepts a repaired body only if it still compiles and is clean. If it
does not, the gate keeps the compiling body, and the violation stays on record.

Style never turns logic into a stub. Only the compile gate can do that. The two gates are
separate on purpose: correctness comes first, and style is only worth arguing about on code that
runs.

**Tree level.** This runs after the validation loop and the Understand verify pass. The gate
loads the generated backend through the Understand engine. It checks every active rule over
files, symbols, spans and control flow graphs. It sorts findings by file, line, rule and symbol,
so two runs over the same tree produce the same report.

## Modes

| Mode | Prompt | Op gate | Tree gate | On error findings |
|---|---|---|---|---|
| `off` | no | no | no | nothing; identical to a run without a profile |
| `report` | yes | no | yes | gen prints them; the run continues |
| `gate` | yes | yes | yes | gen prints them, writes `STYLE-FINDINGS.txt`, and halts the run for review |
| `autofix` | yes | yes | yes | fixable files go through the repair loop once, and the gate checks them again; the run halts if errors remain |

```bash
gen idea --style gen-style.json --style-mode gate "A library lending service..."
GEN_STYLE=gen-style.json GEN_STYLE_MODE=report gen apply schema.json
gen style check ./generated-app/backend --profile gen-style.json
```

## Measuring it

`genbench genpipe --style gen-style.json --style-mode gate` runs the frozen five ideas with the
gate on and adds these metrics next to the existing ones:

| Metric | Meaning |
|---|---|
| `ops_style_violations` | compiled operation bodies that broke a rule |
| `ops_style_repair_attempts` | style repair round trips |
| `ops_style_repaired` | repairs that came back compiling and clean |
| `style_findings`, `style_errors` | tree level findings after generation, summed over ideas |
| `style_clean_ideas` | ideas whose generated backend had zero error findings |

`genbench stylecheck --root <tree> --profile gen-style.json` runs the tree gate alone as a
suite, one case per rule.

This document quotes no number from this feature. The metrics exist so a run can produce them.

## Limits

- The gate covers Go only. TypeScript and mobile output receive the prompt text, and the
  `iteration: stream` rule is prompt only.
- The branch count uses the control flow graph when one exists for the function. When it does
  not, it counts `if`, `for`, `switch`, `case`, `&&` and `||` in the text. Both approximate
  cyclomatic complexity; neither gives the exact number.
- `global-state` counts package level `var` declarations that are not error sentinels or
  compiled regular expressions. A `var` holding a constant value still counts.
- A derived profile sets size limits to the largest function that exists, so it never flags
  the code it came from. To tighten a limit, you decide and edit the number.
- The gate reads the same symbol index as everything else. It does not check a file the parsers
  cannot read, and the parse report says so.
