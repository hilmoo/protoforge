// Generates TypeSpec models/enums from a Drizzle schema directory.
// Embedded in protoforge and run inside the user's project, so bare imports
// such as `drizzle-orm` resolve from that project's node_modules.
// Parameters come as one JSON argument (see Params).
import { mkdirSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { getTableColumns, is } from "drizzle-orm";
import { PgTable, getTableConfig, isPgEnum } from "drizzle-orm/pg-core";
import type { PgEnum } from "drizzle-orm/pg-core";

type Params = {
  schemaDir: string;
  outFile: string;
  namespace: string;
  readOnlyColumns: string[];
  requireDocs: "warn" | "error" | "off";
  header: string[];
};

const params: Params = JSON.parse(process.argv[process.argv.length - 1]);
const schemaDir = resolve(params.schemaDir);
const outFile = resolve(params.outFile);

const pascal = (s: string) =>
  s
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((p) => p[0].toUpperCase() + p.slice(1))
    .join("");
const camel = (s: string) => {
  const p = pascal(s);
  return p[0].toLowerCase() + p.slice(1);
};
const singular = (s: string) =>
  s.endsWith("ies") ? s.slice(0, -3) + "y" : s.endsWith("s") ? s.slice(0, -1) : s;

// ---------------------------------------------------------------------------
// Column docs
//
// Docs come from comments on the column's property in the schema source:
// `//` lines directly above it (no blank line in between), a `/** ... */` block
// directly above it, or a `//` comment at the end of its line. Plain lines become
// `@doc`; lines starting with `@example <typespec value>` become `@example(...)`.
//
// The source is read with a small TypeScript tokenizer/parser (no regex over
// lines), so column objects are found however they are written:
//   pgTable("t", { ... }), pgTable("t", (t) => ({ ... })), schema.table("t", { ... })
// and spreads are resolved with JavaScript semantics (a later member overrides an
// earlier one), recursively, across files:
//   ...baseColumns            const / exported object in any file (incl. aliased imports)
//   ...ns.baseColumns         `import * as ns`
//   ...timestamps()           helper function / arrow returning an object
//   ...{ a: x }               inline object
// ---------------------------------------------------------------------------

type ColDoc = { doc: string; examples: string[] };

function toColDoc(lines: string[]): ColDoc {
  const examples = lines.filter((l) => l.startsWith("@example ")).map((l) => l.slice(9).trim());
  const doc = lines
    .filter((l) => !l.startsWith("@"))
    .join(" ")
    .trim();
  return { doc, examples };
}

// --- tokenizer --------------------------------------------------------------

type Tok = {
  kind: "ident" | "string" | "template" | "number" | "punct" | "regex" | "line" | "block";
  value: string; // for comments: the text inside the markers
  line: number; // line of the first character
  endLine: number; // line of the last character
};

function tokenize(src: string): Tok[] {
  const toks: Tok[] = [];
  let i = 0;
  let line = 1;
  const n = src.length;
  const push = (kind: Tok["kind"], value: string, startLine: number) =>
    toks.push({ kind, value, line: startLine, endLine: line });
  const lastSignificant = () => {
    for (let k = toks.length - 1; k >= 0; k--) if (toks[k].kind !== "line" && toks[k].kind !== "block") return toks[k];
    return undefined;
  };
  const regexAllowed = () => {
    const t = lastSignificant();
    if (!t) return true;
    if (t.kind === "punct") return !")]}".includes(t.value);
    return t.kind === "ident" && ["return", "typeof", "case", "do", "else", "in", "of", "new", "delete", "void", "throw", "yield", "await"].includes(t.value);
  };
  // Scans a string/template body starting after the opening quote; returns the index after the closing quote.
  const scanQuoted = (q: string, from: number): number => {
    let j = from;
    while (j < n) {
      const c = src[j];
      if (c === "\\") {
        j += 2;
        continue;
      }
      if (c === "\n") line++;
      if (c === q) return j + 1;
      if (q === "`" && c === "$" && src[j + 1] === "{") {
        j = scanBalanced(j + 2);
        continue;
      }
      j++;
    }
    return j;
  };
  // Skips a `${ ... }` expression, honouring nested strings, templates and braces.
  const scanBalanced = (from: number): number => {
    let depth = 1;
    let j = from;
    while (j < n && depth > 0) {
      const c = src[j];
      if (c === "'" || c === '"' || c === "`") {
        j = scanQuoted(c, j + 1);
        continue;
      }
      if (c === "\n") line++;
      if (c === "{") depth++;
      else if (c === "}") depth--;
      j++;
    }
    return j;
  };
  while (i < n) {
    const c = src[i];
    if (c === "\n") {
      line++;
      i++;
      continue;
    }
    if (c === " " || c === "\t" || c === "\r" || c === "\f" || c === "\v" || c === "﻿") {
      i++;
      continue;
    }
    const start = line;
    if (c === "/" && src[i + 1] === "/") {
      let j = i + 2;
      while (j < n && src[j] !== "\n") j++;
      push("line", src.slice(i + 2, j).replace(/\r$/, ""), start);
      i = j;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      let j = src.indexOf("*/", i + 2);
      if (j < 0) j = n;
      const body = src.slice(i + 2, j);
      for (const ch of body) if (ch === "\n") line++;
      push("block", body, start);
      i = j + 2;
      continue;
    }
    if (c === "'" || c === '"' || c === "`") {
      const j = scanQuoted(c, i + 1);
      push(c === "`" ? "template" : "string", src.slice(i + 1, j - 1), start);
      i = j;
      continue;
    }
    if (/[A-Za-z_$]/.test(c) || c.charCodeAt(0) > 127) {
      let j = i + 1;
      while (j < n && (/[\w$]/.test(src[j]) || src.charCodeAt(j) > 127)) j++;
      push("ident", src.slice(i, j), start);
      i = j;
      continue;
    }
    if (/[0-9]/.test(c) || (c === "." && /[0-9]/.test(src[i + 1] ?? ""))) {
      let j = i + 1;
      while (j < n && /[\w.]/.test(src[j])) j++;
      push("number", src.slice(i, j), start);
      i = j;
      continue;
    }
    if (c === "/" && regexAllowed()) {
      let j = i + 1;
      let inClass = false;
      while (j < n && src[j] !== "\n") {
        if (src[j] === "\\") j++;
        else if (src[j] === "[") inClass = true;
        else if (src[j] === "]") inClass = false;
        else if (src[j] === "/" && !inClass) break;
        j++;
      }
      j++;
      while (j < n && /[a-z]/i.test(src[j])) j++;
      push("regex", src.slice(i, j), start);
      i = j;
      continue;
    }
    if (src.startsWith("...", i)) {
      push("punct", "...", start);
      i += 3;
      continue;
    }
    if (src.startsWith("=>", i)) {
      push("punct", "=>", start);
      i += 2;
      continue;
    }
    push("punct", c, start);
    i++;
  }
  return toks;
}

// --- parser -----------------------------------------------------------------

type Member =
  | { kind: "prop"; key: string; doc: ColDoc | undefined; shorthand: boolean }
  | { kind: "spread"; expr: Expr };

type Expr =
  | { kind: "object"; members: Member[] }
  | { kind: "fn"; returns: Expr | undefined }
  | { kind: "ref"; path: string[] }
  | { kind: "call"; callee: string[]; calls: Expr[][] } // calls: argument lists along a chain a(...).b(...)
  | { kind: "unknown" };

type Decl = { expr: Expr; doc: ColDoc | undefined };
type Import = { spec: string; imported: string }; // imported "*" = namespace import

type SourceFile = {
  path: string;
  decls: Map<string, Decl>; // top-level const/let/var/function, by local name
  exportsAs: Map<string, string>; // export name -> local name (`export { a as b }`)
  imports: Map<string, Import>; // local name -> import
};

const OPEN: Record<string, string> = { "(": ")", "[": "]", "{": "}" };

class Parser {
  // Comment tokens are kept out of `t` and indexed by position for doc lookup.
  t: Tok[] = [];
  comments: Tok[][] = []; // comments[i] = comment tokens immediately before t[i]
  trailingAfter: Tok[][] = []; // comments on the same line right after t[i]

  constructor(all: Tok[]) {
    let pending: Tok[] = [];
    for (const tok of all) {
      if (tok.kind === "line" || tok.kind === "block") {
        const prev = this.t[this.t.length - 1];
        if (prev && pending.length === 0 && tok.line === prev.endLine) {
          this.trailingAfter[this.t.length - 1].push(tok);
        } else {
          pending.push(tok);
        }
        continue;
      }
      this.comments.push(pending);
      this.trailingAfter.push([]);
      this.t.push(tok);
      pending = [];
    }
    this.comments.push(pending);
  }

  is(i: number, value: string) {
    const tok = this.t[i];
    return tok !== undefined && tok.kind === "punct" && tok.value === value;
  }

  // Index of the bracket closing the one at i.
  close(i: number): number {
    let depth = 0;
    for (let j = i; j < this.t.length; j++) {
      const tok = this.t[j];
      if (tok.kind !== "punct") continue;
      if (OPEN[tok.value]) depth++;
      else if ((tok.value === ")" || tok.value === "]" || tok.value === "}") && --depth === 0) return j;
    }
    return this.t.length - 1;
  }

  // Splits [from, to) at depth-0 occurrences of sep.
  split(from: number, to: number, sep: string): [number, number][] {
    const parts: [number, number][] = [];
    let start = from;
    for (let j = from; j < to; j++) {
      if (this.t[j].kind === "punct" && OPEN[this.t[j].value]) {
        j = this.close(j);
        continue;
      }
      if (this.is(j, sep)) {
        parts.push([start, j]);
        start = j + 1;
      }
    }
    if (start < to) parts.push([start, to]);
    return parts;
  }

  // Doc for the item starting at token `first` and ending before `end` (exclusive).
  docFor(first: number, last: number): ColDoc | undefined {
    const lines: string[] = [];
    // Leading: the comment run directly above, with no blank line in between.
    const before = this.comments[first];
    const run: Tok[] = [];
    let nextLine = this.t[first].line;
    for (let k = before.length - 1; k >= 0; k--) {
      const c = before[k];
      if (c.endLine !== nextLine - 1) break;
      run.unshift(c);
      nextLine = c.line;
    }
    for (const c of run) {
      if (c.kind === "line") lines.push(c.value.replace(/^\s/, ""));
      else lines.push(...blockLines(c.value));
    }
    // Trailing: a `//` comment at the end of the item's first or last line
    // (after the item or after its comma).
    const firstLine = this.t[first].line;
    const lastLine = this.t[last].endLine;
    for (let j = first; j <= last + 1 && j < this.t.length; j++) {
      for (const c of this.trailingAfter[j] ?? []) {
        if (c.kind === "line" && (c.line === firstLine || c.line === lastLine) && (j <= last || this.is(j, ","))) {
          lines.push(c.value.replace(/^\s*/, ""));
        }
      }
    }
    return lines.length ? toColDoc(lines) : undefined;
  }

  // Parses the expression in [from, to).
  expr(from: number, to: number): Expr {
    // Drop trailing `as const`, `as X`, `satisfies X`.
    for (let j = from; j < to; j++) {
      if (this.t[j].kind === "punct" && OPEN[this.t[j].value]) {
        j = this.close(j);
        continue;
      }
      if (this.t[j].kind === "ident" && (this.t[j].value === "as" || this.t[j].value === "satisfies") && j > from) {
        to = j;
        break;
      }
    }
    if (from >= to) return { kind: "unknown" };
    // Parenthesised expression.
    if (this.is(from, "(") && this.close(from) === to - 1) return this.expr(from + 1, to - 1);
    // Object literal.
    if (this.is(from, "{") && this.close(from) === to - 1) return this.object(from, to - 1);
    // Arrow function: params => body (params may be `(...)` or a bare identifier, optionally async).
    for (let j = from; j < to; j++) {
      if (this.t[j].kind === "punct" && OPEN[this.t[j].value]) {
        j = this.close(j);
        continue;
      }
      if (this.is(j, "=>")) return { kind: "fn", returns: this.fnBody(j + 1, to) };
    }
    // function (...) { ... }
    if (this.t[from].kind === "ident" && this.t[from].value === "function") {
      let j = from + 1;
      while (j < to && !this.is(j, "(")) j++;
      j = this.close(j) + 1;
      while (j < to && !this.is(j, "{")) j++;
      return { kind: "fn", returns: this.fnBody(j, to) };
    }
    // Reference or call chain: a.b.c / a.b(...).c(...)
    if (this.t[from].kind === "ident") {
      const callee: string[] = [this.t[from].value];
      const calls: Expr[][] = [];
      let j = from + 1;
      while (j < to) {
        if (this.is(j, ".") && this.t[j + 1]?.kind === "ident") {
          if (calls.length === 0) callee.push(this.t[j + 1].value);
          j += 2;
        } else if (this.is(j, "<")) {
          // generic type arguments: skip to matching '>'
          let depth = 0;
          for (; j < to; j++) {
            if (this.is(j, "<")) depth++;
            else if (this.is(j, ">") && --depth === 0) break;
          }
          j++;
        } else if (this.is(j, "(")) {
          const end = this.close(j);
          calls.push(this.split(j + 1, end, ",").map(([a, b]) => this.expr(a, b)));
          j = end + 1;
        } else if (this.is(j, "!")) {
          j++;
        } else {
          return { kind: "unknown" };
        }
      }
      return calls.length ? { kind: "call", callee, calls } : { kind: "ref", path: callee };
    }
    return { kind: "unknown" };
  }

  // Body of a function starting at token `from`: `({...})`, `{ ... return x; }` or an expression.
  fnBody(from: number, to: number): Expr | undefined {
    if (this.is(from, "{")) {
      const end = this.close(from);
      // last top-level `return <expr>` in the block
      let ret: Expr | undefined;
      for (let j = from + 1; j < end; j++) {
        if (this.t[j].kind === "punct" && OPEN[this.t[j].value]) {
          j = this.close(j);
          continue;
        }
        if (this.t[j].kind === "ident" && this.t[j].value === "return") {
          // The returned expression runs to the next `;` at this depth (or the block end).
          let k = j + 1;
          while (k < end && !this.is(k, ";")) {
            if (this.t[k].kind === "punct" && OPEN[this.t[k].value]) k = this.close(k);
            k++;
          }
          ret = this.expr(j + 1, Math.min(k, end));
          j = k;
        }
      }
      return ret;
    }
    return this.expr(from, to);
  }

  object(open: number, close: number): Expr {
    const members: Member[] = [];
    for (const [a, b] of this.split(open + 1, close, ",")) {
      if (a >= b) continue;
      if (this.is(a, "...")) {
        members.push({ kind: "spread", expr: this.expr(a + 1, b) });
        continue;
      }
      const k = this.t[a];
      let key: string | undefined;
      if (k.kind === "ident" || k.kind === "string" || k.kind === "number") key = k.value;
      if (key === undefined) continue; // computed key, getter, ...
      if (b === a + 1) {
        members.push({ kind: "prop", key, doc: this.docFor(a, b - 1), shorthand: true });
      } else if (this.is(a + 1, ":") || this.is(a + 1, "(")) {
        members.push({ kind: "prop", key, doc: this.docFor(a, b - 1), shorthand: false });
      }
    }
    return { kind: "object", members };
  }

  // Top-level declarations, imports and exports.
  file(path: string): SourceFile {
    const sf: SourceFile = { path, decls: new Map(), exportsAs: new Map(), imports: new Map() };
    const t = this.t;
    for (let i = 0; i < t.length; i++) {
      const tok = t[i];
      if (tok.kind === "punct" && OPEN[tok.value]) {
        i = this.close(i);
        continue;
      }
      if (tok.kind !== "ident") continue;
      if (tok.value === "import" && !this.is(i + 1, "(") && !this.is(i + 1, ".")) {
        let j = i + 1;
        if (t[j]?.value === "type") j++;
        const end = this.findSpec(j);
        if (end < 0) continue;
        const spec = t[end].value;
        // default import
        if (t[j].kind === "ident" && t[j].value !== "from" && !this.is(j, "{")) {
          if (t[j].value !== "*") sf.imports.set(t[j].value, { spec, imported: "default" });
          j++;
          if (this.is(j, ",")) j++;
        }
        if (this.is(j, "*")) {
          sf.imports.set(t[j + 2].value, { spec, imported: "*" }); // * as ns
        } else if (this.is(j, "{")) {
          const close = this.close(j);
          for (const [a, b] of this.split(j + 1, close, ",")) {
            let s = a;
            if (t[s]?.value === "type") s++;
            if (s >= b) continue;
            const imported = t[s].value;
            const local = b - s >= 3 && t[s + 1].value === "as" ? t[s + 2].value : imported;
            sf.imports.set(local, { spec, imported });
          }
        }
        i = end;
        continue;
      }
      if (tok.value === "export" && this.is(i + 1, "{")) {
        const close = this.close(i + 1);
        for (const [a, b] of this.split(i + 2, close, ",")) {
          if (a >= b) continue;
          const local = t[a].value;
          const exported = b - a >= 3 && t[a + 1].value === "as" ? t[a + 2].value : local;
          sf.exportsAs.set(exported, local);
        }
        if (t[close + 1]?.value === "from" && t[close + 2]) {
          // export { a as b } from "./x": treat as imports of the exported names
          for (const [exported, local] of sf.exportsAs) {
            if (!sf.decls.has(local)) sf.imports.set(exported, { spec: t[close + 2].value, imported: local });
          }
        }
        i = close;
        continue;
      }
      if (tok.value === "const" || tok.value === "let" || tok.value === "var") {
        const nameTok = t[i + 1];
        if (nameTok?.kind !== "ident") continue;
        let j = i + 2;
        // skip a type annotation
        while (j < t.length && !this.is(j, "=") && !this.is(j, ";")) {
          if (t[j].kind === "punct" && OPEN[t[j].value]) j = this.close(j);
          j++;
        }
        if (!this.is(j, "=")) continue;
        const end = this.statementEnd(j + 1);
        sf.decls.set(nameTok.value, { expr: this.expr(j + 1, end), doc: this.docFor(this.declStart(i), i + 1) });
        i = end - 1;
        continue;
      }
      if (tok.value === "function" && t[i + 1]?.kind === "ident") {
        let j = i + 2;
        while (j < t.length && !this.is(j, "(")) j++;
        j = this.close(j) + 1;
        while (j < t.length && !this.is(j, "{")) j++;
        if (j >= t.length) continue;
        sf.decls.set(t[i + 1].value, { expr: { kind: "fn", returns: this.fnBody(j, this.close(j) + 1) }, doc: undefined });
        i = this.close(j);
      }
    }
    return sf;
  }

  // `export const x` / `export default` modifiers belong to the declaration's doc position.
  declStart(i: number) {
    let s = i;
    while (s > 0 && this.t[s - 1].kind === "ident" && ["export", "declare"].includes(this.t[s - 1].value)) s--;
    return s;
  }

  // Index of the module-specifier string of an import starting at i (or -1).
  findSpec(i: number): number {
    for (let j = i; j < this.t.length; j++) {
      if (this.t[j].kind === "punct" && OPEN[this.t[j].value]) {
        j = this.close(j);
        continue;
      }
      if (this.t[j].kind === "string") return j;
      if (this.is(j, ";")) return -1;
    }
    return -1;
  }

  // End (exclusive) of a top-level initializer starting at i: the first `;` at depth 0,
  // or the first token on a later line that starts a new top-level statement.
  statementEnd(i: number): number {
    for (let j = i; j < this.t.length; j++) {
      const tok = this.t[j];
      if (tok.kind === "punct" && OPEN[tok.value]) {
        j = this.close(j);
        continue;
      }
      if (this.is(j, ";")) return j;
      if (
        j > i &&
        tok.kind === "ident" &&
        ["export", "import", "const", "let", "var", "function", "type", "interface", "class", "enum", "declare"].includes(tok.value) &&
        tok.line > this.t[j - 1].endLine &&
        !this.is(j - 1, ".") && !this.is(j - 1, "=")
      ) {
        return j;
      }
    }
    return this.t.length;
  }
}

function blockLines(body: string): string[] {
  const lines = body.replace(/^\*/, "").split("\n").map((l) => l.replace(/^\s*\*?\s?/, "").replace(/\s+$/, ""));
  while (lines.length && lines[0] === "") lines.shift();
  while (lines.length && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

// --- resolution across files ------------------------------------------------

const sources = new Map<string, SourceFile | null>();

function loadSource(path: string): SourceFile | null {
  if (!sources.has(path)) {
    let sf: SourceFile | null = null;
    try {
      sf = new Parser(tokenize(readFileSync(path, "utf8"))).file(path);
    } catch {
      sf = null;
    }
    sources.set(path, sf);
  }
  return sources.get(path)!;
}

function resolveModule(from: string, spec: string): string | undefined {
  if (!spec.startsWith(".") && !spec.startsWith("/")) return undefined; // package import
  const base = resolve(dirname(from), spec);
  const stem = base.replace(/\.(m|c)?js$/, ""); // "./x.js" in TS source means ./x.ts
  for (const cand of [base, `${stem}.ts`, `${stem}.mts`, `${stem}.cts`, `${stem}.tsx`, `${base}/index.ts`]) {
    try {
      if (statSync(cand).isFile()) return cand;
    } catch {
      // try the next candidate
    }
  }
  return undefined;
}

type Resolved = { file: SourceFile; expr: Expr; doc?: ColDoc };

// Resolves an exported (or local) name of a file to its declaration.
function lookup(file: SourceFile, name: string, seen = new Set<string>()): Resolved | undefined {
  const key = `${file.path}#${name}`;
  if (seen.has(key)) return undefined;
  seen.add(key);
  const local = file.exportsAs.get(name) ?? name;
  const decl = file.decls.get(local);
  if (decl) return { file, expr: decl.expr, doc: decl.doc };
  const imp = file.imports.get(local) ?? file.imports.get(name);
  if (imp && imp.imported !== "*") {
    const target = resolveModule(file.path, imp.spec);
    const tf = target ? loadSource(target) : null;
    if (tf) return lookup(tf, imp.imported, seen);
  }
  return undefined;
}

function resolvePath(file: SourceFile, path: string[]): Resolved | undefined {
  if (path.length === 2) {
    const imp = file.imports.get(path[0]);
    if (imp?.imported === "*") {
      const target = resolveModule(file.path, imp.spec);
      const tf = target ? loadSource(target) : null;
      return tf ? lookup(tf, path[1]) : undefined;
    }
  }
  if (path.length !== 1) return undefined;
  const r = lookup(file, path[0]);
  if (r) return r;
  // Fallback: a unique top-level declaration with this name in any schema file.
  const hits = [...sources.values()].filter((s): s is SourceFile => !!s && s.decls.has(path[0]));
  return hits.length === 1 ? lookup(hits[0], path[0]) : undefined;
}

// The object literal an expression evaluates to (for spreads and table columns).
function objectOf(file: SourceFile, e: Expr | undefined, depth = 0): { file: SourceFile; members: Member[] } | undefined {
  if (!e || depth > 32) return undefined;
  switch (e.kind) {
    case "object":
      return { file, members: e.members };
    case "fn":
      return objectOf(file, e.returns, depth + 1);
    case "ref": {
      const r = resolvePath(file, e.path);
      return r && objectOf(r.file, r.expr, depth + 1);
    }
    case "call": {
      // A user helper returning an object: `...timestamps()`.
      if (e.calls.length === 1) {
        const r = resolvePath(file, e.callee);
        if (r && r.expr.kind === "fn") return objectOf(r.file, r.expr, depth + 1);
      }
      // A table builder: the first argument (in any call of the chain) that is,
      // or returns, an object: pgTable("t", {...}), schema.table("t", (t) => ({...})).
      for (const args of e.calls) {
        for (const a of args) {
          if (a.kind === "object" || (a.kind === "fn" && a.returns)) return objectOf(file, a, depth + 1);
        }
      }
      return undefined;
    }
  }
  return undefined;
}

type Found = { doc: ColDoc | undefined };

// Doc of property `key` in an object, honouring spreads: the last definition wins.
function memberDoc(file: SourceFile, members: Member[], key: string, depth = 0): Found | undefined {
  if (depth > 32) return undefined;
  let found: Found | undefined;
  for (const m of members) {
    if (m.kind === "prop") {
      if (m.key !== key) continue;
      let doc = m.doc;
      if (!doc && m.shorthand) doc = resolvePath(file, [key])?.doc; // `{ id }` documented at `const id = ...`
      found = { doc };
    } else {
      const obj = objectOf(file, m.expr, depth + 1);
      const r = obj && memberDoc(obj.file, obj.members, key, depth + 1);
      if (r) found = r;
    }
  }
  return found;
}

// --- load the schema ----------------------------------------------------------

const tables = new Map<string, PgTable>();
const tableSources = new Map<string, { file: SourceFile; exportName: string }>();
const enums = new Map<string, PgEnum<[string, ...string[]]>>();

const schemaFiles = readdirSync(schemaDir)
  .filter((f) => f.endsWith(".ts"))
  .sort()
  .map((f) => resolve(schemaDir, f));
for (const p of schemaFiles) loadSource(p);

for (const p of schemaFiles) {
  const mod = await import(pathToFileURL(p).href);
  for (const [exportName, value] of Object.entries(mod)) {
    if (is(value, PgTable)) {
      const name = getTableConfig(value as PgTable).name;
      tables.set(name, value as PgTable);
      const sf = sources.get(p);
      if (sf && (lookup(sf, exportName) || !tableSources.has(name))) tableSources.set(name, { file: sf, exportName });
    } else if (isPgEnum(value)) enums.set(value.enumName, value as PgEnum<[string, ...string[]]>);
  }
}

function columnDoc(tableName: string, key: string): ColDoc | undefined {
  const src = tableSources.get(tableName);
  if (!src) return undefined;
  const decl = lookup(src.file, src.exportName);
  const obj = decl && objectOf(decl.file, decl.expr);
  return obj ? memberDoc(obj.file, obj.members, key)?.doc : undefined;
}

// Array support. Drizzle represents `.array()` columns two ways depending on version:
//  - <= 0.4x: a `PgArray` wrapper column with the element column on `baseColumn`
//  - 1.0 beta: the column keeps its own type and carries `dimensions` (> 0 for arrays)
// Both map to `T[]`. Element types can't carry property decorators like @format, so
// uuid elements use the `Uuid` scalar declared in the generated file header.
function typeOf(col: any, inArray = false): { type: string; decorators: string[] } {
  if (col.baseColumn) {
    const inner = typeOf(col.baseColumn, true);
    return { type: `${inner.type}[]`, decorators: [] };
  }
  const dims = Number(col.dimensions ?? 0);
  const base = scalarOf(col, inArray || dims > 0);
  return dims > 0 ? { type: base.type + "[]".repeat(dims), decorators: [] } : base;
}

function scalarOf(col: any, asElement: boolean): { type: string; decorators: string[] } {
  const enumName = col.config?.enum?.enumName ?? col.enum?.enumName;
  const enumDef = [...enums.values()].find((e) => e.enumName === enumName);
  if (enumDef) return { type: pascal(enumDef.enumName), decorators: [] };

  switch (col.columnType) {
    case "PgUUID":
      return asElement
        ? { type: "Uuid", decorators: [] }
        : { type: "string", decorators: ['@format("uuid")'] };
    case "PgText":
    case "PgVarchar":
      return { type: "string", decorators: [] };
    case "PgInteger":
    case "PgSmallInt":
    case "PgBigInt53":
    case "PgBigInt64":
      return { type: "integer", decorators: [] };
    case "PgBoolean":
      return { type: "boolean", decorators: [] };
    case "PgTimestamp":
    case "PgTimestampString":
      return { type: "utcDateTime", decorators: [] };
    case "PgDateString":
      return { type: "plainDate", decorators: [] };
    case "PgNumeric":
    case "PgNumericNumber":
    case "PgReal":
    case "PgDoublePrecision":
      return { type: "float", decorators: [] };
    case "PgJsonb":
    case "PgJson":
      return { type: "unknown", decorators: [] };
    default:
      console.warn(`unmapped column type ${col.columnType} (${col.name}) -> unknown`);
      return { type: "unknown", decorators: [] };
  }
}

const out: string[] = [
  ...params.header,
  "",
  'import "@typespec/openapi";',
  'import "@typespec/openapi3";',
  "",
  `namespace ${params.namespace};`,
  "",
  '@format("uuid")',
  "scalar Uuid extends string;",
  "",
];

for (const e of [...enums.values()].sort((a, b) => a.enumName.localeCompare(b.enumName))) {
  out.push(`enum ${pascal(e.enumName)} {`);
  for (const v of e.enumValues) out.push(`  ${/^[A-Za-z_]\w*$/.test(v) ? v : `\`${v}\``}: "${v}",`);
  out.push("}", "");
}

const READ_ONLY = new Set(params.readOnlyColumns);
const missingDocs: string[] = [];

for (const [name, table] of [...tables].sort(([a], [b]) => a.localeCompare(b))) {
  const cfg = getTableConfig(table);
  const fkCols = new Set(
    cfg.foreignKeys.flatMap((fk) => fk.reference().columns.map((c) => c.name)),
  );

  out.push(`@doc("Database table \`${name}\`.")`, `model ${pascal(singular(name))}Row {`);
  const keyOf = new Map<unknown, string>(Object.entries(getTableColumns(table)).map(([k, c]) => [c, k]));
  for (const col of cfg.columns) {
    const { type, decorators } = typeOf(col);
    const info = columnDoc(name, keyOf.get(col) ?? col.name);
    if (info?.doc) out.push(`  @doc(${JSON.stringify(info.doc)})`);
    else if (params.requireDocs !== "off") {
      missingDocs.push(`${name}.${col.name}`);
      console.warn(`no doc comment for ${name}.${col.name}`);
    }
    for (const ex of info?.examples ?? []) out.push(`  @example(${ex})`);
    for (const d of decorators) out.push(`  ${d}`);
    if (READ_ONLY.has(col.name)) {
      out.push(`  @visibility(Lifecycle.Read, Lifecycle.Delete)`);
    }
    if (fkCols.has(col.name)) {
      out.push(`  @removeVisibility(Lifecycle.Update)`);
    }
    out.push(`  ${camel(col.name)}${col.notNull ? "" : "?"}: ${type};`, "");
  }
  if (out[out.length - 1] === "") out.pop();
  out.push("}", "");
}

if (params.requireDocs === "error" && missingDocs.length > 0) {
  console.error(`require_docs: error — ${missingDocs.length} column(s) have no doc comment; ${outFile} not written`);
  process.exit(1);
}

mkdirSync(dirname(outFile), { recursive: true });
writeFileSync(outFile, out.join("\n"));
console.log(`wrote ${outFile} (${enums.size} enums, ${tables.size} models)`);
