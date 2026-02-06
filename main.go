package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Version is set at build time via -ldflags
var Version = "dev"

func main() {
	// Load .env (ignore error if file doesn't exist)
	_ = godotenv.Load()
	loadEmbedDimension()
	loadAliasesFromEnv()

	ollamaHost := os.Getenv("OLLAMA_HOST")
	if ollamaHost == "" {
		ollamaHost = "localhost:11434"
	}
	mnemeDB := os.Getenv("MNEME_DB")
	if mnemeDB == "" {
		mnemeDB = "mneme.db"
	}
	embedModel := os.Getenv("EMBED_MODEL")
	if embedModel == "" {
		embedModel = "qwen3-embedding:0.6b"
	}
	userAlias := os.Getenv("USER_ALIAS")
	if userAlias == "" {
		userAlias = "User"
	}
	assistantAlias := os.Getenv("ASSISTANT_ALIAS")
	if assistantAlias == "" {
		assistantAlias = "Assistant"
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "ingest":
		runIngest(os.Args[2:], mnemeDB, ollamaHost, embedModel)
	case "search":
		runSearch(os.Args[2:], mnemeDB, ollamaHost, embedModel)
	case "history":
		runHistory(os.Args[2:], mnemeDB)
	case "status":
		runStatus(os.Args[2:], mnemeDB, ollamaHost, embedModel)
	case "watch-oc":
		runWatch(os.Args[2:], mnemeDB, ollamaHost, embedModel, userAlias, assistantAlias)
	case "watch-cc":
		runWatchCC(os.Args[2:], mnemeDB, ollamaHost, embedModel, userAlias, assistantAlias)
	case "serve":
		runServe(os.Args[2:], mnemeDB, ollamaHost, embedModel)
	case "version", "-v", "--version":
		fmt.Printf("mneme %s\n", Version)
		os.Exit(0)
	case "help", "-h", "--help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Mneme - Personal memory system

Usage:
  mneme <command> [options]

Commands:
  ingest   Parse and ingest markdown file into vector database
  search   Search for relevant chunks (debug output)
  history  Find all mentions of an entity in chronological order
  status   Show system status and health
  serve    Start MCP server
  watch-oc Watch live OpenCode session and auto-ingest into Mneme
  watch-cc Watch live Claude Code session and auto-ingest into Mneme
  help     Show this help message

Examples:
  mneme ingest --file notes.md --valid-at 2025-01-31
  mneme search --as-of 2025-12-31 "key topic"
  mneme history --limit 20 "person name"
  mneme status
`)
}

func runIngest(args []string, mnemeDB, ollamaHost, embedModel string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	file := fs.String("file", "", "path to markdown file (required)")
	validAt := fs.String("valid-at", "", "optional date for valid_at field (YYYY-MM-DD)")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	if *file == "" {
		fmt.Fprintf(os.Stderr, "Error: --file is required\n")
		os.Exit(1)
	}

	// Read and parse markdown
	data, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read file: %v", err)
	}

	sections := ParseMarkdown(string(data))

	// Show sections found
	fmt.Printf("Sections found in %s:\n", *file)
	for _, section := range sections {
		wordCount := len(strings.Fields(section.Content))
		headerStr := strings.Repeat("#", section.HeaderLevel)
		marker := ""
		if wordCount > 600 {
			marker = " [will be sub-chunked]"
		}
		fmt.Printf("  %d. [%s] \"%s\" (%d words)%s\n",
			section.Sequence, headerStr, section.Title, wordCount, marker)
	}

	// Ask for confirmation
	fmt.Print("\nProceed? [y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("read input: %v", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Cancelled.")
		os.Exit(0)
	}

	// Initialize DB and Ollama
	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	// Ingest
	result, err := IngestFile(db, ollama, *file, *validAt)
	if err != nil {
		log.Fatalf("ingest file: %v", err)
	}

	// Print result summary
	fmt.Printf("\nIngest complete:\n")
	fmt.Printf("  Sections: %d\n", result.SectionsFound)
	fmt.Printf("  Chunks: %d\n", result.ChunksCreated)
	fmt.Printf("  Sub-chunks: %d\n", result.SubChunksCreated)
}

func runSearch(args []string, mnemeDB, ollamaHost, embedModel string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	asOf := fs.String("as-of", "", "optional date filter (YYYY-MM-DD)")
	limit := fs.Int("limit", 10, "max chunks to retrieve")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: question required as first positional argument\n")
		os.Exit(1)
	}

	question := fs.Arg(0)

	// Initialize DB and Ollama
	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	// Search
	results, err := Search(db, ollama, question, *limit, *asOf)
	if err != nil {
		log.Fatalf("search: %v", err)
	}

	// Print raw chunks (debug output)
	for _, result := range results {
		validAtLabel := result.ValidAt
		if validAtLabel == "" {
			validAtLabel = "timeless"
		}

		fmt.Printf("[%.4f] [%s] %s — %s\n",
			result.Distance, validAtLabel, result.SourceFile, result.SectionTitle)

		// First 200 chars
		text := result.Text
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		fmt.Printf("%s\n\n", text)
	}
}

func runHistory(args []string, mnemeDB string) {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max chunks to retrieve")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: entity name required as first positional argument\n")
		os.Exit(1)
	}

	entity := fs.Arg(0)

	// Initialize DB
	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	// History
	results, err := History(db, entity, *limit)
	if err != nil {
		log.Fatalf("history: %v", err)
	}

	// Print chronological chunks
	for _, result := range results {
		validAtLabel := result.ValidAt
		if validAtLabel == "" {
			validAtLabel = "timeless"
		}

		fmt.Printf("[%s] %s — %s\n",
			validAtLabel, result.SourceFile, result.SectionTitle)

		// First 300 chars
		text := result.Text
		if len(text) > 300 {
			text = text[:300] + "..."
		}
		fmt.Printf("%s\n", text)
		fmt.Println("---")
	}
}

func runStatus(args []string, mnemeDB, ollamaHost, embedModel string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	// Initialize DB and Ollama
	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	// Get status
	status := Status(db, ollama, embedModel)

	// Format output
	fmt.Println("Mneme Status")
	fmt.Println("─────────────")

	ollamaStatus := "unhealthy"
	if status.OllamaHealthy {
		ollamaStatus = "healthy"
	}
	fmt.Printf("Ollama:      %s (%s)\n", ollamaStatus, ollamaHost)
	fmt.Printf("Embed Model: %s\n", status.EmbedModel)
	fmt.Printf("sqlite-vec:  %s\n", status.SqliteVecVersion)
	fmt.Printf("Chunks:      %d\n", status.TotalChunks)

	dateRange := "none"
	if status.EarliestValidAt != "" && status.LatestValidAt != "" {
		dateRange = fmt.Sprintf("%s → %s", status.EarliestValidAt, status.LatestValidAt)
	} else if status.EarliestValidAt != "" {
		dateRange = status.EarliestValidAt
	}
	fmt.Printf("Date Range:  %s\n", dateRange)
}

func runServe(args []string, mnemeDB, ollamaHost, embedModel string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	// Initialize DB and Ollama
	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	if err := RunMCPServer(db, ollama, embedModel); err != nil {
		log.Fatalf("run MCP server: %v", err)
	}
}
