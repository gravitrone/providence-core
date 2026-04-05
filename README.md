# Providence Core

**The Profaned Core. One interface to command them all.**

## Overview

Providence Core is an **autonomous AI harness** that wraps Claude Code (and soon Codex, Gemini, local models) in a **unified flame-themed terminal interface** with animations, message steering, and features no single agent ships alone.

***Named after Providence, the Profaned Goddess from Terraria's Calamity mod. The theme runs deep.***

## Quickstart

Requires [Go 1.25+](https://go.dev/dl/).

```bash
go install github.com/gravitrone/providence@latest
```

Or from source:

```bash
git clone https://github.com/gravitrone/providence-core.git
cd providence-core
make setup
make install-bin
```

```bash
providence
```

## Why

Every AI coding agent ships its own mediocre TUI. Switch between Claude, Codex, Gemini - three different interfaces, three different workflows, no shared context. Providence wraps them all in one interface that's actually good to look at.

## Features

- **Flame-Themed TUI** - animated gradient borders, per-character shimmer, spring physics, ember breathing
- **Message Steering** - shift+enter to steer (priority), enter to queue. steered messages combine and send first
- **Engine Interface** - pluggable AI backends. Claude Code headless today, Codex and direct API planned
- **Slash Commands** - /model (switch haiku/sonnet/opus), /clear, /help
- **Wraps Claude Code** - runs Claude Code under the hood via headless mode. Codex, Gemini, direct API coming soon
- **Providence Theme** - Calamity-inspired naming, ASCII banner, pulse block spinner with divine verbs

## Vision

Where this is going - multi-engine support (Codex, Gemini, local models from one TUI), terminal visualizations via charm libraries, persistent codebase memory across sessions, smart model routing (haiku for simple, opus for complex), agent orchestration with isolated VMs spawned per task, and background loops that never stop working.

## License

[Apache 2.0](LICENSE)
