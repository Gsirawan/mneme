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
		Description: "Search memories by semantic similarity. Returns raw chunks sorted chronologically.",
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

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(payload)},
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
