# skillci on Azure Pipelines

No task/extension needed — Microsoft-hosted `ubuntu-latest` agents ship
Go preinstalled, so `skillci` installs and runs as plain script steps.

```yaml
# azure-pipelines.yml
trigger:
  - main

pool:
  vmImage: ubuntu-latest

steps:
  - script: go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1
    displayName: Install skillci
  - script: skillci check path/to/your-skill
    displayName: Lint skill
```

Pin the version (`@v0.4.1` above), not `@latest` — floating onto whatever
ships next silently changes your pipeline's behavior with no build
reproducibility. Check
[the latest release](https://github.com/kabirnarang39/skillci/releases/latest)
before bumping.

## Piloting non-blocking first

Rolling `skillci check` into an existing pipeline for the first time?
Start with `--mode warn` so findings are visible without blocking the
build, then drop the flag (or promote specific rules via
`.skillci.yaml`'s `lint.rules`) once the team's ready to enforce it:

```yaml
  - script: skillci check --mode warn path/to/your-skill
    displayName: Lint skill (non-blocking pilot)
```

## Full eval/regress against a model matrix

Needs `ANTHROPIC_API_KEY` as a
[secret pipeline variable](https://learn.microsoft.com/en-us/azure/devops/pipelines/process/set-secret-variables)
(never checked into `azure-pipelines.yml` directly):

```yaml
steps:
  - script: go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1
    displayName: Install skillci
  - script: skillci regress path/to/your-skill
    displayName: Regression test against model matrix
    env:
      ANTHROPIC_API_KEY: $(ANTHROPIC_API_KEY)
```

`skillci regress` only fails on a *new* regression by default (see the
main [README](../../README.md#quick-start) for `fail_on` policy options)
— a case with no prior recorded run gets proposed as a generated case
instead of failing the build outright.

## Skipping `go install` for a faster job

`go install` compiles from source on every run. For a faster pipeline,
pull a prebuilt binary from the
[releases page](https://github.com/kabirnarang39/skillci/releases/latest)
instead — every release ships an SPDX SBOM and a cosign-signed
`checksums.txt` (see the main README's
[Install section](../../README.md#install) for verification steps):

```yaml
steps:
  - script: |
      curl -sSfL -o skillci.tar.gz https://github.com/kabirnarang39/skillci/releases/download/v0.4.1/skillci_0.4.1_linux_amd64.tar.gz
      tar -xzf skillci.tar.gz skillci
    displayName: Download skillci
  - script: ./skillci check path/to/your-skill
    displayName: Lint skill
```
