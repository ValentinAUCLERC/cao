# cao

`cao` is a workspace-first home-state composer for dotfiles, configs, secrets, and user scripts.

It helps you build a clean local setup from small, reviewable workspaces instead of one giant machine-specific repo. Put your personal setup in one workspace, your work setup in another, keep secrets encrypted, and let `cao` materialize the right files on demand.

## Why cao

- Workspace-first by design: every workspace present in `workspaces/` is active by default.
- Friendly CLI for common cases: add files, secrets, and commands without hand-writing manifests.
- Safe local workflow: `plan`, `diff`, `apply`, and `prune` always operate from the workspaces you already have locally.
- Secrets live next to the rest of your setup: use `sops` and `age` without bolting on a separate system.
- Intentionally strict YAML: unknown fields, anchors, merge keys, and custom tags are rejected to keep things explicit and reviewable.

## Install

Homebrew:

```bash
brew install ValentinAUCLERC/tap/cao
```

Prebuilt binaries are also available from [GitHub Releases](https://github.com/ValentinAUCLERC/cao/releases).

Download the archive for your platform, extract `cao`, and place it in a directory already in your `PATH`, such as `~/.local/bin`.

If you prefer building from source:

```bash
go install github.com/ValentinAUCLERC/cao/cmd/cao@latest
```

## Quick start

Check your machine:

```bash
cao doctor
```

Create a workspace:

```bash
cao init perso
```

Add a managed file:

```bash
cao workspace perso files add \
  --input ~/.config/myapp/config.json \
  --target ~/.config/myapp/config.json
```

Preview and apply:

```bash
cao plan
cao apply
```

Want to bring in an existing setup instead of starting from scratch?

```bash
cao fetch git@github.com:me/cao-work.git work
```

## The model

The public model is intentionally simple:

- `cao` has its own base directory under XDG.
- Every workspace present in `workspaces/` is active by default.
- Simple cases use CLI-generated resources.
- `plan`, `apply`, `diff`, and `prune` always work from the locally present workspaces.

That makes the day-to-day flow straightforward: fetch or create a workspace, add resources, preview changes, then apply them locally.

## Runtime dependencies

End users do not need Go if they install a released binary.

Some workflows still rely on external tools:

- `git` for `cao fetch`
- `sops` for secret encryption and decryption
- `age` as the companion CLI for generating and managing age keys
- an age private key file for decrypting age-backed secrets, usually `~/.config/sops/age/keys.txt`

Run `cao doctor` to check the current machine. It reports missing dependencies and prints short fix hints when something is not configured yet.

## Base directories

By default `cao` stores data here:

- workspaces: `~/.local/share/cao/workspaces`
- generated files: `~/.config/cao/generated`
- state: `~/.local/state/cao/state.json`

`cao doctor` prints the exact paths for the current machine.

## Common commands

```bash
cao doctor
cao init perso
cao fetch git@github.com:me/cao-work.git work
cao workspace list
cao workspace rename perso personal
cao workspace show work
cao workspace work secrets add --input ~/Downloads/work-kubeconfig.yaml --format yaml --age age1...
cao workspace work secrets add --input .env --name mysql-root-password --no-target --age age1...
printf %s "$MYSQL_ROOT_PASSWORD" | cao workspace work secrets add --name mysql-root-password --stdin --no-target --age age1...
cao workspace work secrets get mysql-root-password > /tmp/mysql-root-password
cao workspace work command add --name kubectl-work --exec kubectl --env 'KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig'
cao workspace perso files add --input ~/.config/myapp/config.json --target ~/.config/myapp/config.json
cao workspace perso publish add --input ./scripts/devbox --name devbox
cao plan
cao diff
cao apply
cao prune
```

`cao apply` is the normal sync command. It writes the desired files and prunes stale managed files by default. Use `cao apply --no-prune` if you want to keep old managed artifacts around temporarily.

## Workspace layout

Each workspace lives under `~/.local/share/cao/workspaces/<name>` and looks like this:

```text
workspace.yaml
resources/
secrets/
files/
bin/
```

Minimal `workspace.yaml`:

```yaml
name: work
description: Professional environment
platforms:
  - linux
  - wsl
```

Simple resources live in `resources/*.yaml`:

```yaml
kind: secret
name: work-kubeconfig
source: secrets/work-kubeconfig.enc.yaml
target: ${XDG_CONFIG_HOME}/cao/generated/work/work-kubeconfig
mode: "0600"
format: yaml
```

Supported simple resource kinds:

- `secret`
- `file`
- `publish`

For tiny aliases or wrappers, prefer `cao workspace <name> command add`. It generates a small published script for you and still uses the same `publish` mechanism underneath.

For secret resources, `target` is optional:

- when `target` is present, `cao apply` materializes the decrypted secret there
- when `target` is omitted, the secret stays stored in the workspace and `cao apply` skips it
- `cao workspace <name> secrets get <secret>` can decrypt either kind on demand

`cao workspace <name> secrets add` accepts three plaintext sources:

- `--input <path>` for the existing file-based flow
- `--stdin` for direct values or multi-line content without leaking into shell history
- `--value <string>` when you really want an inline one-liner

When you use `--stdin` or `--value`, pass `--name` explicitly because there is no source filename to derive the resource name from.

CLI-generated materialized secret targets follow the same workspace-first layout:

- `~/.config/cao/generated/work/<name>`
- `~/.config/cao/generated/perso/<name>`

Kubeconfigs are plain YAML secrets in `cao`. Use `format: yaml` when you want to force structured encryption, or leave `format` unset and let `auto` infer it from the filename.

If you rename a workspace later, `cao workspace rename <old> <new>` updates the workspace folder, `workspace.yaml`, simple secret targets that still use the default layout, tracked state entries, and generated command wrappers that reference the old default generated path.

## Kubeconfig example

Personal workspace:

```bash
cao workspace perso secrets add \
  --input ~/Downloads/personal-kubeconfig.yaml \
  --format yaml \
  --age age1PERSONAL
```

Work workspace:

```bash
cao workspace work secrets add \
  --input ~/Downloads/work-kubeconfig.yaml \
  --format yaml \
  --age age1WORK
```

Stored-only example:

```bash
cao workspace work secrets add \
  --input .env \
  --name mysql-root-password \
  --no-target \
  --age age1WORK

printf %s "$MYSQL_ROOT_PASSWORD" | \
  cao workspace work secrets add \
  --name mysql-root-password \
  --stdin \
  --no-target \
  --age age1WORK

cao workspace work secrets get mysql-root-password > /tmp/mysql-root-password
```

After `cao apply`, you can use both with:

```bash
export KUBECONFIG="${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig:${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/perso/personal-kubeconfig"
kubectl config get-contexts
```

## YAML subset

Manifests are intentionally strict:

- unknown fields are rejected
- anchors and aliases are rejected
- merge keys (`<<`) are rejected
- custom YAML tags are rejected

That keeps behavior explicit and reviewable.
