# goal · harness.adoption-go-ai

- **State:** building
- **Task:** adopt
- **Branch:** adopt

## Tasks

1. [ ] adopt

## Task brief · adopt

**Problem**

go-ai predates marathon 0.22.0: the check passes while lint-context fails, the design review of
built work sits in context/ at nine times its budget, about 3,900 comment lines fall outside the
five kinds, suppressions need no reason, and two pointers into the coordinator are stale.

**Behaviors**
1. The tool pin puts lint-context and lint-references on PATH, and currency reports a newer
   marathon tag.
2. The check runs what it runs today plus both lints, running both before failing. A lint missing
   from PATH fails with a message naming `mise install`. The description names both lints.
3. lint-context passes: the seven built capabilities are `(built)` entries linking their homes; the
   later-minor candidates sit in one note under a `(planned)` entry; the Cerebro section is gone;
   the context README holds the vision and the map.
4. The design review lives on a docs page. Its Evaluation, Surface, and Modules sections keep their
   names, and their findings and decisions keep their numbers and wording; it drops the change
   list, the per-task table, the signature blocks, and the module tree. Every go-ai pointer to the
   review names the new page.
5. Every comment in configuration and scripts is one of the five kinds: headers are a short
   description plus `See` lines, with their reasoning in the README or the design page; script
   sections are named functions.
6. Every Go comment is one of the five kinds: package comments and exported docs stay; doc comments
   on unexported identifiers and test functions go, their reasoning moving into names, exported
   docs, or the design page. Behavior and the exported API are unchanged.
7. golangci-lint requires every suppression to name its linter and give a reason (nolintlint with
   require-explanation and require-specific).
8. STANDARDS.md states a comment rule covering source, tests, scripts, and every configuration
   file, and its source line names the design page.
9. Every pointer to the coordinator names `standards-lab/roadmap/index.toml`, or the coordinator
   docs page that holds the name map.
10. CI passes.

**Test seams**
- `mise run check`, which builds and tests both modules and then runs both lints.

**Slices**
1. Context: the design page, the capability map, the candidates note, the pointers.
2. Configuration, scripts, suppression rule, STANDARDS comment rule.
3. Go comments: root package, protocol, internal, aitest.
4. Go comments: format and its three formats.
5. Go comments: integration tier and tools/infer.
6. Pin and check.

**Out of scope**

The coordinator edit (pending); any API, behavior, go.mod, release, or CHANGELOG change; the
later-minor candidates themselves; shellcheck; the spikes goal's move of spike-harness-driver's
findings note.

**Door**

Two-way. Nothing is tagged.

## Progress

slices 6/6 committed · standards ✓ · spec ✓ · editor ✓

## Decisions

None.

## Pending edits

- coordinator · docs/ai-strategy.md:222-223: go-ai's `context/design.md` becomes `docs/design.md`,
  and "that note's" becomes "that page's"; the section names and numbers it cites don't change.
