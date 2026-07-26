# skillci on GitLab CI

No plugin exists (or is needed) — `skillci` is a plain Go binary, so a
`.gitlab-ci.yml` job is a few lines: install it, run it.

```yaml
skillci-check:
  stage: test
  image: golang:1.25
  script:
    - go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1
    - skillci check path/to/your-skill
```

Pin the version (`@v0.4.1` above), not `@latest` — floating onto whatever
ships next silently changes your CI's behavior with no build
reproducibility. Check
[the latest release](https://github.com/kabirnarang39/skillci/releases/latest)
before bumping.

## Piloting non-blocking first

Rolling `skillci check` into an existing pipeline for the first time?
Start with `--mode warn` so findings are visible without blocking merges,
then drop the flag (or promote specific rules via `.skillci.yaml`'s
`lint.rules`) once the team's ready to enforce it:

```yaml
skillci-check:
  stage: test
  image: golang:1.25
  script:
    - go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1
    - skillci check --mode warn path/to/your-skill
```

## Full eval/regress against a model matrix

Needs `ANTHROPIC_API_KEY` as a
[masked CI/CD variable](https://docs.gitlab.com/ee/ci/variables/#for-a-project):

```yaml
skillci-regress:
  stage: test
  image: golang:1.25
  script:
    - go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1
    - skillci regress path/to/your-skill
  variables:
    ANTHROPIC_API_KEY: $ANTHROPIC_API_KEY
```

`skillci regress` only fails on a *new* regression by default (see the
main [README](../../README.md#quick-start) for `fail_on` policy options)
— a case with no prior recorded run gets proposed as a generated case
instead of failing the build outright.

## Skipping `go install` for a faster job

`go install` compiles from source on every run. For a faster job, pull a
prebuilt binary from the
[releases page](https://github.com/kabirnarang39/skillci/releases/latest)
instead — every release ships an SPDX SBOM and a cosign-signed
`checksums.txt` (see the main README's
[Install section](../../README.md#install) for verification steps):

```yaml
skillci-check:
  stage: test
  image: ubuntu:24.04
  script:
    - apt-get update && apt-get install -y curl
    - curl -sSfL -o skillci.tar.gz https://github.com/kabirnarang39/skillci/releases/download/v0.4.1/skillci_0.4.1_linux_amd64.tar.gz
    - tar -xzf skillci.tar.gz skillci
    - ./skillci check path/to/your-skill
```
