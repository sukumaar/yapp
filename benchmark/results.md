# JDK 25 install benchmark

Single cold-cache run on Debian Linux x86_64. YAPP installed Eclipse Temurin JDK `25.0.4.1+1`; Homebrew installed OpenJDK `25.0.4.1`.

```text
YAPP      ███                                        4.20s
Homebrew  ██████████████████████████████████████████ 59.04s
```

YAPP's JDK archive was not cached. Homebrew used a fresh temporary download cache. Homebrew's timed command installed 32 dependencies. Both JDK installs exited successfully. The observed elapsed time was about 14.1 times lower for YAPP on this machine.

This compares the full install workflows, including each tool's dependencies. It is one machine and one run, and does not isolate Go's contribution. YAPP installs Temurin; Homebrew installs its OpenJDK build.

An earlier cold-cache attempt also installed the same 32 Homebrew dependencies, and upgraded seven previously installed formulae. It took 4.396 seconds with YAPP and 73.992 seconds with Homebrew. Homebrew returned exit code 1 because it could not link an existing `pip3.14` path; the JDK itself installed. This attempt is not used in the graph. See [earlier results](earlier-attempt.json) and the [YAPP](earlier-yapp-attempt.log) and [Homebrew](earlier-brew-attempt.log) logs.

Both JDKs, YAPP's JDK archive, and the 32 Homebrew dependency formulae installed for the measurement were removed afterward. Homebrew left a D-Bus configuration directory, as recorded in `cleanup.log`.

Re-run with `python3 run_benchmark.py`. The script writes `yapp.log`, `brew.log`, `cleanup.log`, `results.json`, and this report. It uses a temporary YAPP home and fresh temporary `HOMEBREW_CACHE`, and refuses to run if `openjdk@25` is already installed.
