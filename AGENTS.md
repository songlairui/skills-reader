# skills-reader — Agent 必读

## Work system

- **status: ready** — 最小可寻址工作面已由 `mainline new` 种下（默认 local-markdown）
- **查询 / 行动 / 取证**落点：
  - issue tracker → [`docs/agents/issue-tracker.md`](docs/agents/issue-tracker.md)（`.scratch/`）
  - domain docs → [`docs/agents/domain.md`](docs/agents/domain.md) · 根 [`CONTEXT.md`](CONTEXT.md) · [`docs/adr/`](docs/adr/)
- **升级**（换 GitLab/GitHub issues、改 triage labels、multi-context）：跑 `/setup-work-tracer`（或 `/setup-matt-pocock-skills`）。只填空，不推倒已有内容，除非用户明确改 tracker。
- **若 status 被标成 `pending`**：只允许 kickoff / setup-work-tracer / 写 START；禁止 `to-tickets` / `implement` / 大改代码。默认新建项目**不是** pending。

## 缘起

- [`README.md`](README.md) · [`docs/adr/0001-grouping-and-update-check.md`](docs/adr/0001-grouping-and-update-check.md)

## Agent skills

### Issue tracker

Local markdown under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Domain docs

Single-context: root `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.
