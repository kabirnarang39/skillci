import * as vscode from "vscode";
import { spawn } from "child_process";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import { parseIssues, groupByFile, remapIssuePaths, ParsedIssue, Severity } from "./lintResult";

let diagnostics: vscode.DiagnosticCollection;
let outputChannel: vscode.OutputChannel;
const debounceTimers = new Map<string, ReturnType<typeof setTimeout>>();
// A single `skillci check` run against one skill directory can report
// issues against more than one file (SKILL.md itself, plus any
// referenced file its content-scan covers) — each on its own URI. Tracks
// which URIs the LAST run for a given skill directory populated, so the
// next run can clear diagnostics on a referenced file whose issue was
// fixed (and therefore no longer appears in the new issue list) instead
// of leaving a stale diagnostic behind forever.
const lastReportedURIsBySkillDir = new Map<string, Set<string>>();
// Tracks whether we've already warned about a missing/broken skillci
// binary this session — a spawn failure fires on every keystroke once
// lint-on-type is active, and re-showing the same error message that
// often would drown out everything else VS Code is trying to tell the
// user.
let warnedMissingBinary = false;

// skillci is a separate process that always reads SKILL.md from disk
// (os.ReadFile) — it has no notion of VS Code's in-memory document
// buffer. Without this, "lint-on-type" would debounce and re-spawn
// skillci correctly, but skillci would keep re-reading the same
// unchanged on-disk file, so unsaved keystrokes would never actually
// affect what's linted until the user saves — the setting would fire
// on schedule while silently linting stale content. Keyed by real skill
// directory -> a scratch temp directory containing the SAME referenced
// files (scripts/, references/, assets/) linked in, plus a live copy of
// the buffer's own current text as SKILL.md, so `skillci check` run
// against the temp directory sees the actual unsaved content while
// referenced-file checks still resolve correctly.
const tempMirrorDirs = new Map<string, string>();

// ensureTempMirror creates (or reuses) the scratch mirror for skillDir.
// Symlinks are best-effort: they can fail without elevated permissions
// on Windows, in which case referenced-file checks simply won't see
// that entry during lint-on-type specifically — the always-correct
// open/save paths (which lint the real directory directly, no mirror
// involved) are unaffected either way.
//
// ponytail: the mirror is built once and reused for the document's
// lifetime — it does not notice a NEW file being added to skillDir
// mid-session without the SKILL.md tab being closed and reopened.
// Acceptable for now; revisit with an fs.watch if that turns out to
// matter in practice.
function ensureTempMirror(skillDir: string): string {
  const existing = tempMirrorDirs.get(skillDir);
  if (existing) {
    return existing;
  }

  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "skillci-vscode-"));
  let entries: string[] = [];
  try {
    entries = fs.readdirSync(skillDir);
  } catch {
    entries = [];
  }
  for (const entry of entries) {
    if (entry === "SKILL.md") {
      continue;
    }
    try {
      fs.symlinkSync(path.join(skillDir, entry), path.join(tempDir, entry));
    } catch {
      // See the function-level comment above.
    }
  }
  tempMirrorDirs.set(skillDir, tempDir);
  return tempDir;
}

function cleanupTempMirror(skillDir: string): void {
  const tempDir = tempMirrorDirs.get(skillDir);
  if (!tempDir) {
    return;
  }
  tempMirrorDirs.delete(skillDir);
  fs.rm(tempDir, { recursive: true, force: true }, () => {
    // Best-effort cleanup of a scratch temp directory — nothing
    // meaningful to do if OS-level removal fails (e.g. a file locked by
    // another process); it's under the OS temp dir and will eventually
    // be cleaned up by the OS itself regardless.
  });
}

export function activate(context: vscode.ExtensionContext): void {
  diagnostics = vscode.languages.createDiagnosticCollection("skillci");
  outputChannel = vscode.window.createOutputChannel("SkillCI");
  context.subscriptions.push(diagnostics, outputChannel);

  for (const doc of vscode.workspace.textDocuments) {
    if (isSkillFile(doc)) {
      lintDocument(doc);
    }
  }

  context.subscriptions.push(
    vscode.workspace.onDidOpenTextDocument((doc) => {
      if (isSkillFile(doc)) {
        lintDocument(doc);
      }
    }),
    vscode.workspace.onDidSaveTextDocument((doc) => {
      if (isSkillFile(doc)) {
        lintDocument(doc);
      }
    }),
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (!isSkillFile(e.document)) {
        return;
      }
      const cfg = vscode.workspace.getConfiguration("skillci");
      if (!cfg.get<boolean>("lintOnType", true)) {
        return;
      }
      const delay = cfg.get<number>("lintOnTypeDelayMs", 500);
      const key = e.document.uri.toString();
      const existing = debounceTimers.get(key);
      if (existing) {
        clearTimeout(existing);
      }
      debounceTimers.set(
        key,
        setTimeout(() => lintDocumentFromBuffer(e.document), delay),
      );
    }),
    vscode.workspace.onDidCloseTextDocument((doc) => {
      if (isSkillFile(doc)) {
        const skillDir = path.dirname(doc.uri.fsPath);
        const uris = lastReportedURIsBySkillDir.get(skillDir);
        if (uris) {
          for (const uriString of uris) {
            diagnostics.delete(vscode.Uri.parse(uriString));
          }
          lastReportedURIsBySkillDir.delete(skillDir);
        } else {
          diagnostics.delete(doc.uri);
        }
        cleanupTempMirror(skillDir);
      }
      const key = doc.uri.toString();
      const timer = debounceTimers.get(key);
      if (timer) {
        clearTimeout(timer);
        debounceTimers.delete(key);
      }
    }),
  );
}

export function deactivate(): void {
  for (const timer of debounceTimers.values()) {
    clearTimeout(timer);
  }
  debounceTimers.clear();
  for (const skillDir of Array.from(tempMirrorDirs.keys())) {
    cleanupTempMirror(skillDir);
  }
}

function isSkillFile(doc: vscode.TextDocument): boolean {
  return path.basename(doc.uri.fsPath) === "SKILL.md";
}

// runSkillciCheck spawns `skillci check --format json <dir>` and hands the
// parsed issues to onSuccess — shared by both lintDocument (real
// directory, used for open/save, where disk already matches the buffer)
// and lintDocumentFromBuffer (a scratch mirror directory, used for
// lint-on-type, where disk does NOT yet match the buffer).
function runSkillciCheck(dir: string, onSuccess: (issues: ParsedIssue[]) => void): void {
  const binary = vscode.workspace.getConfiguration("skillci").get<string>("path", "skillci");

  const child = spawn(binary, ["check", "--format", "json", dir], {
    cwd: dir,
  });

  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => (stdout += chunk));
  child.stderr.on("data", (chunk) => (stderr += chunk));

  child.on("error", (err) => {
    // ENOENT: the binary itself isn't found on PATH/skillci.path — the
    // one failure mode worth surfacing to the user, since every other
    // failure below is either expected (exit 1 = issues found) or a
    // skillci-internal problem better diagnosed via the output channel.
    if (!warnedMissingBinary) {
      warnedMissingBinary = true;
      vscode.window
        .showErrorMessage(
          `SkillCI: couldn't run "${binary}" (${err.message}). Set "skillci.path" in settings if it's not on your PATH.`,
          "Open Settings",
        )
        .then((choice) => {
          if (choice === "Open Settings") {
            vscode.commands.executeCommand("workbench.action.openSettings", "skillci.path");
          }
        });
    }
  });

  child.on("close", (code) => {
    // Exit code 0 = no issues, 1 = issues found (both are skillci check's
    // own normal outcomes, per cmd/skillci/check.go — only >1 or a
    // non-JSON stdout indicates something actually went wrong).
    if (code !== 0 && code !== 1) {
      outputChannel.appendLine(`skillci check exited ${code} for ${dir}\nstderr: ${stderr}`);
      return;
    }

    try {
      const issues = parseIssues(stdout);
      onSuccess(issues);
    } catch (err) {
      outputChannel.appendLine(
        `failed to parse skillci output for ${dir}: ${err}\nstdout: ${stdout}\nstderr: ${stderr}`,
      );
    }
  });
}

function lintDocument(doc: vscode.TextDocument): void {
  const skillDir = path.dirname(doc.uri.fsPath);
  runSkillciCheck(skillDir, (issues) => publishDiagnostics(skillDir, issues));
}

// lintDocumentFromBuffer is lintDocument's counterpart for the
// lint-on-type (debounced, unsaved-edit) path: skillci always reads
// SKILL.md from disk, so linting the real skillDir directly here would
// silently re-check the same unchanged on-disk content on every
// keystroke, never reflecting what was actually just typed. Instead this
// writes the live buffer to a scratch mirror directory (see
// ensureTempMirror) and runs skillci against that, then remaps the
// resulting issue paths back onto the real files before publishing.
function lintDocumentFromBuffer(doc: vscode.TextDocument): void {
  const skillDir = path.dirname(doc.uri.fsPath);
  const tempDir = ensureTempMirror(skillDir);

  try {
    fs.writeFileSync(path.join(tempDir, "SKILL.md"), doc.getText());
  } catch (err) {
    outputChannel.appendLine(`lint-on-type: failed to write buffer mirror for ${skillDir}: ${err}`);
    return;
  }

  runSkillciCheck(tempDir, (issues) => {
    publishDiagnostics(skillDir, remapIssuePaths(issues, tempDir, skillDir));
  });
}

function publishDiagnostics(
  skillDir: string,
  issues: ReturnType<typeof parseIssues>,
): void {
  const byFile = groupByFile(issues);
  const newURIs = new Set<string>();
  for (const [file, fileIssues] of byFile) {
    const uri = vscode.Uri.file(file);
    newURIs.add(uri.toString());
    diagnostics.set(
      uri,
      fileIssues.map((issue) => toDiagnostic(issue.line, issue.rule, issue.msg, issue.severity)),
    );
  }

  // Clear any URI this skill directory reported last run but not this
  // run — e.g. a referenced file (scripts/install.sh) whose issue was
  // fixed no longer appears in `issues`, so its stale diagnostic would
  // otherwise never get removed.
  const previousURIs = lastReportedURIsBySkillDir.get(skillDir);
  if (previousURIs) {
    for (const uriString of previousURIs) {
      if (!newURIs.has(uriString)) {
        diagnostics.delete(vscode.Uri.parse(uriString));
      }
    }
  }
  lastReportedURIsBySkillDir.set(skillDir, newURIs);
}

function toDiagnostic(
  line: number,
  rule: string,
  msg: string,
  severity: Severity,
): vscode.Diagnostic {
  // skillci reports 1-indexed lines; vscode.Position is 0-indexed. A
  // line of 0 (shouldn't happen, but not guaranteed by the JSON contract)
  // is clamped rather than producing a negative Position, which VS Code
  // would reject.
  const zeroIndexedLine = Math.max(0, line - 1);
  const range = new vscode.Range(zeroIndexedLine, 0, zeroIndexedLine, Number.MAX_SAFE_INTEGER);
  const diagnostic = new vscode.Diagnostic(
    range,
    `[${rule}] ${msg}`,
    severity === "error" ? vscode.DiagnosticSeverity.Error : vscode.DiagnosticSeverity.Warning,
  );
  diagnostic.source = "skillci";
  diagnostic.code = rule;
  return diagnostic;
}
