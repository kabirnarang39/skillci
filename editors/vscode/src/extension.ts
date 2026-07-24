import * as vscode from "vscode";
import { spawn } from "child_process";
import * as path from "path";
import { parseIssues, groupByFile, Severity } from "./lintResult";

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
        setTimeout(() => lintDocument(e.document), delay),
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
}

function isSkillFile(doc: vscode.TextDocument): boolean {
  return path.basename(doc.uri.fsPath) === "SKILL.md";
}

function lintDocument(doc: vscode.TextDocument): void {
  const skillDir = path.dirname(doc.uri.fsPath);
  const binary = vscode.workspace.getConfiguration("skillci").get<string>("path", "skillci");

  const child = spawn(binary, ["check", "--format", "json", skillDir], {
    cwd: skillDir,
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
      outputChannel.appendLine(
        `skillci check exited ${code} for ${skillDir}\nstderr: ${stderr}`,
      );
      return;
    }

    try {
      const issues = parseIssues(stdout);
      publishDiagnostics(skillDir, issues);
    } catch (err) {
      outputChannel.appendLine(
        `failed to parse skillci output for ${skillDir}: ${err}\nstdout: ${stdout}\nstderr: ${stderr}`,
      );
    }
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
