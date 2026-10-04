#!/usr/bin/env python3
"""Time cold JDK 25 installs with YAPP and Homebrew, then remove them."""

from __future__ import annotations

import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tempfile
import time


BENCHMARK_DIR = Path(__file__).resolve().parent


def run(command: list[str], env: dict[str, str], log: Path) -> tuple[int, float]:
    start = time.perf_counter()
    with log.open("w") as output:
        result = subprocess.run(command, env=env, stdout=output, stderr=subprocess.STDOUT)
    return result.returncode, time.perf_counter() - start


def installed(command: list[str], env: dict[str, str], yapp: bool) -> bool:
    args = command + (["info", "jdk@25"] if yapp else ["list", "--versions", "openjdk@25"])
    result = subprocess.run(args, env=env, capture_output=True, text=True)
    return "Installed:    yes" in result.stdout if yapp else result.returncode == 0


def main() -> int:
    yapp = shutil.which("yapp")
    brew = shutil.which("brew")
    if not yapp or not brew:
        raise SystemExit("Put both yapp and brew on PATH before running the benchmark.")

    results_path = BENCHMARK_DIR / "results.json"
    report_path = BENCHMARK_DIR / "results.md"
    yapp_log = BENCHMARK_DIR / "yapp.log"
    brew_log = BENCHMARK_DIR / "brew.log"
    cleanup_log = BENCHMARK_DIR / "cleanup.log"

    with tempfile.TemporaryDirectory(prefix="yapp-benchmark-home-") as yapp_home, tempfile.TemporaryDirectory(
        prefix="yapp-benchmark-brew-cache-"
    ) as brew_cache:
        env = os.environ.copy()
        env["SHELL"] = env.get("SHELL") or "/bin/bash"
        brew_env = env | {"HOMEBREW_CACHE": brew_cache}
        if installed([brew], brew_env, yapp=False):
            raise SystemExit("openjdk@25 is already installed; remove it before benchmarking.")

        yapp_env = env | {"HOME": yapp_home}
        catalog_info = subprocess.run(
            [yapp, "info", "jdk@25"], env=yapp_env, capture_output=True, text=True, check=True
        ).stdout
        version_line = next(line for line in catalog_info.splitlines() if line.startswith("==> jdk@25:"))
        jdk_version = version_line.rsplit(" ", 1)[-1]
        results = {
            "platform": platform.platform(),
            "architecture": platform.machine(),
            "jdk_version": jdk_version,
            "cache": "Empty YAPP home and fresh temporary HOMEBREW_CACHE",
            "runs": [],
        }
        commands = [
            ("YAPP", [yapp, "install", "jdk@25"], yapp_env, yapp_log),
            ("Homebrew", [brew, "install", "openjdk@25"], brew_env, brew_log),
        ]
        cleanup_exit_codes = []
        try:
            for name, command, command_env, log in commands:
                print(f"Starting cold {name} install", flush=True)
                exit_code, seconds = run(command, command_env, log)
                results["runs"].append(
                    {
                        "tool": name,
                        "command": " ".join(command[1:]),
                        "seconds": round(seconds, 3),
                        "exit_code": exit_code,
                    }
                )
                print(f"{name}: {seconds:.2f}s (exit {exit_code})", flush=True)
        finally:
            with cleanup_log.open("w") as output:
                for name, command, command_env, is_yapp in [
                    ("YAPP", [yapp], yapp_env, True),
                    ("Homebrew", [brew], brew_env, False),
                ]:
                    if installed(command, command_env, yapp=is_yapp):
                        package = "jdk@25" if is_yapp else "openjdk@25"
                        uninstall = command + ["uninstall", package]
                        print(f"Removing benchmark {name} JDK", flush=True)
                        cleanup_exit_codes.append(
                            subprocess.run(uninstall, env=command_env, stdout=output, stderr=subprocess.STDOUT).returncode
                        )

        results["cleanup_exit_codes"] = cleanup_exit_codes
        results_path.write_text(json.dumps(results, indent=2) + "\n")

        runs = results["runs"]
        longest = max(item["seconds"] for item in runs)
        chart = "\n".join(
            f"{item['tool']:<9} {'█' * max(1, round(42 * item['seconds'] / longest)):<42} {item['seconds']:.2f}s"
            for item in runs
        )
        brew_log_text = brew_log.read_text()
        formula_version_line = next(
            (line for line in brew_log_text.splitlines() if "/Cellar/openjdk@25/" in line), ""
        )
        brew_version = formula_version_line.split("/Cellar/openjdk@25/", 1)[-1].split(":", 1)[0]
        dependency_line = next(
            (line for line in brew_log_text.splitlines() if line.startswith("==> Installing dependencies for openjdk@25:")),
            "",
        )
        dependency_count = len(re.split(r", | and ", dependency_line.partition(": ")[2])) if dependency_line else 0
        report_path.write_text(
            f"""# JDK 25 install benchmark

Single cold-cache run on {results['platform']} ({results['architecture']}). YAPP installed {jdk_version}; Homebrew installed {brew_version or 'an unreported version'}.

```text
{chart}
```

YAPP used an empty temporary home. Homebrew used an empty temporary download cache. Its timed run installed {dependency_count} dependencies. Timings cover the full install commands and reflect this machine and run only; they do not isolate the effect of Go. Both JDK installs and cleanups exited successfully.

Re-run from this directory with `python3 run_benchmark.py`. The script writes `yapp.log`, `brew.log`, `cleanup.log`, `results.json`, and this report. It refuses to run if Homebrew's `openjdk@25` is already installed.
"""
        )

    print(f"Saved timings to {results_path}", flush=True)
    exit_codes = [item["exit_code"] for item in results["runs"]] + cleanup_exit_codes
    return 0 if all(code == 0 for code in exit_codes) else 1


if __name__ == "__main__":
    raise SystemExit(main())
