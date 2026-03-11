# SciCLI Release Setup

This document describes the minimum repository setup required to publish:

- GitHub release assets built from Go
- the public npm package `@scimate/scicli`

## Current Release Flow

The workflow is defined in `.github/workflows/release.yml`.

On every pushed git tag:

1. GitHub Actions checks out the repository.
2. `package.json` is synced to the git tag version.
3. GoReleaser builds and uploads platform binaries to the GitHub release.
4. npm publishes `@scimate/scicli`.

## Required Repository Settings

### 1. Default branch

Set the main development branch to `main` once you are ready to stop working from `scicli-implementation`.

### 2. Actions permissions

In GitHub repository settings:

- `Settings -> Actions -> General -> Workflow permissions`
- set workflow permissions to `Read and write permissions`

The release workflow uses the repository `GITHUB_TOKEN` to create release assets.

### 3. npm secret

Add this repository secret:

- `NPM_TOKEN`

Create it from npm:

1. Log in to npm with an owner account for the `@scimate` scope.
2. Create an automation token.
3. Save it to `Settings -> Secrets and variables -> Actions -> New repository secret`.

### 4. npm package ownership

Make sure the npm scope and package exist and the publishing account has rights:

- scope: `@scimate`
- package: `@scimate/scicli`

If the scope does not exist yet, create the org on npm first.

## Expected Release Assets

GoReleaser currently publishes assets named like:

- `scicli-windows-x86_64.zip`
- `scicli-windows-arm64.zip`
- `scicli-mac-x86_64.tar.gz`
- `scicli-mac-arm64.tar.gz`
- `scicli-linux-x86_64.tar.gz`
- `scicli-linux-arm64.tar.gz`

The npm installer downloads these assets during `npm install -g @scimate/scicli`.

## First Release Checklist

1. Merge the implementation branch into the branch you want to release from.
2. Confirm `package.json` package name is correct.
3. Confirm the GitHub repo is `SciMate-AI/scicli`.
4. Add the `NPM_TOKEN` secret.
5. Push a version tag:

```bash
git tag v0.0.55
git push origin v0.0.55
```

If you release from the `scimate` remote instead:

```bash
git tag v0.0.55
git push scimate v0.0.55
```

## Local Preflight

Before pushing a tag, run:

```bash
go test ./...
go build ./...
npm pack --dry-run
```

On this machine, Go commands need explicit cache/proxy env vars because the default user cache path is restricted.

## Current Limitations

- The Go module path is still `github.com/opencode-ai/opencode`, so `go install` is not yet aligned with the SciCLI repo path.
- The npm package path is ready, but the first public publish still depends on npm scope ownership and `NPM_TOKEN`.
- Homebrew and AUR publishing were removed from the automated release path to keep the first release minimal and reliable.
