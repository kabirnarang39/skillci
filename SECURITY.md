# Security Policy

## Supported Versions

skillci is pre-1.0 and moves fast. Only the latest tagged release is
supported — please update before reporting an issue that might already be
fixed:

```bash
go install github.com/kabirnarang39/skillci/cmd/skillci@latest
# or: brew upgrade skillci  /  re-download the latest release
```

## Reporting a Vulnerability

**Preferred: GitHub's private vulnerability reporting.** Go to the
[Security tab](https://github.com/kabirnarang39/skillci/security) →
"Report a vulnerability". This opens a private advisory only you and the
maintainer can see — not a public issue.

**Alternative:** email kabir.narang@zinier.com with a description and, if
possible, a minimal reproduction.

Please do not open a public GitHub issue for a suspected vulnerability.

## What to Expect

This is a solo-maintained open-source project, not a company with a
formal SLA — there's no guaranteed response time. In practice: expect an
initial acknowledgment within a few days, and a fix or mitigation
prioritized ahead of other work once confirmed. You'll be credited in the
advisory and changelog unless you ask not to be.

## Scope

skillci is a **first-layer static scanner and CI test runner** for Claude
Skills, not a sandbox or a malware detector — see the
[README's security section](README.md#quick-start) for exactly which
[OWASP Agentic Skills Top 10](https://owasp.org/www-project-agentic-skills-top-10/)
categories `skillci check` actually covers (6 of 10) and which it
deliberately doesn't (the other 4 need registry/sandbox/organizational
infrastructure this project isn't). A report that a crafted `SKILL.md` can
bypass a specific lint rule's pattern matching is in scope and useful — a
report that skillci doesn't sandbox skill execution or verify supply-chain
provenance is a known, documented limitation, not a vulnerability.

Also in scope: the hosted dashboard (`cmd/skillci-server`) — auth bypass,
token scope escape, injection in the ingest/query paths.
