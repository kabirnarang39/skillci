// Pure, dependency-free mapping from skillci's `--format json` output to
// VS Code diagnostics — kept separate from extension.ts (which needs the
// real `vscode` module, only resolvable inside a running extension host)
// so this logic can be unit-tested with plain `node:test`, no extension
// test harness required.

export interface Issue {
  file: string;
  line: number;
  rule: string;
  msg: string;
}

export type Severity = "error" | "warning";

// securityRulePrefix identifies skillci's OWASP-mapped security rules
// (ast01-*, ast02-*, ... ast10-*) — these get Error severity in the
// editor; everything else (missing-description, bloat warnings, path
// issues) gets Warning. A security finding is qualitatively different
// from a style nit and should stand out accordingly.
const securityRulePrefix = /^ast\d\d-/;

export function severityForRule(rule: string): Severity {
  return securityRulePrefix.test(rule) ? "error" : "warning";
}

export interface ParsedIssue extends Issue {
  severity: Severity;
}

// parseIssues parses skillci's --format json stdout. Throws on malformed
// JSON or a payload that isn't a JSON array — the caller decides how to
// surface that (skillci's own JSON contract guarantees an array, so a
// parse failure here means something is genuinely wrong, e.g. a stale
// skillci binary predating --format json, or unexpected stderr mixed in).
export function parseIssues(stdout: string): ParsedIssue[] {
  const trimmed = stdout.trim();
  if (trimmed === "") {
    return [];
  }
  const raw = JSON.parse(trimmed);
  if (!Array.isArray(raw)) {
    throw new Error("skillci check --format json did not return a JSON array");
  }
  return raw.map((issue: Issue) => ({
    ...issue,
    severity: severityForRule(issue.rule),
  }));
}

// remapIssuePaths rewrites any issue whose file path lives under tempDir
// back to the equivalent path under realDir — used by lint-on-type's
// buffer-mirror mode, where skillci was run against a scratch temp
// directory (so it sees the live unsaved buffer content for SKILL.md, via
// symlinks for everything else) but diagnostics must land on the real
// files the user actually has open on disk, not the scratch mirror they
// never asked to see. An issue whose path isn't under tempDir at all
// (shouldn't happen given how the caller invokes skillci, but not
// guaranteed by the JSON contract) passes through unchanged rather than
// being dropped.
export function remapIssuePaths(issues: ParsedIssue[], tempDir: string, realDir: string): ParsedIssue[] {
  return issues.map((issue) => {
    if (issue.file === tempDir) {
      return { ...issue, file: realDir };
    }
    if (issue.file.startsWith(tempDir + "/") || issue.file.startsWith(tempDir + "\\")) {
      return { ...issue, file: realDir + issue.file.slice(tempDir.length) };
    }
    return issue;
  });
}

// groupByFile splits a flat issue list by absolute file path, since a
// single `skillci check` invocation can report issues against more than
// one file (SKILL.md itself, plus any referenced file its content-scan
// covers) — each needs its own vscode.Diagnostic[] set on its own URI.
export function groupByFile(issues: ParsedIssue[]): Map<string, ParsedIssue[]> {
  const byFile = new Map<string, ParsedIssue[]>();
  for (const issue of issues) {
    const existing = byFile.get(issue.file);
    if (existing) {
      existing.push(issue);
    } else {
      byFile.set(issue.file, [issue]);
    }
  }
  return byFile;
}
