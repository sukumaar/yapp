# YAPP Architecture

This document is for contributors and maintainers. It records the current design and implementation status; proposed behavior is identified as such.

## Go project structure

YAPP is implemented in Go as a single CLI binary. The repository uses a small `cmd/` and `internal/` layout:

```text
cmd/yapp/                 # Executable entry point and process lifecycle
internal/cli/             # Command parsing and user-facing CLI behavior
internal/buildinfo/       # Version metadata embedded at build time
internal/catalog/         # Validated, embedded YAML app catalog
internal/installer/       # HTTPS download, checksum verification, safe extraction
internal/state/           # Local installation registry
internal/shellenv/        # Bash/Zsh PATH integration
Makefile                  # Local build with linker-injected version metadata
```

Keep application packages under `internal/` until there is a concrete, stable API that other Go modules need to import. Add focused packages as real responsibilities are implemented; avoid placeholder packages and unnecessary abstraction layers. The module path is `github.com/sukumaar/yapp`.

The initial module targets Go 1.27. Release builds should embed version, commit, and build-time metadata using linker flags. The CLI currently implements help, version, and installation of the catalog's Linux amd64 JDK 25 artifact. Other package-management commands and platform artifacts remain unimplemented.

## Goals

- Install and manage developer tools, initially including Java, Scala, and Maven.
- Keep YAPP-managed files and metadata under `~/.yapp`.
- Use a community-maintained GitHub catalog to describe available apps and their downloadable artifacts.
- Track local installations and artifact hashes so YAPP can report and verify managed tools.
- Install apps independently without resolving, installing, or requiring their runtime dependencies.
- Make installed app commands available through `PATH`.
- Provide a familiar CLI, with command names influenced by Homebrew.

## High-level model

YAPP has two sources of configuration:

1. The **app catalog** is shared and version controlled in GitHub. It describes app versions and the platform-specific artifacts needed to install them.
2. The **local state** is generated and maintained on the user's machine. It records YAPP-managed installations and the hash of each installed artifact.

The CLI reads the catalog to select an app version and matching artifact, downloads and verifies it, installs it under the YAPP home directory, exposes its commands through `PATH`, and records the result in local state. It does not resolve or install runtime dependencies for that app.

```text
YAPP repository/
└── internal/catalog/catalog.yaml # Shared catalog source in this repository

~/.yapp/
├── .yapp_config           # Local install state (JSON)
├── yapp-env.sh            # Generated JAVA_HOME and PATH settings
├── apps/                  # YAPP-managed installations
└── cache/                 # Optional cached downloads
```

The catalog is embedded into the YAPP binary at build time. Updating a catalog entry currently requires rebuilding YAPP; a separate catalog refresh command is planned. The initial JDK entry supports Linux amd64 only.

## Shared app catalog

The catalog lives in `internal/catalog/catalog.yaml` in this GitHub repository so contributors can add apps and maintain download links through pull requests. It is embedded into release binaries. YAML is used because catalog entries are reviewed and edited by people; the schema is strict and unknown fields are rejected.

An illustrative entry:

```yaml
apps:
  jdk25:
    name: Eclipse Temurin JDK 25
    version: 25.0.4.1+1
    release_url: https://github.com/adoptium/temurin25-binaries/releases/tag/jdk-25.0.4.1%2B1
    artifacts:
      - os: linux
        arch: amd64
        format: tar.gz
        url: https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_linux_hotspot_25.0.4.1_1.tar.gz
        sha256: dbb698396d478e7fa2b1e50f4103324b2a99b90569ee27c33f2261f9215cf41e
        strip_components: 1
    install_path: apps/jdk25/25.0.4.1+1
```

Each entry pins one app version and provides platform-specific artifacts. The first entry supports Linux amd64. Catalog validation currently accepts HTTPS `tar.gz` artifacts, SHA-256 hashes, and a safe relative install path. YAPP verifies the archive before extraction. Catalog entries should use upstream artifact URLs and verified hashes. The catalog may document an app's external runtime needs for users, but YAPP will not use them to install or gate apps.

## Local state

The local state file will be `~/.yapp/.yapp_config`. It should record enough information for YAPP to list, verify, upgrade, and uninstall managed apps. Suggested fields include:

- App name and installed version
- Installation path
- Artifact URL and SHA-256 hash
- Platform and architecture
- Install time

JSON is used because this file is generated by YAPP and Go includes JSON support in its standard library. The local state is machine-specific and must not be committed to the shared catalog repository. Writes are atomic so an interrupted install does not leave a partially written state file.

Example shape (illustrative):

```json
{
  "schemaVersion": 1,
  "apps": {
    "jdk25": {
      "name": "Eclipse Temurin JDK 25",
      "version": "25.0.4.1+1",
      "path": "apps/jdk25/25.0.4.1+1",
      "artifactUrl": "https://github.com/adoptium/temurin25-binaries/releases/download/jdk-25.0.4.1%2B1/OpenJDK25U-jdk_x64_linux_hotspot_25.0.4.1_1.tar.gz",
      "sha256": "dbb698396d478e7fa2b1e50f4103324b2a99b90569ee27c33f2261f9215cf41e",
      "os": "linux",
      "arch": "amd64",
      "installedAt": "2026-10-01T00:00:00Z"
    }
  }
}
```

## Install and PATH flow

The proposed install flow is:

1. Load the catalog and validate the requested app and version.
2. Select the artifact matching the host operating system and architecture.
3. Download to a temporary/cache location and verify its checksum before extraction.
4. Install under `~/.yapp` and update `.yapp_config` only after successful installation.
5. Expose the app's commands through a YAPP-managed directory on `PATH`.

YAPP does not install an app's runtime dependencies or reject an app because those dependencies are absent. For example, installing Maven must succeed even if Java is not installed; running Maven may still require the user to install and configure Java separately.

For the JDK install, YAPP writes `~/.yapp/yapp-env.sh` with `JAVA_HOME` and a `PATH` entry for the selected JDK's `bin` directory. It adds an idempotent source block to the startup file for the shell named by `$SHELL` (`~/.bashrc` or `~/.zshrc`). It preserves existing `PATH` entries. Handling multiple versions of a command remains to be designed.

## CLI direction

Command names are intended to feel familiar to Homebrew users. The current public command proposal is listed in the [README](README.md). The main command responsibilities are:

- `install`: select the app artifact, fetch, verify, install, expose its commands on `PATH`, and record state. It does not install dependencies.
- `uninstall`: remove a YAPP-managed installation and update local state.
- `list`, `info`, and `search`: inspect installed apps and the shared catalog.
- `update`: refresh the local catalog copy.
- `outdated` and `upgrade`: compare installed versions with catalog versions and apply selected updates.
- `doctor`: diagnose local configuration and installation problems.
- `cleanup`: remove unused cached downloads without touching installed apps.

Command syntax and exact behavior are not finalized.

## Integrity and catalog maintenance

Each artifact should have a SHA-256 checksum in the catalog. YAPP should verify the downloaded file before extracting or installing it, and retain the checksum in local state for later inspection. Catalog changes should include the upstream URL, version, supported platform/architecture, and a verified checksum.

Because the URL and checksum are both distributed through the catalog, a checksum verifies artifact consistency against the catalog but does not independently authenticate a compromised catalog or upstream account. Catalog review and a future signing or provenance mechanism may be considered as the project matures.

Installer code must also:

- Create YAPP-owned directories with restrictive permissions and never follow paths outside `~/.yapp` when installing or uninstalling.
- Apply download timeouts and a reasonable maximum artifact size.
- Reject archive entries with absolute paths, `..` traversal, or symlinks that escape the install directory.
- Stage installs separately and publish them only after verification and extraction succeed.
- Update local state atomically, and only remove files that the local state identifies as YAPP-managed.
- Avoid executing scripts bundled with downloaded apps.

## Open design decisions

- Final catalog filename, format, and schema
- Final local state schema and migration policy
- How YAPP adds and removes its command directory from `PATH`
- How YAPP handles command-name collisions and multiple installed versions
- Whether to support project-local version selection or lockfiles
- Catalog caching and update behavior
- Supported operating systems and architectures for the first release
- Artifact signature or catalog signing strategy
