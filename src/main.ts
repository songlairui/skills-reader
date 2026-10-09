import MarkdownIt from "markdown-it";
import hljs from "highlight.js/lib/common";
import DOMPurify from "dompurify";
import { Channel, isMyGo } from "mygo-runtime";
import {
  Skills,
  type FMField,
  type FMValue,
  type FileContent,
  type Library,
  type Skill,
  type SkillDetail,
  type UpdateProgress,
  type UpdateResult,
} from "./mygo";

// ---------- state ----------

type GroupMode = "source" | "prefix" | "flat";
type Filter = "all" | "outdated" | "differs" | "snapshot" | "local" | "missing";

const state = {
  root: localStorage.getItem("root") || "",
  lib: null as Library | null,
  q: "",
  mode: (localStorage.getItem("groupMode") as GroupMode) || "source",
  filter: "all" as Filter,
  selected: localStorage.getItem("selected") || "",
  detail: null as SkillDetail | null,
  file: null as FileContent | null,
  collapsed: new Set<string>(JSON.parse(localStorage.getItem("collapsed") || "[]")),
  checking: null as UpdateProgress | null,
};

const $ = <T extends HTMLElement = HTMLElement>(sel: string) => document.querySelector<T>(sel)!;
const esc = (s: unknown) =>
  String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);

function toast(msg: string) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout((toast as any).timer);
  (toast as any).timer = setTimeout(() => t.classList.remove("show"), 2200);
}

const fmtDate = (iso?: string) => {
  if (!iso) return "";
  const d = new Date(iso);
  if (isNaN(+d)) return iso;
  return d.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false });
};
const relTime = (iso?: string) => {
  if (!iso) return "";
  const s = (Date.now() - +new Date(iso)) / 1000;
  if (s < 60) return "刚刚";
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} 天前`;
  return fmtDate(iso).slice(0, 10);
};
const short = (h?: string) => (h ? h.slice(0, 7) : "");
const tildify = (p: string) => p.replace(/^\/Users\/[^/]+/, "~");
const fmtSize = (n: number) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`);

// ---------- groups ----------

function groupLabel(key: string): { title: string; sub: string; icon: string } {
  if (key === "local") return { title: "本地 skill", sub: "未在锁文件登记", icon: "folder" };
  if (key.startsWith("local:")) return { title: key.slice(6), sub: "本地仓库（无 origin）", icon: "link" };
  const parts = key.split("/");
  if (parts.length === 2) return { title: parts[1]!, sub: parts[0]!, icon: "github" };
  return { title: parts.slice(-1)[0]!, sub: parts.slice(0, -1).join("/"), icon: "git" };
}

function repoWebUrl(group: string, sourceUrl?: string): string | null {
  if (/^[\w.-]+\/[\w.-]+$/.test(group)) return `https://github.com/${group}`;
  if (sourceUrl?.startsWith("https://")) return sourceUrl.replace(/\.git$/, "");
  return null;
}

function updateOf(s: Skill): UpdateResult | undefined {
  return state.lib?.updates?.results?.[s.id];
}

function matches(s: Skill): boolean {
  const u = updateOf(s);
  switch (state.filter) {
    case "outdated":
      if (!(u && (u.status === "outdated" || u.status === "gone"))) return false;
      break;
    case "differs":
      if (s.integrity !== "differs") return false;
      break;
    case "snapshot":
      if (s.snapshot !== "changed" && s.snapshot !== "new") return false;
      break;
    case "local":
      if (s.kind !== "local" && !(s.kind === "linked" && !s.lock)) return false;
      break;
    case "missing":
      if (s.kind !== "missing") return false;
      break;
    default:
      if (s.kind === "missing") return false;
  }
  if (!state.q) return true;
  const hay = `${s.id} ${s.name} ${s.description} ${s.group} ${s.subGroup ?? ""} ${s.tags.join(" ")}`.toLowerCase();
  return state.q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}

function grouped(skills: Skill[]): { key: string; items: Skill[] }[] {
  const map = new Map<string, Skill[]>();
  const add = (k: string, s: Skill) => (map.get(k) ?? map.set(k, []).get(k)!).push(s);
  if (state.mode === "flat") return [{ key: "", items: skills }];
  if (state.mode === "prefix") {
    const all = state.lib!.skills.filter((s) => s.kind !== "missing");
    const counts = new Map<string, number>();
    for (const s of all) {
      const p = s.id.split(/[-_]/)[0]!;
      counts.set(p, (counts.get(p) ?? 0) + 1);
    }
    for (const s of skills) {
      const p = s.id.split(/[-_]/)[0]!;
      add((counts.get(p) ?? 0) >= 3 ? `prefix:${p}` : "prefix:~", s);
    }
  } else {
    for (const s of skills) add(s.group, s);
  }
  const rank = (k: string) => (k === "prefix:~" ? 3 : k === "local" ? 2 : k.startsWith("local:") ? 1 : 0);
  return [...map.entries()]
    .map(([key, items]) => ({ key, items }))
    .sort((a, b) => rank(a.key) - rank(b.key) || b.items.length - a.items.length || a.key.localeCompare(b.key));
}

// ---------- sidebar ----------

const ICONS: Record<string, string> = {
  github: '<svg viewBox="0 0 16 16"><path d="M8 0a8 8 0 0 0-2.53 15.59c.4.07.55-.17.55-.38v-1.33c-2.23.48-2.7-1.07-2.7-1.07-.36-.92-.89-1.17-.89-1.17-.73-.5.05-.49.05-.49.8.06 1.23.83 1.23.83.71 1.22 1.87.87 2.33.66.07-.52.28-.87.5-1.07-1.78-.2-3.65-.89-3.65-3.95 0-.87.31-1.59.83-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.6 7.6 0 0 1 4 0c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48v2.2c0 .21.15.46.55.38A8 8 0 0 0 8 0Z"/></svg>',
  git: '<svg viewBox="0 0 16 16"><path d="M15.7 7.3 8.7.3a1 1 0 0 0-1.4 0L5.8 1.8l1.8 1.8a1.2 1.2 0 0 1 1.5 1.5l1.8 1.8a1.2 1.2 0 1 1-.7.7L8.5 5.9v4.4a1.2 1.2 0 1 1-1-.1V5.8a1.2 1.2 0 0 1-.6-1.6L5.1 2.5.3 7.3a1 1 0 0 0 0 1.4l7 7a1 1 0 0 0 1.4 0l7-7a1 1 0 0 0 0-1.4Z"/></svg>',
  folder: '<svg viewBox="0 0 16 16"><path d="M1.5 3A1.5 1.5 0 0 1 3 1.5h3.3l1.5 1.5H13A1.5 1.5 0 0 1 14.5 4.5v8A1.5 1.5 0 0 1 13 14H3a1.5 1.5 0 0 1-1.5-1.5V3Z"/></svg>',
  link: '<svg viewBox="0 0 16 16"><path d="M6.4 9.6a.75.75 0 0 1 0-1.06l3.1-3.1a.75.75 0 1 1 1.06 1.06l-3.1 3.1a.75.75 0 0 1-1.06 0ZM4.2 8.3 3 9.5a2.5 2.5 0 0 0 3.5 3.5l1.2-1.2.96.96-1.2 1.2a3.9 3.9 0 0 1-5.5-5.5l1.2-1.2.96.96Zm7.6-.6L13 6.5A2.5 2.5 0 0 0 9.5 3L8.3 4.2l-.96-.96 1.2-1.2a3.9 3.9 0 0 1 5.5 5.5l-1.2 1.2-.96-.96Z"/></svg>',
  tag: '<svg viewBox="0 0 16 16"><path d="M1.5 2.5v4.6l7.4 7.4 5.6-5.6-7.4-7.4H2.5a1 1 0 0 0-1 1ZM5 4a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z"/></svg>',
};

function markers(s: Skill): string {
  const u = updateOf(s);
  const m: string[] = [];
  if (u?.status === "outdated") m.push('<i class="mk mk-up" title="上游有更新"></i>');
  if (u?.status === "gone") m.push('<i class="mk mk-gone" title="上游已移除或改路径"></i>');
  if (s.integrity === "differs") m.push('<i class="mk mk-diff" title="本地内容与锁文件哈希不一致"></i>');
  if (s.snapshot === "changed" || s.snapshot === "new") m.push(`<i class="mk mk-snap" title="${s.snapshot === "new" ? "快照后新增" : "快照后有改动"}"></i>`);
  if (s.kind === "linked") m.push('<span class="mk-link" title="软链">↗</span>');
  if (s.error) m.push('<span class="mk-err" title="' + esc(s.error) + '">!</span>');
  return m.join("");
}

function renderFilters() {
  const lib = state.lib;
  const sk = lib?.skills ?? [];
  const n = {
    all: sk.filter((s) => s.kind !== "missing").length,
    outdated: sk.filter((s) => ["outdated", "gone"].includes(updateOf(s)?.status ?? "")).length,
    differs: sk.filter((s) => s.integrity === "differs").length,
    snapshot: sk.filter((s) => s.snapshot === "changed" || s.snapshot === "new").length,
    local: sk.filter((s) => s.kind === "local" || (s.kind === "linked" && !s.lock)).length,
    missing: sk.filter((s) => s.kind === "missing").length,
  };
  const items: [Filter, string, string][] = [
    ["all", "全部", ""],
    ["outdated", "有更新", "up"],
    ["differs", "本地改动", "diff"],
    ["snapshot", "快照后变化", "snap"],
    ["local", "未登记", ""],
    ["missing", "锁中未装", ""],
  ];
  $("#filters").innerHTML = items
    .filter(([k]) => k === "all" || n[k] > 0 || state.filter === k)
    .map(
      ([k, label, dot]) =>
        `<button class="chip ${state.filter === k ? "on" : ""}" data-filter="${k}">${dot ? `<i class="mk mk-${dot}"></i>` : ""}${label}<b>${n[k]}</b></button>`,
    )
    .join("");
}

function renderList() {
  const lib = state.lib;
  const list = $("#list");
  if (!lib) {
    list.innerHTML = `<div class="empty-side">正在读取…</div>`;
    return;
  }
  if (!lib.exists) {
    list.innerHTML = `<div class="empty-side">目录不存在：<br><code>${esc(tildify(lib.root))}</code></div>`;
    return;
  }
  const shown = lib.skills.filter(matches);
  $("#count").textContent = `${lib.skills.filter((s) => s.kind !== "missing").length}`;
  if (!shown.length) {
    list.innerHTML = `<div class="empty-side">没有匹配的 skill</div>`;
    return;
  }
  const html: string[] = [];
  for (const g of grouped(shown)) {
    if (g.key) {
      let title: string, sub: string, icon: string;
      if (g.key.startsWith("prefix:")) {
        const p = g.key.slice(7);
        title = p === "~" ? "其他" : `${p}-*`;
        sub = "";
        icon = "tag";
      } else ({ title, sub, icon } = groupLabel(g.key));
      const collapsed = state.collapsed.has(g.key) && !state.q;
      const ups = g.items.filter((s) => updateOf(s)?.status === "outdated").length;
      html.push(
        `<div class="group ${collapsed ? "collapsed" : ""}"><button class="group-head" data-group="${esc(g.key)}" title="${esc(g.key)}">
          <span class="chev">›</span><span class="gicon">${ICONS[icon]}</span>
          <span class="gtitle">${esc(title)}</span>${sub ? `<span class="gsub">${esc(sub)}</span>` : ""}
          ${ups ? `<span class="gup">${ups}</span>` : ""}<span class="gcount">${g.items.length}</span></button><div class="group-items">`,
      );
    } else html.push(`<div class="group"><div class="group-items">`);
    let lastSub = "";
    const items = state.mode === "source" ? [...g.items].sort((a, b) => (a.subGroup ?? "").localeCompare(b.subGroup ?? "") || a.id.localeCompare(b.id)) : g.items;
    const hasSubs = state.mode === "source" && new Set(items.map((s) => s.subGroup ?? "")).size > 1;
    for (const s of items) {
      if (hasSubs && (s.subGroup ?? "") !== lastSub) {
        lastSub = s.subGroup ?? "";
        html.push(`<div class="subgroup">${esc(lastSub || "—")}</div>`);
      }
      html.push(
        `<button class="item ${s.id === state.selected ? "sel" : ""} ${s.kind === "missing" ? "missing" : ""}" data-id="${esc(s.id)}">
          <span class="iname">${esc(s.name)}${s.name !== s.id ? `<small>${esc(s.id)}</small>` : ""}</span><span class="imk">${markers(s)}</span>
          <span class="idesc">${esc(s.description || (s.kind === "missing" ? "锁文件里有记录，但不在此目录" : "（无描述）"))}</span>
        </button>`,
      );
    }
    html.push(`</div></div>`);
  }
  list.innerHTML = html.join("");
}

function renderFoot() {
  const lib = state.lib;
  const foot = $("#foot");
  if (!lib) return (foot.innerHTML = "");
  const up = lib.updates;
  const snap = lib.snapshot;
  const checking = state.checking;
  foot.innerHTML = `
    <div class="foot-row">
      <button class="path" id="rootBtn" title="在 Finder 中显示 ${esc(lib.root)}">${ICONS.folder}<span>${esc(tildify(lib.root))}</span></button>
      <button class="ghost sm" id="pickRoot" title="选择其他目录">更换</button>
      ${state.root && state.root !== "" ? `<button class="ghost sm" id="resetRoot" title="回到 ~/.agents/skills">默认</button>` : ""}
    </div>
    <div class="foot-row meta">
      ${
        lib.lock
          ? `<span title="${esc(lib.lock.path)}">锁 <b>${esc(lib.lock.path.split("/").pop())}</b> v${lib.lock.version} · ${lib.lock.count} 条</span>`
          : `<span>未找到锁文件，仅本地索引</span>`
      }
    </div>
    <div class="foot-row meta">
      ${
        snap
          ? `<span title="本地索引快照：每个 skill 目录的 git tree 哈希\n${esc(snap.path)}">快照 ${relTime(snap.takenAt)} · ${snap.changed + snap.added + snap.removed ? `<b class="warn">${snap.changed} 改 · ${snap.added} 增 · ${snap.removed} 删</b>` : "无变化"}</span>`
          : "<span>尚无快照</span>"
      }
      <button class="ghost sm" id="snapBtn" title="把当前状态记为新的索引快照">记快照</button>
    </div>
    <div class="foot-row">
      <button class="primary" id="checkBtn" ${checking || !lib.lock ? "disabled" : ""}>
        ${checking ? `<span class="spin"></span>检查中 ${checking.done}/${checking.total}` : "检查更新"}
      </button>
      <span class="meta check-meta">${
        checking
          ? esc(checking.source)
          : up
            ? `${relTime(up.checkedAt)} · ${up.outdated ? `<b class="accent">${up.outdated} 个有更新</b>` : "全部最新"}${up.errors ? ` · <span class="warn">${up.errors} 个失败</span>` : ""}`
            : lib.lock
              ? "尚未检查"
              : ""
      }</span>
    </div>`;
}

function renderSidebar() {
  renderFilters();
  renderList();
  renderFoot();
  document.querySelectorAll<HTMLButtonElement>("#groupMode button").forEach((b) => b.classList.toggle("on", b.dataset.mode === state.mode));
}

// ---------- markdown ----------

const md = new MarkdownIt({
  html: true,
  linkify: true,
  typographer: false,
  highlight(code, lang) {
    const l = (lang || "").trim().split(/\s+/)[0]!.toLowerCase();
    const alias: Record<string, string> = { sh: "bash", shell: "bash", zsh: "bash", console: "bash", ts: "typescript", js: "javascript", yml: "yaml", py: "python", golang: "go", jsonc: "json" };
    const name = alias[l] || l;
    try {
      if (name && hljs.getLanguage(name)) return hljs.highlight(code, { language: name, ignoreIllegals: true }).value;
    } catch {}
    return esc(code);
  },
});

const slugify = (s: string) =>
  s
    .trim()
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .replace(/\s+/g, "-");

type TocItem = { level: number; text: string; id: string };

function renderMarkdown(src: string, baseDir: string, skillId: string, titles: string[] = []): { html: string; toc: TocItem[] } {
  const env = {};
  const tokens = md.parse(src, env);
  // The card already shows the name: drop a leading "# name" that repeats it.
  const norm = (x: string) => x.trim().toLowerCase().replace(/[\s_-]+/g, "");
  const first = tokens.findIndex((t) => t.type !== "html_block" || t.content.trim() !== "");
  if (first >= 0 && tokens[first]?.type === "heading_open" && tokens[first]!.tag === "h1" && titles.some((t) => t && norm(t) === norm(tokens[first + 1]?.content ?? ""))) {
    tokens.splice(first, 3);
  }
  const toc: TocItem[] = [];
  const used = new Map<string, number>();
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]!;
    if (t.type === "heading_open") {
      const inline = tokens[i + 1]!;
      const text = inline.children?.map((c: { content: string }) => c.content).join("") ?? inline.content;
      let id = slugify(text) || "section";
      const n = used.get(id) ?? 0;
      used.set(id, n + 1);
      if (n) id += `-${n}`;
      t.attrSet("id", id);
      const level = Number(t.tag.slice(1));
      if (level <= 3) toc.push({ level, text, id });
    }
  }
  let html = md.renderer.render(tokens, md.options, env);
  html = DOMPurify.sanitize(html, { ADD_ATTR: ["id"], FORBID_TAGS: ["style", "form", "input", "button"] });
  const tpl = document.createElement("template");
  tpl.innerHTML = html;
  const isExternal = (u: string) => /^[a-z][a-z0-9+.-]*:/i.test(u) || u.startsWith("//");
  tpl.content.querySelectorAll("a[href]").forEach((a) => {
    const href = a.getAttribute("href")!;
    if (href.startsWith("#")) return;
    if (isExternal(href)) {
      a.setAttribute("data-external", href);
      a.classList.add("ext");
    } else {
      const [p] = href.split("#");
      const resolved = resolvePath(baseDir, decodeURIComponent(p!));
      a.setAttribute("data-file", resolved);
      a.classList.add("internal");
    }
    a.setAttribute("href", "#");
  });
  tpl.content.querySelectorAll("img[src]").forEach((img) => {
    const src = img.getAttribute("src")!;
    if (!isExternal(src) && !src.startsWith("data:")) {
      img.setAttribute("src", `skillfile://localhost/${encodeURIComponent(skillId)}/${resolvePath(baseDir, decodeURIComponent(src)).split("/").map(encodeURIComponent).join("/")}`);
    }
    img.setAttribute("loading", "lazy");
  });
  tpl.content.querySelectorAll("pre > code").forEach((code) => {
    const pre = code.parentElement!;
    const lang = [...code.classList].find((c) => c.startsWith("language-"))?.slice(9);
    pre.classList.add("code");
    const bar = document.createElement("div");
    bar.className = "code-bar";
    bar.innerHTML = `<span>${esc(lang || "")}</span><button class="copy" type="button">复制</button>`;
    pre.prepend(bar);
  });
  tpl.content.querySelectorAll("table").forEach((t) => {
    const wrap = document.createElement("div");
    wrap.className = "table-wrap";
    t.replaceWith(wrap);
    wrap.append(t);
  });
  const div = document.createElement("div");
  div.append(tpl.content);
  return { html: div.innerHTML, toc };
}

function resolvePath(baseDir: string, rel: string): string {
  const parts = (baseDir ? baseDir.split("/") : []).concat(rel.split("/"));
  const out: string[] = [];
  for (const p of parts) {
    if (!p || p === ".") continue;
    if (p === "..") out.pop();
    else out.push(p);
  }
  return out.join("/");
}

// ---------- frontmatter card ----------

const KNOWN_LABELS: Record<string, string> = {
  name: "名称",
  description: "描述",
  version: "版本",
  license: "许可",
  author: "作者",
  tags: "标签",
  keywords: "关键词",
  category: "分类",
  "allowed-tools": "允许的工具",
  "disable-model-invocation": "禁止模型自动调用",
  "user-invocable": "用户可调用",
  "argument-hint": "参数提示",
  model: "模型",
  metadata: "元数据",
  date: "日期",
  homepage: "主页",
  repository: "仓库",
};

function fmValueHTML(v: FMValue): string {
  if (v.type === "list") {
    const items = v.items ?? [];
    if (items.every((i) => i.type === "scalar")) return `<span class="chips">${items.map((i) => `<span class="chip-v">${esc(i.value)}</span>`).join("") || '<span class="muted">（空）</span>'}</span>`;
    return `<ol class="fm-list">${items.map((i) => `<li>${fmValueHTML(i)}</li>`).join("")}</ol>`;
  }
  if (v.type === "map") return `<dl class="fm-grid nested">${(v.fields ?? []).map(fmRow).join("")}</dl>`;
  if (v.tag === "bool") return `<span class="bool ${v.value === "true" ? "yes" : "no"}">${v.value === "true" ? "是" : "否"}</span>`;
  if (v.tag === "null" || v.value === undefined) return `<span class="muted">—</span>`;
  const s = v.value ?? "";
  if (/^https?:\/\/\S+$/.test(s)) return `<a href="#" data-external="${esc(s)}" class="ext">${esc(s)}</a>`;
  return `<span class="${s.length > 80 ? "long" : ""}">${esc(s)}</span>`;
}

function fmRow(f: FMField): string {
  const label = KNOWN_LABELS[f.key];
  return `<dt title="${esc(f.key)}">${label ? `${esc(label)}<code>${esc(f.key)}</code>` : `<code class="only">${esc(f.key)}</code>`}</dt><dd>${fmValueHTML(f.value)}</dd>`;
}

function frontmatterCard(d: SkillDetail): string {
  const s = d.skill;
  const fm = d.frontmatter;
  const get = (k: string) => fm.find((f) => f.key === k);
  const meta = get("metadata");
  const metaGet = (k: string) => meta?.value.fields?.find((f) => f.key === k)?.value.value;
  const badges: string[] = [];
  const kindLabel = { installed: "已安装", linked: "软链", local: "本地", missing: "未安装" }[s.kind];
  badges.push(`<span class="badge kind-${s.kind}">${kindLabel}</span>`);
  if (s.version) badges.push(`<span class="badge ver">v${esc(s.version.replace(/^v/, ""))}</span>`);
  const license = get("license")?.value.value || metaGet("license");
  if (license) badges.push(`<span class="badge">${esc(license)}</span>`);
  const author = get("author")?.value.value || metaGet("author");
  if (author) badges.push(`<span class="badge">by ${esc(author)}</span>`);
  if (get("disable-model-invocation")?.value.value === "true") badges.push(`<span class="badge warn">仅手动调用</span>`);
  if (get("user-invocable")?.value.value === "false") badges.push(`<span class="badge">不可由用户直接调用</span>`);
  for (const t of s.tags) badges.push(`<span class="badge tag">#${esc(t)}</span>`);

  const hidden = new Set(["name", "description"]);
  const rest = fm.filter((f) => !hidden.has(f.key));
  return `
  <section class="fm-card">
    <div class="fm-head">
      <h1 class="fm-title">${esc(s.name)}</h1>
      ${s.name !== s.id ? `<code class="fm-id" title="目录名">${esc(s.id)}</code>` : ""}
    </div>
    ${s.description ? `<p class="fm-desc">${esc(s.description)}</p>` : `<p class="fm-desc muted">frontmatter 中没有 description</p>`}
    <div class="badges">${badges.join("")}</div>
    ${d.frontmatterError ? `<div class="notice warn">YAML 解析失败，已按行兜底展示：<code>${esc(d.frontmatterError)}</code></div>` : ""}
    ${rest.length ? `<dl class="fm-grid">${rest.map(fmRow).join("")}</dl>` : ""}
    ${d.frontmatterRaw ? `<details class="raw"><summary>原始 frontmatter</summary><pre class="code"><code>${hljs.highlight(d.frontmatterRaw, { language: "yaml" }).value}</code></pre></details>` : ""}
  </section>`;
}

function sourceCard(d: SkillDetail): string {
  const s = d.skill;
  const rows: string[] = [];
  const row = (k: string, v: string) => rows.push(`<dt>${k}</dt><dd>${v}</dd>`);
  const e = s.lock;
  if (e) {
    const web = repoWebUrl(s.group, e.sourceUrl);
    const folder = (e.skillPath ?? "").replace(/\/?SKILL\.md$/i, "");
    row("来源", web ? `<a href="#" class="ext" data-external="${esc(web)}">${esc(s.group)}</a>` : `<code>${esc(e.source)}</code>`);
    if (e.skillPath)
      row(
        "路径",
        web && s.group.split("/").length === 2
          ? `<a href="#" class="ext" data-external="${esc(`${web}/tree/${e.ref || "HEAD"}/${folder}`)}"><code>${esc(e.skillPath)}</code></a>`
          : `<code>${esc(e.skillPath)}</code>`,
      );
    if (e.ref) row("引用", `<code>${esc(e.ref)}</code>`);
    if (e.pluginName) row("插件", esc(e.pluginName));
    const hash = e.skillFolderHash || e.computedHash || "";
    const integ =
      s.integrity === "match"
        ? `<span class="ok">✓ 与本地一致</span>`
        : s.integrity === "differs"
          ? `<span class="warn" title="本地: ${esc(s.localHash)}">≠ 本地内容不同</span>`
          : "";
    row("索引哈希", `<code title="${esc(hash)}">${esc(short(hash))}</code> <span class="muted">${hash.length === 40 ? "git tree" : "sha256"}</span> ${integ}`);
    row("安装于", esc(fmtDate(e.installedAt)));
    if (e.updatedAt && e.updatedAt !== e.installedAt) row("更新于", esc(fmtDate(e.updatedAt)));
    const u = updateOf(s);
    if (u) {
      const label = {
        current: `<span class="ok">✓ 已是最新</span>`,
        outdated: `<span class="accent">● 有更新</span> <code title="${esc(u.remoteHash)}">${esc(short(u.lockHash))} → ${esc(short(u.remoteHash))}</code>`,
        gone: `<span class="warn">上游已移除或改了路径</span>`,
        error: `<span class="warn">检查失败</span>`,
        skipped: `<span class="muted">跳过</span>`,
      }[u.status];
      row("上游", `${label}${u.message ? ` <span class="muted">${esc(u.message)}</span>` : ""} <span class="muted">· ${esc(u.via ?? "")} · ${relTime(state.lib?.updates?.checkedAt)}</span>`);
      if (u.status === "outdated" || u.status === "gone") row("更新命令", `<code class="cmd" data-copy="npx skills update ${esc(s.id)} -g">npx skills update ${esc(s.id)} -g</code>`);
    } else if (s.kind !== "missing") row("上游", `<span class="muted">尚未检查 · 点左下角「检查更新」</span>`);
  }
  if (s.kind === "linked" && s.linkTarget) {
    row("软链到", `<code>${esc(tildify(s.linkTarget))}</code>`);
    if (d.git) {
      const g = d.git;
      if (!e) {
        const web = repoWebUrl(s.group);
        row("仓库", web ? `<a href="#" class="ext" data-external="${esc(web)}">${esc(s.group)}</a>` : `<code>${esc(tildify(g.root))}</code>`);
      }
      if (g.branch) row("分支", `<code>${esc(g.branch)}</code>`);
      if (g.lastCommit) row("最后提交", `<code>${esc(g.lastCommit)}</code> ${esc(g.lastTitle)} <span class="muted">· ${esc(relTime(g.lastDate))}</span>`);
      row("工作区", g.dirty.length ? `<span class="warn">${g.dirty.length} 个未提交改动</span>` : `<span class="ok">干净</span>`);
    }
  }
  if (s.kind === "local" && !e) row("登记", `<span class="muted">不在锁文件中，无上游可比对；变化看本地快照</span>`);
  if (s.snapshot)
    row(
      "本地快照",
      { same: `<span class="ok">与快照一致</span>`, changed: `<span class="warn">快照后有改动</span>`, new: `<span class="warn">快照后新增</span>` }[s.snapshot] ?? "",
    );
  if (s.kind !== "missing") row("文件", `${s.fileCount} 个${s.modified ? ` · 最近修改 ${esc(fmtDate(s.modified))}` : ""}`);
  return `<section class="src-card"><h2 class="card-title">来源与索引</h2><dl class="kv">${rows.join("")}</dl></section>`;
}

function filesPanel(d: SkillDetail): string {
  if (!d.files.length) return "";
  const cur = state.file?.path ?? "SKILL.md";
  return `<div class="rail-block"><div class="rail-title">文件 <span>${d.files.filter((f) => !f.dir).length}</span></div><ul class="files">${d.files
    .map((f) => {
      const depth = f.path.split("/").length - 1;
      const name = f.path.split("/").pop();
      return f.dir
        ? `<li class="dir" style="--d:${depth}">${esc(name)}/</li>`
        : `<li style="--d:${depth}"><button data-file="${esc(f.path)}" class="${cur === f.path ? "on" : ""}" title="${esc(f.path)} · ${fmtSize(f.size)}">${esc(name)}</button></li>`;
    })
    .join("")}</ul></div>`;
}

function tocPanel(toc: TocItem[]): string {
  if (toc.length < 2) return "";
  const min = Math.min(...toc.map((t) => t.level));
  return `<div class="rail-block"><div class="rail-title">目录</div><ul class="toc">${toc
    .map((t) => `<li style="--l:${t.level - min}"><a href="#" data-anchor="${esc(t.id)}">${esc(t.text)}</a></li>`)
    .join("")}</ul></div>`;
}

// ---------- reader ----------

function renderTopbar() {
  const d = state.detail;
  const top = $("#topbar");
  if (!d) return (top.innerHTML = "");
  const s = d.skill;
  const g = s.group.startsWith("local") ? groupLabel(s.group).title : s.group;
  top.innerHTML = `
    <nav class="crumbs">
      <span class="crumb-g">${esc(g)}</span><span class="sep">/</span>
      ${state.file ? `<a href="#" id="backSkill">${esc(s.id)}</a><span class="sep">/</span><span>${esc(state.file.path)}</span>` : `<span>${esc(s.id)}</span>`}
    </nav>
    <div class="actions">
      ${d.realPath ? `<button class="ghost sm" id="revealBtn" title="${esc(d.realPath)}">在 Finder 中显示</button>` : ""}
      ${d.realPath ? `<button class="ghost sm" id="copyPath" title="复制 SKILL.md 路径">复制路径</button>` : ""}
    </div>`;
}

function renderPage() {
  const page = $("#page");
  const d = state.detail;
  renderTopbar();
  if (!d) {
    const lib = state.lib;
    page.innerHTML = `<div class="welcome"><h1>Skills Reader</h1><p>${lib ? `${tildify(lib.root)} · ${lib.skills.filter((s) => s.kind !== "missing").length} 个 skill` : "正在读取…"}</p><p class="muted">从左侧选一个开始阅读</p></div>`;
    return;
  }
  const s = d.skill;
  let body: { html: string; toc: TocItem[] };
  let header = "";
  if (state.file) {
    const f = state.file;
    const dir = f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/")) : "";
    header = `<div class="file-head"><button class="ghost sm" id="backSkill2">← SKILL.md</button><code>${esc(f.path)}</code><span class="muted">${fmtSize(f.size)}</span></div>`;
    if (f.binary) body = { html: `<div class="notice">二进制文件，无法预览。</div>`, toc: [] };
    else if (/\.(md|markdown|mdx)$/i.test(f.path)) {
      const split = f.text.match(/^---\n([\s\S]*?)\n---\n?/);
      body = renderMarkdown(split ? f.text.slice(split[0].length) : f.text, dir, s.id);
      if (split) header += `<details class="raw"><summary>frontmatter</summary><pre class="code"><code>${hljs.highlight(split[1]!, { language: "yaml" }).value}</code></pre></details>`;
    } else {
      const ext = f.path.split(".").pop()!.toLowerCase();
      const lang = { ts: "typescript", js: "javascript", mjs: "javascript", py: "python", sh: "bash", yml: "yaml", yaml: "yaml", json: "json", go: "go", toml: "ini", html: "xml", css: "css", rb: "ruby", rs: "rust" }[ext];
      const code = lang && hljs.getLanguage(lang) ? hljs.highlight(f.text, { language: lang, ignoreIllegals: true }).value : esc(f.text);
      body = { html: `<pre class="code file-code"><div class="code-bar"><span>${esc(lang ?? ext)}</span><button class="copy" type="button">复制</button></div><code>${code}</code></pre>`, toc: [] };
    }
    if (f.truncated) header += `<div class="notice">文件较大，只显示前 1 MB。</div>`;
  } else {
    body = d.body ? renderMarkdown(d.body, "", s.id, [s.name, s.id]) : { html: s.kind === "missing" ? "" : `<div class="notice">没有 SKILL.md</div>`, toc: [] };
  }
  page.innerHTML = `
    <div class="layout">
      <article class="reader">
        ${state.file ? header : frontmatterCard(d) + sourceCard(d)}
        <div class="markdown">${body.html}</div>
      </article>
      <aside class="rail">${tocPanel(body.toc)}${filesPanel(d)}</aside>
    </div>`;
}

async function select(id: string, opts: { keepScroll?: boolean } = {}) {
  state.selected = id;
  state.file = null;
  localStorage.setItem("selected", id);
  document.querySelectorAll(".item.sel").forEach((e) => e.classList.remove("sel"));
  document.querySelector(`.item[data-id="${CSS.escape(id)}"]`)?.classList.add("sel");
  try {
    state.detail = await Skills.read(state.lib?.root ?? state.root, id);
    const summary = state.lib?.skills.find((s) => s.id === id);
    if (state.detail && summary) state.detail.skill.snapshot = summary.snapshot;
  } catch (err) {
    state.detail = null;
    toast(String((err as Error).message ?? err));
  }
  renderPage();
  if (!opts.keepScroll) $("#scroller").scrollTop = 0;
}

async function openFile(path: string) {
  const d = state.detail;
  if (!d) return;
  if (path === "SKILL.md" || path === "") {
    state.file = null;
    renderPage();
    $("#scroller").scrollTop = 0;
    return;
  }
  try {
    state.file = await Skills.readFile(state.lib!.root, d.skill.id, path);
    renderPage();
    $("#scroller").scrollTop = 0;
  } catch (err) {
    toast(`打不开 ${path}：${(err as Error).message ?? err}`);
  }
}

// ---------- data ----------

async function load(root = state.root) {
  try {
    state.lib = await Skills.scan(root);
  } catch (err) {
    toast(String((err as Error).message ?? err));
    return;
  }
  renderSidebar();
  const lib = state.lib!;
  const exists = lib.skills.some((s) => s.id === state.selected);
  if (exists) {
    await select(state.selected, { keepScroll: true });
    document.querySelector(`.item[data-id="${CSS.escape(state.selected)}"]`)?.scrollIntoView({ block: "nearest" });
  }
  else {
    const first = lib.skills.find(matches);
    if (first) await select(first.id);
    else {
      state.detail = null;
      renderPage();
    }
  }
}

async function checkUpdates() {
  if (!state.lib || state.checking) return;
  state.checking = { done: 0, total: 0, source: "准备中…" };
  renderFoot();
  const ch = new Channel<UpdateProgress>((p) => {
    state.checking = p;
    renderFoot();
  });
  try {
    const report = await Skills.checkUpdates(state.lib.root, ch);
    state.checking = null;
    if (report) state.lib.updates = report;
    toast(report ? `检查完成：${report.outdated} 个有更新${report.errors ? `，${report.errors} 个失败` : ""}` : "检查完成");
  } catch (err) {
    state.checking = null;
    toast(`检查失败：${(err as Error).message ?? err}`);
  }
  renderSidebar();
  if (state.detail) renderPage();
}

// ---------- events ----------

let qTimer = 0;
$("#q").addEventListener("input", (e) => {
  clearTimeout(qTimer);
  qTimer = window.setTimeout(() => {
    state.q = (e.target as HTMLInputElement).value.trim();
    renderList();
  }, 60);
});

$("#groupMode").addEventListener("click", (e) => {
  const b = (e.target as HTMLElement).closest<HTMLButtonElement>("button[data-mode]");
  if (!b) return;
  state.mode = b.dataset.mode as GroupMode;
  localStorage.setItem("groupMode", state.mode);
  renderSidebar();
});

$("#filters").addEventListener("click", (e) => {
  const b = (e.target as HTMLElement).closest<HTMLButtonElement>("[data-filter]");
  if (!b) return;
  state.filter = b.dataset.filter as Filter;
  renderSidebar();
});

$("#list").addEventListener("click", (e) => {
  const t = e.target as HTMLElement;
  const head = t.closest<HTMLButtonElement>(".group-head");
  if (head) {
    const k = head.dataset.group!;
    state.collapsed.has(k) ? state.collapsed.delete(k) : state.collapsed.add(k);
    localStorage.setItem("collapsed", JSON.stringify([...state.collapsed]));
    head.parentElement!.classList.toggle("collapsed");
    return;
  }
  const item = t.closest<HTMLButtonElement>(".item");
  if (item) select(item.dataset.id!);
});

$("#foot").addEventListener("click", async (e) => {
  const t = (e.target as HTMLElement).closest<HTMLElement>("button");
  if (!t || !state.lib) return;
  if (t.id === "rootBtn") Skills.reveal(state.lib.root);
  if (t.id === "pickRoot") {
    const p = await Skills.pickRoot(state.lib.root);
    if (p) {
      state.root = p;
      localStorage.setItem("root", p);
      state.selected = "";
      await load(p);
    }
  }
  if (t.id === "resetRoot") {
    state.root = "";
    localStorage.removeItem("root");
    state.selected = "";
    await load("");
  }
  if (t.id === "snapBtn") {
    state.lib = await Skills.takeSnapshot(state.lib.root);
    renderSidebar();
    if (state.detail) await select(state.selected, { keepScroll: true });
    toast("已记录新的索引快照");
  }
  if (t.id === "checkBtn") checkUpdates();
});

document.addEventListener("click", async (e) => {
  const t = e.target as HTMLElement;
  const ext = t.closest<HTMLElement>("[data-external]");
  if (ext) {
    e.preventDefault();
    Skills.openExternal(ext.dataset.external!);
    return;
  }
  const file = t.closest<HTMLElement>("#page [data-file]");
  if (file) {
    e.preventDefault();
    openFile(file.dataset.file!);
    return;
  }
  const anchor = t.closest<HTMLElement>("[data-anchor]");
  if (anchor) {
    e.preventDefault();
    document.getElementById(anchor.dataset.anchor!)?.scrollIntoView({ behavior: "smooth", block: "start" });
    return;
  }
  const a = t.closest<HTMLAnchorElement>(".markdown a[href^='#']");
  if (a && a.getAttribute("href")!.length > 1) {
    e.preventDefault();
    document.getElementById(decodeURIComponent(a.getAttribute("href")!.slice(1)))?.scrollIntoView({ behavior: "smooth" });
    return;
  }
  if (t.closest("#backSkill, #backSkill2")) {
    e.preventDefault();
    openFile("SKILL.md");
    return;
  }
  if (t.closest("#revealBtn") && state.detail) Skills.reveal(state.file ? `${state.detail.realPath}/${state.file.path}` : `${state.detail.realPath}/SKILL.md`);
  if (t.closest("#copyPath") && state.detail) {
    await navigator.clipboard.writeText(`${state.detail.path}/${state.file?.path ?? "SKILL.md"}`);
    toast("已复制路径");
  }
  const copy = t.closest<HTMLButtonElement>(".copy");
  if (copy) {
    const code = copy.closest("pre")?.querySelector("code")?.textContent ?? "";
    await navigator.clipboard.writeText(code);
    copy.textContent = "已复制";
    setTimeout(() => (copy.textContent = "复制"), 1200);
  }
  const cmd = t.closest<HTMLElement>("[data-copy]");
  if (cmd) {
    await navigator.clipboard.writeText(cmd.dataset.copy!);
    toast("已复制命令");
  }
});

document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    $<HTMLInputElement>("#q").focus();
    $<HTMLInputElement>("#q").select();
    return;
  }
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "r") {
    e.preventDefault();
    load(state.lib?.root ?? state.root).then(() => toast("已重新扫描"));
    return;
  }
  const inInput = (e.target as HTMLElement).matches("input, textarea");
  if ((e.key === "ArrowDown" || e.key === "ArrowUp") && (inInput || !(e.target as HTMLElement).closest(".scroller"))) {
    const items = [...document.querySelectorAll<HTMLButtonElement>("#list .group:not(.collapsed) .item, #list .group .item")].filter((el, i, arr) => arr.indexOf(el) === i && el.offsetParent);
    if (!items.length) return;
    e.preventDefault();
    let i = items.findIndex((el) => el.dataset.id === state.selected);
    i = e.key === "ArrowDown" ? Math.min(items.length - 1, i + 1) : Math.max(0, i - 1);
    items[i]!.scrollIntoView({ block: "nearest" });
    select(items[i]!.dataset.id!);
  }
  if (e.key === "Escape" && inInput) {
    (e.target as HTMLInputElement).value = "";
    state.q = "";
    renderList();
  }
});

let lastScan = Date.now();
window.addEventListener("focus", () => {
  // Pick up skills installed or edited while the app was in the background.
  if (state.lib && !state.checking && Date.now() - lastScan > 5000) {
    lastScan = Date.now();
    load(state.lib.root);
  }
});

if (!isMyGo()) document.documentElement.classList.add("browser");
renderPage();
load();
