# YAPP

YAPP (Yet Another Package Provisioner) installs developer tools under `~/.yapp` and adds their commands to your shell.

The project is in early development. You can currently install and uninstall Eclipse Temurin JDK 25 and Apache Maven 3.9.16 on Linux amd64, with shell setup for Bash and Zsh.

## Why YAPP?

Install Maven with `yapp install maven`; YAPP won't download Java or stop because Java is missing.

Maven needs Java to run. If YAPP can't find or verify a compatible version, it suggests `yapp install jdk25`. You choose whether to run it.

Available tools come from a shared catalog in this GitHub repository. More tools, including Scala, are planned.

## Using YAPP

Use these commands:

```sh
yapp install jdk25
yapp install maven
yapp version
yapp help
```

YAPP checks each download's checksum. The JDK provides `java`, `javac`, `javap`, `jar`, and `jshell` in `~/.yapp/bin`; Maven provides `mvn`.

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

If a command name is already taken in `~/.yapp/bin`, YAPP reports the conflict and preserves the existing file or link. The app remains installed. Resolve the conflict, then rerun its install command to create the links.

## Planned commands

| Command | What it does |
| --- | --- |
| `yapp list` | See the tools you have installed. |
| `yapp search <term>` | Find a tool in the catalog. |
| `yapp info <app>` | View details about a tool. |
| `yapp update` | Get the latest app catalog. |
| `yapp outdated` | Check which installed tools have updates. |
| `yapp upgrade [app]` | Upgrade one tool or all installed tools. |
| `yapp doctor` | Check for common setup problems. |
| `yapp cleanup` | Remove cached downloads that are no longer needed. |

Commands and behavior may change during development.

## License

YAPP is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
