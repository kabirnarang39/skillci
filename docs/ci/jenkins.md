# skillci on Jenkins

Jenkins's ecosystem is plugin-first by convention, but `skillci` is a
plain Go binary — a raw shell step in a declarative `Jenkinsfile` works
fine and needs no dedicated plugin. (If real demand for a native plugin
shows up, that's a fair ask to open an issue about — this doc is the
YAGNI default until then.)

## Using a Go container agent

Simplest if your Jenkins setup can run Docker-based agents:

```groovy
// Jenkinsfile
pipeline {
    agent {
        docker { image 'golang:1.25' }
    }
    stages {
        stage('Install skillci') {
            steps {
                sh 'go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1'
            }
        }
        stage('Lint skill') {
            steps {
                sh '$(go env GOPATH)/bin/skillci check path/to/your-skill'
            }
        }
    }
}
```

Pin the version (`@v0.4.1` above), not `@latest` — floating onto whatever
ships next silently changes your build's behavior with no reproducibility.
Check [the latest release](https://github.com/kabirnarang39/skillci/releases/latest)
before bumping.

## Piloting non-blocking first

Rolling `skillci check` into an existing Jenkinsfile for the first time?
Start with `--mode warn` so findings are visible without failing the
build, then drop the flag (or promote specific rules via
`.skillci.yaml`'s `lint.rules`) once the team's ready to enforce it:

```groovy
sh '$(go env GOPATH)/bin/skillci check --mode warn path/to/your-skill'
```

## Full eval/regress against a model matrix

Needs `ANTHROPIC_API_KEY` bound via Jenkins
[Credentials](https://www.jenkins.io/doc/book/using/using-credentials/)
(never hardcoded in the `Jenkinsfile`):

```groovy
pipeline {
    agent { docker { image 'golang:1.25' } }
    environment {
        ANTHROPIC_API_KEY = credentials('anthropic-api-key')
    }
    stages {
        stage('Install skillci') {
            steps { sh 'go install github.com/kabirnarang39/skillci/cmd/skillci@v0.4.1' }
        }
        stage('Regress against model matrix') {
            steps { sh '$(go env GOPATH)/bin/skillci regress path/to/your-skill' }
        }
    }
}
```

`skillci regress` only fails on a *new* regression by default (see the
main [README](../../README.md#quick-start) for `fail_on` policy options)
— a case with no prior recorded run gets proposed as a generated case
instead of failing the build outright.

## Skipping `go install` for a faster build, and non-Docker agents

`go install` compiles from source on every run, and not every Jenkins
agent runs Docker. For a faster build (or a plain shell/bare-metal
agent), pull a prebuilt binary from the
[releases page](https://github.com/kabirnarang39/skillci/releases/latest)
instead — every release ships an SPDX SBOM and a cosign-signed
`checksums.txt` (see the main README's
[Install section](../../README.md#install) for verification steps):

```groovy
pipeline {
    agent any
    stages {
        stage('Download skillci') {
            steps {
                sh '''
                    curl -sSfL -o skillci.tar.gz https://github.com/kabirnarang39/skillci/releases/download/v0.4.1/skillci_0.4.1_linux_amd64.tar.gz
                    tar -xzf skillci.tar.gz skillci
                '''
            }
        }
        stage('Lint skill') {
            steps { sh './skillci check path/to/your-skill' }
        }
    }
}
```
