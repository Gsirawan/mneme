# Mneme

> _Mneme (NEE-mee) — Greek Muse of memory_

Persistent memory for AI coding sessions. Semantic search over your conversations, architecture decisions, and debugging sessions — so your next AI session picks up where the last one left off.

**100% local. Zero cloud cost. One Go binary. One SQLite file.**

## The Problem

Every AI coding session starts from zero. Your architecture decisions, debugging solutions, "why we chose X over Y" — all lost when the session ends. The next session reinvents the wheel.

Mneme fixes this. Ingest your session transcripts and project notes as markdown. Search them by meaning in your next session. Your AI remembers what you decided, what you tried, and what worked.

## Important: Mneme Is a 3-Way System

Mneme is **not** a standalone solution. It's one part of a three-way collaboration:

```
┌─────────┐      ┌─────────┐      ┌─────────┐
│   You   │◄────►│  Mneme  │◄────►│   AI    │
│ (Human) │      │ (Memory)│      │(Claude, │
│         │      │         │      │ etc.)   │
└─────────┘      └─────────┘      └─────────┘
```

- **Mneme** stores and retrieves chunks by semantic similarity. That's it. No reasoning. No synthesis.
- **Your AI** does the thinking — but only if it knows Mneme exists and when to search.
- **You** guide the search — your hints ("check the auth discussion from last week") make the difference between relevant results and noise.

All three must work together. Without AI instructions, Mneme never gets called. Without your guidance, searches return vague results. Without Mneme, every session starts from zero.

See [Getting the Most Out of Mneme](#getting-the-most-out-of-mneme) for setup details.

## Quick Start

```bash
# Build
git clone https://github.com/Gsirawan/mneme.git
cd mneme
go build -o mneme .

# Or install directly
go install github.com/Gsirawan/mneme@latest

# Pull embedding model
ollama pull qwen3-embedding:0.6b

# Ingest your notes
./mneme ingest --file decisions.md

# Search
./mneme search "why did we choose PostgreSQL over MongoDB"
```

## Architecture

```
Your AI Session → Mneme MCP (stdio) → SQLite + sqlite-vec → Ollama (embeddings)
```

- **SQLite + sqlite-vec** — vector search in a single file. No server. `cp` to backup.
- **Ollama** — local embeddings. Default: `qwen3-embedding:0.6b` (1024 dims). Any GPU.
- **MCP server** — your AI reads/writes memories through native tool calls.
- **No LLM in the loop** — Mneme retrieves raw chunks. Your AI does the thinking.

## Features

- **Semantic search** — find memories by meaning, not keywords
- **Temporal metadata** — date extraction from markdown headers (`## January 31, 2026 — 14:00`)
- **Date filtering** — `--as-of 2026-01-15` returns only memories from before that date
- **Entity aliases** — configure via `MNEME_ALIASES` so searching "React" also finds "ReactJS"
- **Section-aware chunking** — respects `##`/`###` markdown structure with sub-chunking for oversized sections
- **Re-ingestion** — delete-then-insert. Update a file, re-ingest, chunks refresh cleanly
- **Live session watcher** — auto-ingest for [OpenCode](https://github.com/sst/opencode) or [Claude Code](https://github.com/anthropics/claude-code) sessions in real-time
- **MCP server** — integrate directly with Claude Code, OpenCode, or any MCP-compatible client
- **Styled TUI** — colored terminal output with lipgloss

## Installation

### Prerequisites

- **Go 1.21+**
- **Ollama** running locally

### Build from source

```bash
git clone https://github.com/Gsirawan/mneme.git
cd mneme
go build -o mneme .

# Or install directly
go install github.com/Gsirawan/mneme@latest
```

### Configure

```bash
cp .env.example .env
```

Default configuration works out of the box. See [Configuration](#configuration) for customization.

## Usage

### Ingest markdown files

```bash
./mneme ingest --file architecture-decisions.md
./mneme ingest --file session-transcript.md --valid-at 2026-01-31
```

Mneme parses markdown by `##`/`###` headers, extracts dates from headers, and embeds each section locally.

### Search your memory

```bash
./mneme search "why did we choose event sourcing"
./mneme search --as-of 2026-01-15 "database migration strategy"
./mneme search --limit 20 "authentication flow"
```

### Track entity history

```bash
./mneme history "PostgreSQL"
./mneme history --limit 30 "auth module"
```

### Check system status

```bash
./mneme status
```

### MCP Server

Add to your `.mcp.json` (OpenCode, Claude Code, etc.):

```json
{
  "mcpServers": {
    "mneme": {
      "command": "/path/to/mneme",
      "args": ["serve"],
      "env": {
        "MNEME_DB": "/path/to/mneme.db"
      }
    }
  }
}
```

Your AI gets these tools:

| Tool            | Description                                               |
| --------------- | --------------------------------------------------------- |
| `mneme_search`  | Semantic search — returns relevant chunks chronologically |
| `mneme_ingest`  | Ingest a markdown file into memory                        |
| `mneme_history` | All mentions of an entity over time                       |
| `mneme_status`  | Health check and database stats                           |

### Getting the Most Out of Mneme

Mneme won't help unless your AI knows to use it and you help it search well. Two things to set up:

#### 1. Instruct Your AI to Use Mneme

Add something like this to your project instructions, system prompt, or `CLAUDE.md`:

```markdown
## Memory: Mneme

Mneme is your persistent memory. Use it to maintain context across sessions.

**The rule: when in doubt, search.**

- If a person, project, decision, or past task is mentioned → `mneme_search` before responding
- If a topic might have history → search first, think second
- Craft a deliberate query — not the user's raw words, but what you're actually looking for
- If the first search doesn't feel complete, search again with a different angle
- Use `mneme_history` for tracking how an entity evolved over time
- Never assume you have enough context from conversation alone
```

Without this, your AI may never call the Mneme tools — even though they're available.

#### 2. Guide the Search (The Human Role)

Semantic search finds meaning, not keywords — but it works best when you help your AI aim:

| Instead of...              | Try...                                                      |
| -------------------------- | ----------------------------------------------------------- |
| "What happened yesterday?" | "Search for the database migration discussion"              |
| "Do you remember?"         | "Check Mneme for the auth module decision from last sprint" |
| "What did we decide?"      | "Search for 'PostgreSQL vs MongoDB' trade-offs"             |

**Why this matters:** Your AI doesn't know what's in Mneme's database. You do — you lived those conversations. A specific hint ("search for the retry logic we built in the payment service") gets precise results. A vague question gets vague chunks.

The best workflow is collaborative: you hint, the AI searches, Mneme retrieves, the AI synthesizes. Three-way system.

### Live Session Watcher

```bash
# Watch an OpenCode session
./mneme watch-oc

# Watch a Claude Code session
./mneme watch-cc
```

Auto-discovers sessions from [OpenCode](https://github.com/sst/opencode) or [Claude Code](https://docs.anthropic.com/en/docs/claude-code), presents a picker, then polls for new messages. Every N messages (default: 6) get batched, embedded, and ingested. Includes preflight checks — starts Ollama if needed, pulls the model if missing, warms it into VRAM. Pending messages are flushed on Ctrl+C so nothing is lost.

## How It Works

### Markdown → Chunks → Vectors

```
## January 31, 2026                        ← extracted as valid_at: 2026-01-31
### Why We Chose Event Sourcing             ← section_title (preserved in sub-chunks)
We evaluated three approaches...            ← embedded as one chunk
```

- Splits at `##` and `###` headers
- Date cascade: `##` dates apply to all `###` sections beneath
- Sections over 600 words get sub-chunked with parent context preserved
- Each chunk embedded via Ollama → stored in sqlite-vec

### Retrieval

1. Query → embedded via Ollama
2. Cosine similarity search → top N chunks
3. Optional `as_of` date filter
4. Results sorted chronologically
5. Raw text returned — your AI synthesizes the answer

## Configuration

All configuration via environment variables or `.env` file:

| Variable          | Default                | Description                                |
| ----------------- | ---------------------- | ------------------------------------------ |
| `OLLAMA_HOST`     | `localhost:11434`      | Ollama server address                      |
| `MNEME_DB`        | `mneme.db`             | SQLite database path                       |
| `EMBED_MODEL`     | `qwen3-embedding:0.6b` | Embedding model name                       |
| `USER_ALIAS`      | `User`                 | Display name for human messages in watcher |
| `ASSISTANT_ALIAS` | `Assistant`            | Display name for AI messages in watcher    |
| `MNEME_ALIASES`   | _(empty)_              | Entity aliases for history search          |

### Entity Aliases

Configure aliases so searching one name finds all variants:

```bash
# Format: alias1=name1,name2;alias2=name3,name4
MNEME_ALIASES="react=React,ReactJS,react.js;pg=PostgreSQL,Postgres,psql"
```

Now `./mneme history "react"` finds mentions of React, ReactJS, and react.js.

## Commands

| Command                    | Description                                          |
| -------------------------- | ---------------------------------------------------- |
| `mneme ingest --file <md>` | Parse and ingest markdown (interactive confirmation) |
| `mneme search "<query>"`   | Semantic search with debug output                    |
| `mneme history "<entity>"` | Chronological mentions of an entity                  |
| `mneme status`             | System health, chunk count, date range               |
| `mneme serve`              | Start MCP stdio server                               |
| `mneme watch-oc`           | Live OpenCode session watcher with auto-ingestion    |
| `mneme watch-cc`           | Live Claude Code session watcher with auto-ingestion |

## Project Structure

```
mneme/
├── main.go          # CLI entry point, command routing
├── db.go            # SQLite + sqlite-vec initialization
├── ingest.go        # Markdown parsing, chunking, embedding, ingestion
├── search.go        # Vector similarity search with date filtering
├── history.go       # Entity history with configurable aliases
├── ollama.go        # Ollama client (embed + generate)
├── serve.go         # MCP server implementation
├── watch.go         # OpenCode live session watcher + preflight
├── cc-watch.go      # Claude Code live session watcher
├── status.go        # Health check
├── ui.go            # Terminal styling (lipgloss)
└── *_test.go        # Tests
```

## Use Cases

- **Architecture Decision Records** — ingest your ADRs, search "why did we choose X" in future sessions
- **Session Continuity** — ingest transcripts from past AI sessions, maintain consistency across new ones
- **Debugging Journal** — record what you tried and what worked, never re-debug the same issue
- **Project Onboarding** — ingest design docs, let new team members' AI assistants search project history
- **Personal Knowledge Base** — any markdown notes, searchable by meaning

## License

MIT
