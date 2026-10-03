# YAPP — a focused developer toolchain manager

YAPP installs the language runtimes, SDKs, and build tools developers use to build software. Its curated catalog focuses on development toolchains instead of trying to cover every command-line utility. YAPP installs pinned releases under `~/.yapp`, verifies downloads, and makes selected commands available in your shell.

The project is in early development.

| App ID | Software | Version | Commands |
| --- | --- | --- | --- |
| `jdk@25` | Eclipse Temurin JDK | 25 | `java`, `javac`, `javap`, `jar`, `jshell` |
| `maven@3` | Apache Maven | 3.9.16 | `mvn` |
| `node@24` | Node.js | 24.21.0 (includes npm 11.19.0) | `node`, `npm`, `npx` |
| `python-standalone@3` | Python (Astral standalone) | 3.14.8 | `python3`, `python3.14`, `pip3` |
| `sbt@2` | sbt | 2.0.9 | `sbt` |
| `go@1` | Go | 1.27.1 | `go`, `gofmt` |
| `rust@1` | Rust | 1.98.0 | `rustc`, `cargo` |
| `scala@3` | Scala | 3.9.0 LTS | `scala`, `scalac`, `scaladoc` |

Setup supports Bash and Zsh in macOS and Linux.

## A focused toolchain catalog

YAPP is for the tools you use to write, run, compile, test, and package software: language runtimes, SDKs, and build tools. The catalog stays focused on that job. Each entry pins a release and records its upstream archive, checksum, install location, commands, and runtime requirements.

Installations live under `~/.yapp`, and selected commands are linked into `~/.yapp/bin`. Installing Maven, for example, does not silently install Java; if no compatible JDK is available, YAPP suggests `yapp install jdk@25` and leaves the choice to you.

Catalog entries use final stable releases only; YAPP does not pin alpha, beta, or release-candidate versions. Scala 3.9.0 is the current Scala LTS. Python, sbt, Go, and Rust do not use an upstream LTS designation, so YAPP pins their current stable releases. The Python catalog contains only the newest available Python version overall: if the choices are 3.13.3, 3.12.10, and 3.14.8, it includes only 3.14.8. Python is provided by Astral's standalone CPython builds.

## Install YAPP from source

From the repository root, run:

```sh
bash scripts/install.sh
# or show installer options and requirements
bash scripts/install.sh --help
```

The installer:

- Builds YAPP at `~/.yapp/lib/yapp` and links the command from `~/.yapp/bin/yapp`.
- Uses a compatible Go compiler from `PATH`, or asks before downloading the checksum-verified Go 1.27.1 toolchain from the [official Go downloads](https://go.dev/dl/).
- Asks before replacing an existing YAPP binary or command link and leaves other files in `~/.yapp` untouched.
- Creates `~/.yapp/yapp-env.sh` and adds one source line to the shell startup file: `.bashrc` on Linux or `.bash_profile` on macOS for Bash; `.zshrc` on Linux or `.zprofile` on macOS for Zsh.
- Shows colored build stages, Go build output, and curl's download progress.

The temporary Go download supports Linux and macOS on amd64 and arm64. The installer requires `bash`, `curl`, `tar`, and `sha256sum` or `shasum`.

## Using YAPP

Use these commands:

```sh
yapp install jdk@25
yapp install maven@3
yapp install node@24
yapp install python-standalone@3
yapp install sbt@2
yapp install go@1
yapp install rust@1
yapp install scala@3
yapp info node@24
yapp version
yapp help
```

YAPP checks each download's checksum. The JDK provides `java`, `javac`, `javap`, `jar`, and `jshell` in `~/.yapp/bin`; Maven provides `mvn`; Node.js provides `node`, `npm`, and `npx`; Python provides `python3`, `python3.14`, and `pip3`; Go provides `go` and `gofmt`; Rust provides `rustc` and `cargo`; Scala provides `scala`, `scalac`, and `scaladoc`.

The catalog also defines the short aliases `go` → `go@1` and `rust` → `rust@1`. You can install those tools with `yapp install go` or `yapp install rust`; YAPP records each installation under its versioned catalog ID.

Verified download archives stay in `~/.yapp/cache` and are reused on later installs. Before reuse, YAPP hashes the archive contents and compares the result with the catalog SHA-256. If it differs, YAPP deletes the bad archive and downloads it again. After each successful install, YAPP removes cached archives older than 30 days. It skips an archive that an active install is using.

After installing, open a new terminal or load YAPP's environment into the current shell:

```sh
. "${HOME}/.yapp/yapp-env.sh"
yapp
```

Once `~/.yapp/bin` is on your PATH, new commands are available right away. Reload your shell after environment changes such as `JAVA_HOME`.

For installations from an earlier YAPP build, rerun the install commands to add command links without downloading the tools again. Open a new terminal to load the updated PATH.

To remove a tool:

```sh
yapp uninstall maven@3
```

This removes Maven and its command links, then updates the shell setup. Other tools remain installed.

To remove YAPP itself, manual removal is preferred: remove the YAPP source line (`. "${HOME}/.yapp/yapp-env.sh"`) from your Bash or Zsh startup file, then delete YAPP's directory:

```sh
rm -rf "$HOME/.yapp"
```

This deletes YAPP, installed tools, and cached archives. The separate `scripts/uninstall.sh` is available if you prefer an interactive option: it asks before deleting `~/.yapp` and everything inside it, then separately asks whether to remove the source line from Bash or Zsh startup files. A declined prompt cancels the uninstall.

```sh
bash scripts/uninstall.sh
```

If a command name is already taken in `~/.yapp/bin`, YAPP reports the conflict and preserves the existing file or link. The app remains installed. Resolve the conflict, then rerun its install command to create the links.

## Planned commands

| Command | What it does |
| --- | --- |
| `yapp list` | See the tools you have installed. |
| `yapp search <term>` | Find a tool in the catalog. |
| `yapp update` | Get the latest app catalog. |
| `yapp outdated` | Check which installed tools have updates. |
| `yapp upgrade [app]` | Upgrade one tool or all installed tools. |
| `yapp doctor` | Check for common setup problems. |
| `yapp cleanup` | Remove cached downloads that are no longer needed. |

Commands and behavior may change during development.

## License

YAPP is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
