# YAPP

**Yet Another Package Provisioner** is a package manager in development for the tools developers use every day. YAPP aims to make it simple to install and update tools such as Java, Scala, and Maven, while keeping them together under `~/.yapp`.

## Why YAPP?

Installing a developer tool can pull in software you did not ask for, or leave you juggling versions across different projects. YAPP is being built to give you one place to manage your tools and their versions, and to make installed commands available on your `PATH`.

YAPP will install each requested app on its own. For example, `yapp install maven` will not install Java or fail because Java is missing. Maven may still need Java when you run it; YAPP leaves that runtime choice and setup to you.

Tool downloads will come from a shared catalog hosted on GitHub. That catalog can grow as developers add tools and keep download versions current.

## Using YAPP

YAPP's commands will be familiar if you have used Homebrew:

```sh
yapp install jdk25
yapp install maven
yapp uninstall maven
yapp version
```

Available install targets are Eclipse Temurin JDK 25 and Apache Maven 3.9.16 for Linux amd64. Installing either creates `~/.yapp`, verifies the download, and configures environment variables and `PATH` through your Bash or Zsh startup file. When both are installed, Java is placed before Maven on `PATH`. Open a new shell or source your startup file after installation. YAPP never installs dependencies automatically. Installing Maven does not require Java to already be installed; if Java is not detected, YAPP suggests the `yapp install jdk25` command.

Use `yapp uninstall <app>` to remove an app installed by YAPP and refresh its shell configuration.

Other commands are planned:

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

Commands and behavior are still being designed and may change.

## Installation

YAPP is in early development. Temurin JDK 25 installation is implemented for Linux amd64; other app-management commands and platform artifacts are still in progress.

## License

YAPP is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
