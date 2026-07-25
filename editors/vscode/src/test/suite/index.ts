// Real integration test: launched by runTest.ts inside an actual VS Code
// Extension Development Host (via @vscode/test-electron), with this
// extension genuinely activated and its real vscode API calls executing
// — not a mock. Deliberately hand-written with plain node:assert rather
// than pulling in Mocha as a dependency: @vscode/test-electron only
// requires the module at extensionTestsPath to export an async run(), it
// has no opinion about what runs inside it.
import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as path from "node:path";
import * as vscode from "vscode";

// waitForSkillciDiagnostics polls vscode.languages.getDiagnostics(uri)
// until at least one skillci-sourced diagnostic appears (or deadline),
// since the extension's spawn of `skillci check` is asynchronous — a
// real child process launch inside a real Electron extension host, not
// an in-memory call.
//
// Filtered to source === "skillci": getDiagnostics returns diagnostics
// from every contributor on this URI, not just this extension's —
// VS Code's own built-in Markdown language features can independently
// notice and report an unrelated finding on the same .md file, which
// would otherwise let a caller's assertions pass even if this
// extension's own diagnostics never published at all.
async function waitForSkillciDiagnostics(uri: vscode.Uri, timeoutMs = 15_000): Promise<vscode.Diagnostic[]> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const diagnostics = vscode.languages.getDiagnostics(uri).filter((d) => d.source === "skillci");
    if (diagnostics.length > 0) {
      return diagnostics;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return [];
}

// waitForNoSkillciDiagnostics is waitForSkillciDiagnostics' inverse: polls
// until skillci-sourced diagnostics on uri disappear, for proving a fixed
// issue's diagnostic actually gets cleared, not just that a new one gets
// added on top.
async function waitForNoSkillciDiagnostics(uri: vscode.Uri, timeoutMs = 15_000): Promise<vscode.Diagnostic[]> {
  const deadline = Date.now() + timeoutMs;
  let diagnostics: vscode.Diagnostic[] = [];
  while (Date.now() < deadline) {
    diagnostics = vscode.languages.getDiagnostics(uri).filter((d) => d.source === "skillci");
    if (diagnostics.length === 0) {
      return diagnostics;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  return diagnostics;
}

export async function run(): Promise<void> {
  // Set skillci.path directly via the configuration API, targeting the
  // isolated test profile (ConfigurationTarget.Global — the test-runner's
  // own throwaway VS Code profile under .vscode-test/, never the fixture
  // folder's own files) rather than relying on PATH. VS Code resolves its
  // own PATH from the user's login shell on macOS (to fix the PATH GUI
  // apps normally don't inherit) — that resolution can win over anything
  // passed through @vscode/test-electron's extensionTestsEnv, and on a
  // machine with multiple skillci installs (e.g. an older Homebrew-tapped
  // one earlier in PATH) it silently picks the wrong binary instead of
  // the one this specific test run just built.
  const binaryPath = path.resolve(__dirname, "../../../.bin/skillci");
  await vscode.workspace
    .getConfiguration("skillci")
    .update("path", binaryPath, vscode.ConfigurationTarget.Global);

  await testBrokenSkillMdDiagnostics();
  await testReferencedFileDiagnosticsAndClearing();

  console.log("PASS: all VS Code extension integration scenarios");
}

async function testBrokenSkillMdDiagnostics(): Promise<void> {
  const fixtureUri = vscode.Uri.file(
    path.resolve(__dirname, "../../../test-fixtures/broken-skill/SKILL.md"),
  );

  const doc = await vscode.workspace.openTextDocument(fixtureUri);
  await vscode.window.showTextDocument(doc);

  const diagnostics = await waitForSkillciDiagnostics(fixtureUri);
  assert.ok(
    diagnostics.length > 0,
    `expected at least one skillci diagnostic on ${fixtureUri.fsPath} within 15s, got none — the extension either didn't run skillci check or found the binary at the wrong path`,
  );

  const pipeToShell = diagnostics.find((d) => d.code === "ast01-pipe-to-shell");
  assert.ok(
    pipeToShell,
    `expected an ast01-pipe-to-shell diagnostic, got: ${JSON.stringify(diagnostics.map((d) => ({ code: d.code, message: d.message, severity: d.severity })))}`,
  );
  assert.equal(
    pipeToShell.severity,
    vscode.DiagnosticSeverity.Error,
    "ast01-pipe-to-shell must be reported as Error severity, not Warning",
  );

  const missingDescription = diagnostics.find((d) => d.code === "missing-description");
  assert.ok(
    missingDescription,
    `expected a missing-description diagnostic (the fixture has no description field), got: ${JSON.stringify(diagnostics.map((d) => d.code))}`,
  );
  assert.equal(
    missingDescription.severity,
    vscode.DiagnosticSeverity.Warning,
    "missing-description must be reported as Warning severity, not Error",
  );

  console.log(`PASS: broken-skill fixture published ${diagnostics.length} real diagnostic(s)`);
}

// testReferencedFileDiagnosticsAndClearing closes a real, previously
// untested gap: extension.ts's lastReportedURIsBySkillDir tracking exists
// specifically to attribute a referenced file's own issues to that
// file's own URI (not fold them into SKILL.md's), and to clear them once
// fixed — neither had ever actually run before this test. The fixture
// (skill-with-referenced-issue) has a clean SKILL.md that only
// references scripts/install.sh, which itself contains a real
// ast01-pipe-to-shell violation.
async function testReferencedFileDiagnosticsAndClearing(): Promise<void> {
  const skillDir = path.resolve(__dirname, "../../../test-fixtures/skill-with-referenced-issue");
  const skillMdUri = vscode.Uri.file(path.join(skillDir, "SKILL.md"));
  const scriptUri = vscode.Uri.file(path.join(skillDir, "scripts", "install.sh"));
  const scriptPath = scriptUri.fsPath;
  const originalScriptContent = fs.readFileSync(scriptPath, "utf8");

  try {
    const doc = await vscode.workspace.openTextDocument(skillMdUri);
    await vscode.window.showTextDocument(doc);

    const scriptDiagnostics = await waitForSkillciDiagnostics(scriptUri);
    assert.ok(
      scriptDiagnostics.length > 0,
      `expected at least one skillci diagnostic on ${scriptPath} (the referenced file, not SKILL.md) within 15s, got none`,
    );
    const pipeToShell = scriptDiagnostics.find((d) => d.code === "ast01-pipe-to-shell");
    assert.ok(
      pipeToShell,
      `expected an ast01-pipe-to-shell diagnostic on the referenced script, got: ${JSON.stringify(scriptDiagnostics.map((d) => d.code))}`,
    );

    // SKILL.md itself is clean per the fixture's own real skillci output
    // (verified with the compiled binary before writing this test) —
    // the referenced file's issues must not have been folded into it.
    const skillMdDiagnostics = vscode.languages.getDiagnostics(skillMdUri).filter((d) => d.source === "skillci");
    assert.equal(
      skillMdDiagnostics.length,
      0,
      `expected no skillci diagnostics on SKILL.md itself, got: ${JSON.stringify(skillMdDiagnostics.map((d) => d.code))} — the referenced file's issues must be attributed to its own URI, not folded into SKILL.md's`,
    );

    // Fix the referenced file on disk (skillci reads scripts/install.sh
    // fresh from disk on every check run, not from any open editor
    // buffer) and re-save SKILL.md to trigger another lint pass — insert
    // a harmless trailing space and immediately delete it, guaranteeing
    // two real save events fire (a genuinely no-op save() may not fire
    // onDidSaveTextDocument the same way).
    fs.writeFileSync(scriptPath, "#!/usr/bin/env bash\necho 'hello'\n");
    const endOfDoc = doc.lineAt(doc.lineCount - 1).range.end;
    const insertEdit = new vscode.WorkspaceEdit();
    insertEdit.insert(skillMdUri, endOfDoc, " ");
    await vscode.workspace.applyEdit(insertEdit);
    await doc.save();
    const newEndOfDoc = doc.lineAt(doc.lineCount - 1).range.end;
    const deleteEdit = new vscode.WorkspaceEdit();
    deleteEdit.delete(skillMdUri, new vscode.Range(newEndOfDoc.translate(0, -1), newEndOfDoc));
    await vscode.workspace.applyEdit(deleteEdit);
    await doc.save();

    const remaining = await waitForNoSkillciDiagnostics(scriptUri);
    assert.equal(
      remaining.length,
      0,
      `expected the fixed referenced file's skillci diagnostics to clear within 15s, still had: ${JSON.stringify(remaining.map((d) => d.code))} — a fixed issue must not leave a stale diagnostic behind forever`,
    );

    console.log("PASS: referenced-file diagnostics attributed to their own URI and cleared once fixed");
  } finally {
    // Always restore the fixture's committed content, regardless of
    // pass/fail — this file is git-tracked, not a throwaway temp dir.
    fs.writeFileSync(scriptPath, originalScriptContent);
  }
}
