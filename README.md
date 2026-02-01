# Mneme

> *Mneme (NEE-mee) — Greek Muse of memory*

Persistent memory for AI coding sessions. Semantic search over your conversations, architecture decisions, and debugging sessions — so your next AI session picks up where the last one left off.

**100% local. Zero cloud cost. One Go binary. One SQLite file.**

## The Problem

Every AI coding session starts from zero. Your architecture decisions, debugging solutions, "why we chose X over Y" — all lost when the session ends. The next session reinvents the wheel.

Mneme fixes this. Ingest your session transcripts and project notes as markdown. Search them by meaning in your next session. Your AI remembers what you decided, what you tried, and what worked.

## Quick Start

```bash
# Build
git clone https://github.com/Gsirawan/mneme.git
cd mneme
go build -o mneme .

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
- **Live session watcher** — auto-ingest from [OpenCode](https://github.com/sst/opencode) sessions in real-time
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

| Tool | Description |
|------|-------------|
| `mneme_search` | Semantic search — returns relevant chunks chronologically |
| `mneme_ingest` | Ingest a markdown file into memory |
| `mneme_history` | All mentions of an entity over time |
| `mneme_status` | Health check and database stats |

### Live Session Watcher

```bash
./mneme watch
```

Auto-discovers [OpenCode](https://github.com/sst/opencode) sessions, presents a picker, then polls for new messages. Every N messages (default: 6) get batched, embedded, and ingested. Includes preflight checks — starts Ollama if needed, pulls the model if missing, warms it into VRAM.

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

| Variable | Default | Description |
|----------|---------|-------------|
| `OLLAMA_HOST` | `localhost:11434` | Ollama server address |
| `MNEME_DB` | `mneme.db` | SQLite database path |
| `EMBED_MODEL` | `qwen3-embedding:0.6b` | Embedding model name |
| `USER_ALIAS` | `User` | Display name for human messages in watcher |
| `ASSISTANT_ALIAS` | `Assistant` | Display name for AI messages in watcher |
| `MNEME_ALIASES` | *(empty)* | Entity aliases for history search |

### Entity Aliases

Configure aliases so searching one name finds all variants:

```bash
# Format: alias1=name1,name2;alias2=name3,name4
MNEME_ALIASES="react=React,ReactJS,react.js;pg=PostgreSQL,Postgres,psql"
```

Now `./mneme history "react"` finds mentions of React, ReactJS, and react.js.

## Commands

| Command | Description |
|---------|-------------|
| `mneme ingest --file <md>` | Parse and ingest markdown (interactive confirmation) |
| `mneme search "<query>"` | Semantic search with debug output |
| `mneme history "<entity>"` | Chronological mentions of an entity |
| `mneme status` | System health, chunk count, date range |
| `mneme serve` | Start MCP stdio server |
| `mneme watch` | Live session watcher with auto-ingestion |

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
├── watch.go         # Live session watcher + preflight
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
