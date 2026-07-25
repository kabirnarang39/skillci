import * as path from "node:path";
import { runTests } from "@vscode/test-electron";

async function main(): Promise<void> {
  // Deliberately does NOT point at the user's own installed VS Code
  // profile/extensions — @vscode/test-electron's default behavior
  // downloads and launches a separate, isolated test build of VS Code
  // into .vscode-test/ (gitignored), so this never touches the user's
  // real settings, installed extensions, or open windows.
  const extensionDevelopmentPath = path.resolve(__dirname, "../../");
  const extensionTestsPath = path.resolve(__dirname, "./suite/index");
  const testWorkspace = path.resolve(__dirname, "../../test-fixtures/broken-skill");

  // The fixture's SKILL.md deliberately has no committed .vscode/settings.json
  // pointing skillci.path at a hardcoded path — that would only work on the
  // machine that wrote it. The test suite (src/test/suite/index.ts) instead
  // sets skillci.path programmatically via the configuration API, targeting
  // the isolated test profile — not PATH: VS Code resolves its own PATH
  // from the user's login shell on macOS, which can win over anything
  // passed here and, on a machine with more than one skillci install
  // (e.g. an older Homebrew-tapped one), silently picks the wrong binary.
  await runTests({
    extensionDevelopmentPath,
    extensionTestsPath,
    launchArgs: [testWorkspace, "--disable-extensions"],
  });
}

main().catch((err) => {
  console.error("integration test run failed:", err);
  process.exit(1);
});
