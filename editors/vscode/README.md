# SkillCI for VS Code

Live linting for `SKILL.md` files, powered by `skillci check --format json`.
Runs on open, on save, and (debounced) as you type — the same OWASP-mapped
security scan and skill-bloat checks the CLI runs, surfaced inline instead
of only in a terminal or CI log.

## Requirements

The [`skillci`](https://github.com/kabirnarang39/skillci) binary must be
installed and on your `PATH` (`brew install --cask skillci`, a
[prebuilt release binary](https://github.com/kabirnarang39/skillci/releases),
or `go install github.com/kabirnarang39/skillci/cmd/skillci@latest`). If it's
somewhere else, set `skillci.path` in your settings.

No `ANTHROPIC_API_KEY` needed — `check` is local-only, same as running it
from a terminal.

## Settings

| Setting | Default | What it does |
|---|---|---|
| `skillci.path` | `"skillci"` | Path to the binary, if not on your PATH |
| `skillci.lintOnType` | `true` | Re-lint on every edit (debounced), not just on save |
| `skillci.lintOnTypeDelayMs` | `500` | Debounce delay for lint-on-type |

## What you'll see

- Security findings (`ast01-*` through `ast10-*`) as **Errors**
- Everything else (missing frontmatter fields, skill-bloat warnings, path
  issues) as **Warnings**
- Issues found in a file the skill *references* (not just `SKILL.md`
  itself) appear on that file too

## Development

```bash
npm install
npm run compile   # or: npm run watch
npm test          # pure-logic unit tests (node:test, no extension host needed)
npm run package   # builds a real .vsix via @vscode/vsce
```

Press F5 in VS Code (with this folder open) to launch an Extension
Development Host for manual testing.
