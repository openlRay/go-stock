# Backend Development Guidelines

> Best practices for backend development in this project.

---

## Overview

This directory contains guidelines for backend development. Fill in each file with your project's specific conventions.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | To fill |
| [Database Guidelines](./database-guidelines.md) | ORM patterns, queries, migrations | To fill |
| [Error Handling](./error-handling.md) | Error types, handling strategies | To fill |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | To fill |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging, log levels | To fill |
| [Web Runtime and Docker Contract](./web-runtime.md) | Web build tags, RPC/SSE boundary, and persistent runtime paths | Active |
| [Feishu Webhook Signing](./feishu-webhook.md) | Custom bot signature generation, raw-message signing, and safe validation | Active |
| [AI Model Configuration Contract](./ai-model-configuration.md) | Capability-driven fields, immediate CRUD, reasoning overrides, and request mapping | Active |
| [Announcement AI Analysis Contract](./announcement-ai-analysis.md) | Source-only PDF analysis, context preflight, one-request streaming, and latest-result persistence | Active |
| [Cron Schedule Editor Contract](./cron-schedule-editor.md) | Common schedules, optional AI filling, expert Cron, bindings, and fixed China Standard Time | Active |

---

## How to Fill These Guidelines

For each guideline file:

1. Document your project's **actual conventions** (not ideals)
2. Include **code examples** from your codebase
3. List **forbidden patterns** and why
4. Add **common mistakes** your team has made

The goal is to help AI assistants and new team members understand how YOUR project works.

---

**Language**: All documentation should be written in **English**.
