# YAPP

YAPP (Yet Another Package Provisioner) installs developer tools under `~/.yapp` and adds their commands to your shell.

The project is in early development. On Linux amd64, you can install Eclipse Temurin JDK 25, Apache Maven 3.9.16, and Node.js 24.21.0 with bundled npm 11.19.0. Shell setup supports Bash and Zsh.

## Why YAPP?

Install Maven with `yapp install maven`; YAPP won't download Java or stop because Java is missing.

Maven needs Java to run. If YAPP can't find or verify a compatible version, it suggests `yapp install jdk25`. You choose whether to run it.

Available tools come from a shared catalog in this GitHub repository. More tools, including Scala, are planned.

## Install YAPP from source

From the repository root, run:

```sh
bash scripts/install.sh
# or show installer options and requirements
bash scripts/install.sh --help
```

The script shows colored build stages, streams Go build output, and displays curl's download bar. It builds YAPP under `~/.yapp/lib/yapp` and links the command as `~/.yapp/bin/yapp`. It uses Go from `PATH` when a compatible version is available. Otherwise, it asks before downloading a checksum-verified Go 1.27.1 toolchain temporarily for the build; the pinned release and checksums are from the [official Go downloads](https://go.dev/dl/). It asks before replacing an existing YAPP binary or command link, leaves other files in `~/.yapp` untouched, creates `~/.yapp/yapp-env.sh`, and adds one source line to Bash or Zsh startup files. YAPP refreshes the environment file after app installs and removals, including `JAVA_HOME`. The temporary Go download supports Linux and macOS on amd64 and arm64; `bash`, `curl`, `tar`, and `sha256sum` or `shasum` are required.

## Using YAPP

Use these commands:

```sh
yapp install jdk25
yapp install maven
yapp install node24
yapp info node24
yapp version
yapp help
```

YAPP checks each download's checksum. The JDK provides `java`, `javac`, `javap`, `jar`, and `jshell` in `~/.yapp/bin`; Maven provides `mvn`; Node.js provides `node`, `npm`, and `npx`.

Verified download archives stay in `~/.yapp/cache` and are reused on later installs. Before reuse, YAPP hashes the archive contents and compares the result with the catalog SHA-256. If it differs, YAPP deletes the bad archive and downloads it again. After each successful install, YAPP removes cached archives older than 30 days. It skips an archive that an active install is using.

After installing, open a new terminal or reload your shell setup:

```sh
source ~/.bashrc  # Bash
# or
source ~/.zshrc   # Zsh
```

Once `~/.yapp/bin` is on your PATH, new commands are available right away. Reload your shell after environment changes such as `JAVA_HOME`.

For installations from an earlier YAPP build, rerun the install commands to add command links without downloading the tools again. Open a new terminal to load the updated PATH.

To remove a tool:

```sh
yapp uninstall maven
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
