package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RunMCPServer(db *sql.DB, ollama *OllamaClient, embedModel string) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "mneme",
		Version: "1.0.0",
	}, nil)

	server.AddTool(&mcp.Tool{
		Name:        "mneme_search",
		Description: "Search memories by semantic similarity. Returns raw chunks sorted chronologically. IMPORTANT: When you find a relevant chunk, do NOT skim it. Use the Read tool to open the SourceFile and read the full section (matching SectionTitle) and its sub-sections before responding. The chunk is a pointer — the full context lives in the file.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Search query"},
				"as_of": {"type": "string", "description": "Optional ISO date filter"},
			"limit": {"type": "integer", "description": "Maximum results (default 10)"}
			},
			"required": ["query"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := argsOrEmpty(req)
		if err != nil {
			return nil, err
		}
		query, err := requiredStringArg(args, "query")
		if err != nil {
			return nil, err
		}
		asOf, err := optionalStringArg(args, "as_of")
		if err != nil {
			return nil, err
		}
		limit, ok, err := optionalIntArg(args, "limit")
		if err != nil {
			return nil, err
		}
		if !ok || limit <= 0 {
			limit = 10
		}

		results, err := Search(db, ollama, query, limit, asOf)
		if err != nil {
			return nil, err
		}

		payload, err := json.Marshal(results)
		if err != nil {
			return nil, err
		}

		whisper := "\n\n---\n⚡ Before responding: if any chunk above is relevant, READ the full section in its SourceFile (use Read tool with the file path). The chunk is a summary — the real context, nuance, and sub-sections live in the original file. Don't skim. Don't guess. Read it."

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload) + whisper},
			},
		}, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "mneme_ingest",
		Description: "Ingest a markdown file into the memory store.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Path to markdown file"},
				"valid_at": {"type": "string", "description": "Optional ISO date for valid_at"}
			},
			"required": ["file_path"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := argsOrEmpty(req)
		if err != nil {
			return nil, err
		}
		filePath, err := requiredStringArg(args, "file_path")
		if err != nil {
			return nil, err
		}
		if err := validateIngestPath(filePath); err != nil {
			return nil, err
		}
		validAt, err := optionalStringArg(args, "valid_at")
		if err != nil {
			return nil, err
		}

		result, err := IngestFile(db, ollama, filePath, validAt)
		if err != nil {
			return nil, err
		}

		payload, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "mneme_history",
		Description: "Fetch chronological history for an entity.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"entity": {"type": "string", "description": "Entity name"},
			"limit": {"type": "integer", "description": "Maximum results (default 20)"}
			},
			"required": ["entity"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := argsOrEmpty(req)
		if err != nil {
			return nil, err
		}
		entity, err := requiredStringArg(args, "entity")
		if err != nil {
			return nil, err
		}
		limit, ok, err := optionalIntArg(args, "limit")
		if err != nil {
			return nil, err
		}
		if !ok || limit <= 0 {
			limit = 20
		}

		results, err := History(db, entity, limit)
		if err != nil {
			return nil, err
		}

		payload, err := json.Marshal(results)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "mneme_search_msg",
		Description: "Search messages directly with context window. Returns conversation snippets around matching messages. Use for finding specific discussions or phrases.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Search query"},
				"fts": {"type": "boolean", "description": "Use exact phrase matching (FTS5/LIKE) instead of semantic search"},
				"context": {"type": "integer", "description": "Context window in minutes (default 3)"},
				"limit": {"type": "integer", "description": "Maximum results (default 5)"}
			},
			"required": ["query"]
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := argsOrEmpty(req)
		if err != nil {
			return nil, err
		}
		query, err := requiredStringArg(args, "query")
		if err != nil {
			return nil, err
		}
		useFTS, _, _ := optionalBoolArg(args, "fts")
		contextMins, ok, _ := optionalIntArg(args, "context")
		if !ok || contextMins <= 0 {
			contextMins = 3
		}
		limit, ok, _ := optionalIntArg(args, "limit")
		if !ok || limit <= 0 {
			limit = 5
		}

		if useFTS {
			results, err := searchMessagesFTS(db, query, limit)
			if err != nil {
				return nil, err
			}
			payload, err := json.Marshal(results)
			if err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: string(payload)},
				},
			}, nil
		}

		// Semantic search with context
		contexts, err := searchMessagesWithContext(db, ollama, query, limit, contextMins)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(contexts)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil
	})

	server.AddTool(&mcp.Tool{
		Name:        "mneme_status",
		Description: "Get system status and health details.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status := Status(db, ollama, embedModel)

		payload, err := json.Marshal(status)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
			},
		}, nil
	})

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func validateIngestPath(filePath string) error {
	cleaned := filepath.Clean(filePath)
	if filepath.IsAbs(cleaned) {
		root := os.Getenv("MNEME_INGEST_ROOT")
		if root == "" {
			return fmt.Errorf("absolute paths require MNEME_INGEST_ROOT to be set")
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("invalid MNEME_INGEST_ROOT: %w", err)
		}
		if !strings.HasPrefix(cleaned, absRoot+string(filepath.Separator)) && cleaned != absRoot {
			return fmt.Errorf("path %q is outside allowed root %q", cleaned, absRoot)
		}
	} else if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path %q contains directory traversal", filePath)
	}
	return nil
}

func argsOrEmpty(req *mcp.CallToolRequest) (map[string]any, error) {
	if req == nil || len(req.Params.Arguments) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return nil, err
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string", key)
	}
	return str, nil
}

func optionalStringArg(args map[string]any, key string) (string, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return "", nil
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string", key)
	}
	return str, nil
}

func optionalBoolArg(args map[string]any, key string) (bool, bool, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return false, false, nil
	}
	b, ok := value.(bool)
	if !ok {
		return false, true, fmt.Errorf("argument %s must be a boolean", key)
	}
	return b, true, nil
}

func optionalIntArg(args map[string]any, key string) (int, bool, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	switch typed := value.(type) {
	case float64:
		if typed != math.Trunc(typed) {
			return 0, true, fmt.Errorf("argument %s must be an integer", key)
		}
		return int(typed), true, nil
	case int:
		return typed, true, nil
	case int32:
		return int(typed), true, nil
	case int64:
		return int(typed), true, nil
	default:
		return 0, true, fmt.Errorf("argument %s must be an integer", key)
	}
}
