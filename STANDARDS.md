# go-ai standards

The judgement calls the standards-reviewer applies to go-ai, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies. The design task of
v1.ai.client writes them; until then the full Go Elemental standard applies.

- `architecture/standards/go-elemental/principles/dependencies.md`: the base module requires the
  standard library only; the `infer` CLI's nested module is the only place a dependency enters.
