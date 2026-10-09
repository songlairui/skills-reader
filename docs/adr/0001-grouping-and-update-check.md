# 0001 · skill 分组与更新检查沿用 skills CLI 的锁文件

日期：2026-10-08 · 状态：已采纳

## 背景

`~/.agents/skills` 由 vercel-labs/skills CLI（`npx skills add … -g`）安装，CLI 在 `~/.agents/.skill-lock.json`（v3，`$XDG_STATE_HOME/skills/` 优先）里为每个 skill 记录 `source`、`sourceType`、`sourceUrl`、`ref`、`skillPath`、`skillFolderHash`、`pluginName`、`installedAt`、`updatedAt`。目录里还有指向本地仓库的软链和未登记的手工目录。项目级的 `skills-lock.json`（`computedHash`）同理。

## 决定

**分组**（skill group）：

1. 锁里有记录 → 组 = 归一化的 `source`（GitHub 为 `owner/repo`，其他 git 主机为 `host/owner/repo`）；组内子分类 = 仓库里 skill 目录的上一级目录名（如 `skills/engineering/tdd` → `engineering`），没有则用 `pluginName`。
2. 软链且锁里没有 → 找到目标所在 git 仓库，读 `.git/config` 的 origin 归一化后作为组，所以指向本地 `~/code/my-skills` 仓库的软链和锁里来自同一 `owner/my-skills` 的条目落在同一组；无 origin 时为「本地仓库 · 目录名」。
3. 其余 → 「本地 skill（未登记）」。
4. 另提供「按前缀」（同前缀 ≥3 个成组，如 `principle-*`、`better-*`）与「平铺」。

**索引版本 / 更新检查**（与 `npx skills check` 同一套哈希）：

- 本地一致性：对每个 skill 目录在本地算 git tree 哈希（与 `git rev-parse HEAD:<dir>` 相同算法），与锁中 40 位 `skillFolderHash` 比；64 位哈希（git 源 / 项目锁）按 CLI 的 `computeSkillFolderHash`（sha256(相对路径+内容)，按 CLDR root 排序近似 `localeCompare`）比。
- 上游：按 `source + ref` 分组。GitHub 源用 `gh api`（无 gh 时匿名 REST）读 skill 目录父目录的 contents 列表，取目录的 tree sha；仓库根目录的 skill 取提交的 tree sha。失败或非 GitHub 源时 `git clone --depth 1` 到临时目录再算哈希（SSH 用 BatchMode，不弹密码）。结果为 已是最新 / 有更新 / 上游已不在该路径 / 失败，存到应用数据目录，下次启动直接显示。
- 应用只读不改：不替用户执行更新，只给出 `npx skills update <id> -g`。
- 本地索引快照：应用自己在数据目录保存每个 skill 的 tree 哈希快照（首次启动自动建立，「记快照」更新），用来标出快照后新增 / 改动 / 删除——覆盖锁文件管不到的软链与本地 skill。

## 后果

- 「本地改动」也会因安装时被过滤的文件（如 `metadata.json`）或安装后上游变动等原因出现，界面上写作「本地内容与锁哈希不一致」，不断言是谁改的。
- 上游检查依赖网络与 gh 登录；私有 git 源要求 SSH key 可用。
