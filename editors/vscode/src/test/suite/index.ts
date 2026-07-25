// Real integration test: launched by runTest.ts inside an actual VS Code
// Extension Development Host (via @vscode/test-electron), with this
// extension genuinely activated and its real vscode API calls executing
// — not a mock. Deliberately hand-written with plain node:assert rather
// than pulling in Mocha as a dependency: @vscode/test-electron only
// requires the module at extensionTestsPath to export an async run(), it
// has no opinion about what runs inside it.
import * as assert from "node:assert/strict";
import * as path from "node:path";
import * as vscode from "vscode";

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

  const fixtureUri = vscode.Uri.file(
    path.resolve(__dirname, "../../../test-fixtures/broken-skill/SKILL.md"),
  );

  const doc = await vscode.workspace.openTextDocument(fixtureUri);
  await vscode.window.showTextDocument(doc);

  // The extension's onDidOpenTextDocument handler spawns `skillci check`
  // asynchronously; poll for diagnostics to appear rather than a fixed
  // sleep, with a generous ceiling since this is a real child process
  // launch inside a real Electron extension host, not an in-memory call.
  //
  // Filtered to source === "skillci": vscode.languages.getDiagnostics
  // returns diagnostics from every contributor on this URI, not just this
  // extension's — VS Code's own built-in Markdown language features (this
  // fixture is a .md file) can independently notice and report the same
  // "missing description" gap in their own words, which would otherwise
  // let this test pass even if this extension's own diagnostics never
  // published at all.
  const deadline = Date.now() + 15_000;
  let diagnostics: vscode.Diagnostic[] = [];
  while (Date.now() < deadline) {
    diagnostics = vscode.languages.getDiagnostics(fixtureUri).filter((d) => d.source === "skillci");
    if (diagnostics.length > 0) {
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

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

  console.log(`PASS: extension published ${diagnostics.length} real diagnostic(s) from a real skillci check run`);
}
