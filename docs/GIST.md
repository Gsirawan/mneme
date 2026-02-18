# Gist — Token-Efficient Memory Retrieval

> **Gist** gives you the gist. A lightweight subagent pattern that searches Mneme on behalf of your primary AI session — keeping raw memory chunks out of your expensive model's context window.

---

## What Is Gist?

When your AI calls `mneme_search` directly, the raw memory chunks land in its context window. A thorough search across multiple queries can easily consume 5,000–10,000+ tokens — in your primary model's context, at your primary model's cost.

**Gist is a token shield.**

Instead of your primary model (e.g., Claude Opus, GPT-4o) doing the memory retrieval itself, you spawn a Gist subagent running on a cheaper model (e.g., Claude Sonnet, Haiku). Gist:

1. Runs all the `mneme_search` and `mneme_history` calls
2. Reads the raw chunks (in its cheaper context)
3. Returns a clean, structured summary

Your primary model receives the summary — not the raw chunks. The expensive tokens stay clean.

```
Primary Model (Opus)
    │
    ├── "Search Mneme for the auth module discussion"
    │
    ▼
Gist Subagent (Sonnet)          ← raw chunks land here
    ├── mneme_search("auth module")
    ├── mneme_search("authentication flow")
    ├── mneme_search("JWT login")
    └── returns structured summary
    │
    ▼
Primary Model (Opus)            ← receives clean summary only
    └── continues with synthesized context
```

---

## Why Use Gist?

| Approach | Token Cost | Quality |
|---|---|---|
| Primary model calls `mneme_search` directly | High — raw chunks in expensive context | Good |
| Primary model calls Gist subagent | Low — raw chunks in cheap context | Same |

**When Gist pays off:**
- You're using an expensive primary model (Opus, GPT-4o, etc.)
- Memory searches are frequent (multiple per session)
- Topics may require several search queries to cover fully
- You want to preserve primary model context for actual reasoning

**When to skip Gist and call `mneme_search` directly:**
- You're already on a cheap model
- You need only one quick lookup
- You want to inspect raw chunks yourself

---

## Setup

### 1. Configure Mneme MCP (if not already done)

Add Mneme to your MCP config (`.mcp.json` or `opencode.json`):

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

### 2. Configure Gist as a Subagent (OpenCode)

In your `opencode.json`, add Gist as a named agent:

```json
{
  "model": "anthropic/claude-opus-4-5",
  "agents": {
    "gist": {
      "model": "anthropic/claude-sonnet-4-5",
      "instructions": "You are Gist — a focused memory retrieval agent for Mneme. Your only job: search Mneme memory, read the results carefully, and return a clean structured summary. See agents/gist.md for full instructions."
    }
  }
}
```

For the full agent prompt, copy the contents of [`agents/gist.md`](../agents/gist.md) into the `instructions` field, or reference it from your project's `AGENTS.md`.

### 3. Instruct Your Primary Model to Use Gist

Add to your `CLAUDE.md`, `AGENTS.md`, or system prompt:

```markdown
## Memory: Mneme + Gist

Mneme is persistent memory. Gist is the retrieval agent.

**The rule: delegate memory searches to Gist.**

Instead of calling `mneme_search` directly, use:
```
task(subagent_type="gist", prompt="Search Mneme for [topic]. Return all relevant chunks and a summary.")
```

Only call `mneme_search` directly for quick single-query lookups where token cost doesn't matter.
```

---

## How to Invoke Gist

### Basic lookup

```python
task(
    subagent_type="gist",
    prompt="Search Mneme for our decision to use event sourcing. Why did we choose it over CRUD?"
)
```

### Entity history

```python
task(
    subagent_type="gist",
    prompt="Use mneme_history to get the full timeline of mentions of 'PaymentService'. What changed over time?"
)
```

### Multi-topic research

```python
task(
    subagent_type="gist",
    prompt="Search Mneme for everything related to our authentication system: JWT setup, session management, the security incident in Q3, and any open questions. Run multiple searches and give me a complete picture."
)
```

### Date-filtered search

```python
task(
    subagent_type="gist",
    prompt="Search Mneme for database migration discussions from before 2026-01-15. What was the state of the migration at that point?"
)
```

---

## Writing Good Gist Prompts

Gist is only as good as the prompt you give it. A few principles:

**Be specific about the topic:**
- ❌ `"Search for the thing we discussed last week"`
- ✅ `"Search for the retry logic discussion in the payment service"`

**Ask for multiple angles when the topic is broad:**
- ✅ `"Search for 'PostgreSQL', 'database choice', and 'why not MongoDB' — cover all angles"`

**Tell Gist what you need from the results:**
- ✅ `"I need to know: what was decided, when, and what alternatives were rejected"`

**Ask for gaps explicitly:**
- ✅ `"If you find nothing on X, say so — don't guess"`

---

## Gist Report Format

Gist always returns a structured report:

```
Query received: [what was asked]

Searches performed:
- mneme_search("...") → N results
- mneme_search("...") → N results

Raw findings:
[Full relevant chunks — not truncated]

Summary:
- Key decisions or facts
- Timeline (if dates available)
- Open questions visible in memory
- Contradictions or evolution in thinking

Gaps:
[What was NOT found]
```

Your primary model receives this report and continues reasoning from it.

---

## Gist vs Direct `mneme_search`

| Situation | Use |
|---|---|
| Expensive primary model, frequent searches | Gist |
| Topic needs 3+ search queries to cover | Gist |
| You want clean context in primary model | Gist |
| Quick single lookup, cheap model | Direct `mneme_search` |
| You want to inspect raw chunks yourself | Direct `mneme_search` |
| Debugging what's in Mneme | Direct `mneme_search` |

---

## Agent Prompt

The full Gist agent prompt lives at [`agents/gist.md`](../agents/gist.md). Copy it into your agent configuration or reference it from your project instructions.

---

## Token Savings Example

A typical multi-query memory search:

- 3 × `mneme_search` calls × ~10 chunks each × ~300 tokens/chunk = **~9,000 tokens**
- Gist summary returned to primary model: **~500–800 tokens**

**Savings: ~8,000–8,500 tokens per memory retrieval session** — at your primary model's per-token rate.

At scale (10 memory retrievals per session, 100 sessions/month), this adds up quickly.
