// Pure-logic tests for lintResult.ts, using Node's built-in test runner
// (node:test) rather than adding a jest/vitest dependency for a handful
// of assertions on plain data transformation — no vscode module involved
// here, so these run outside the extension host entirely.
import { test } from "node:test";
import * as assert from "node:assert/strict";
import { parseIssues, severityForRule, groupByFile } from "./lintResult";

test("parseIssues returns empty array for empty stdout", () => {
  assert.deepEqual(parseIssues(""), []);
  assert.deepEqual(parseIssues("   \n"), []);
});

test("parseIssues returns empty array for JSON []", () => {
  assert.deepEqual(parseIssues("[]"), []);
});

test("parseIssues parses a real issue array and attaches severity", () => {
  const stdout = JSON.stringify([
    { file: "/tmp/SKILL.md", line: 2, rule: "missing-description", msg: "frontmatter must set description" },
  ]);
  const issues = parseIssues(stdout);
  assert.equal(issues.length, 1);
  assert.equal(issues[0].file, "/tmp/SKILL.md");
  assert.equal(issues[0].line, 2);
  assert.equal(issues[0].rule, "missing-description");
  assert.equal(issues[0].severity, "warning");
});

test("parseIssues throws on non-array JSON", () => {
  assert.throws(() => parseIssues('{"not": "an array"}'));
});

test("parseIssues throws on malformed JSON", () => {
  assert.throws(() => parseIssues("not json at all"));
});

test("severityForRule maps every ast0X-* rule to error", () => {
  assert.equal(severityForRule("ast01-pipe-to-shell"), "error");
  assert.equal(severityForRule("ast02-unpinned-dependency"), "error");
  assert.equal(severityForRule("ast03-broad-filesystem-access"), "error");
  assert.equal(severityForRule("ast04-yaml-anchor-alias"), "error");
  assert.equal(severityForRule("ast05-untrusted-external-instructions"), "error");
  assert.equal(severityForRule("ast10-absolute-path-reference"), "error");
});

test("severityForRule maps non-security rules to warning", () => {
  assert.equal(severityForRule("missing-description"), "warning");
  assert.equal(severityForRule("fuzz-strict-without-fuzz"), "warning");
  assert.equal(severityForRule("skill-bloat-oversized-body"), "warning");
});

test("severityForRule does not false-positive on a rule that merely contains 'ast' mid-word", () => {
  // A rule name like "contrast-something" must not match the ast0X-
  // prefix pattern just because it contains "ast" as a substring.
  assert.equal(severityForRule("contrast-something"), "warning");
});

test("groupByFile splits issues by their file field", () => {
  const issues = parseIssues(
    JSON.stringify([
      { file: "/skill/SKILL.md", line: 1, rule: "r1", msg: "m1" },
      { file: "/skill/scripts/install.sh", line: 3, rule: "ast01-pipe-to-shell", msg: "m2" },
      { file: "/skill/SKILL.md", line: 5, rule: "r3", msg: "m3" },
    ]),
  );
  const grouped = groupByFile(issues);
  assert.equal(grouped.size, 2);
  assert.equal(grouped.get("/skill/SKILL.md")?.length, 2);
  assert.equal(grouped.get("/skill/scripts/install.sh")?.length, 1);
});

test("groupByFile returns an empty map for an empty issue list", () => {
  assert.equal(groupByFile([]).size, 0);
});
