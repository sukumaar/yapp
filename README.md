# YAPP

**Yet Another Package Puller** is a package manager in development for the tools developers use every day. YAPP aims to make it simple to install and update tools such as Java, Scala, and Maven, while keeping them together under `~/.yapp`.

## Why YAPP?

Installing a developer tool can pull in software you did not ask for, or leave you juggling versions across different projects. YAPP is being built to give you one place to manage your tools and their versions, and to make installed commands available on your `PATH`.

YAPP will install each requested app on its own. For example, `yapp install maven` will not install Java or fail because Java is missing. Maven may still need Java when you run it; YAPP leaves that runtime choice and setup to you.

Tool downloads will come from a shared catalog hosted on GitHub. That catalog can grow as developers add tools and keep download versions current.

## Using YAPP

YAPP's commands will be familiar if you have used Homebrew:

```sh
yapp install maven
yapp list
yapp outdated
yapp upgrade
```

The planned commands include:

| Command | What it does |
| --- | --- |
| `yapp install <app>` | Install a tool, such as Java, Scala, or Maven. |
| `yapp uninstall <app>` | Remove a tool installed by YAPP. |
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

YAPP is in early development. The CLI scaffold can show help and version information, but package installation and management are not implemented yet.

## License

YAPP is licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
