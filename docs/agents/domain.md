# Domain Docs

How engineering skills should consume this repo's domain documentation.

**Seeded by `mainline new` / adopt** as single-context. Upgrade layout with `/setup-work-tracer` only if this becomes a multi-context monorepo.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root
- **`docs/adr/`** — ADRs that touch the area you're about to work in

If glossary terms or ADRs are thin, **proceed**. Don't block work to invent a full glossary upfront; `/domain-modeling` (via `/grill-with-docs`) fills them when terms or decisions actually resolve.

## File structure

Single-context (this repo's default):

```
/
├── CONTEXT.md
├── docs/adr/
└── …
```

## Use the glossary's vocabulary

When your output names a domain concept, use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding.
