package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Claude Code session from sessions-index.json
type ccSessionEntry struct {
	SessionID    string `json:"sessionId"`
	FullPath     string `json:"fullPath"`
	Summary      string `json:"summary"`
	FirstPrompt  string `json:"firstPrompt"`
	MessageCount int    `json:"messageCount"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	ProjectPath  string `json:"projectPath"`
	IsSidechain  bool   `json:"isSidechain"`
}

type ccSessionsIndex struct {
	Version      int              `json:"version"`
	Entries      []ccSessionEntry `json:"entries"`
	OriginalPath string           `json:"originalPath"`
}

// Claude Code JSONL line
type ccJSONLLine struct {
	Type      string    `json:"type"`
	UUID      string    `json:"uuid"`
	SessionID string    `json:"sessionId"`
	Timestamp string    `json:"timestamp"`
	Message   ccMessage `json:"message"`
}

type ccMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string for user, []interface{} for assistant
}

type ccContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func claudeCodeBasePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

func discoverCCProjects(basePath string) ([]string, error) {
	projectsDir := filepath.Join(basePath, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		indexPath := filepath.Join(projectsDir, entry.Name(), "sessions-index.json")
		if _, err := os.Stat(indexPath); err == nil {
			projects = append(projects, entry.Name())
		}
	}
	return projects, nil
}

func discoverCCSessions(basePath, projectDir string) ([]ccSessionEntry, error) {
	indexPath := filepath.Join(basePath, "projects", projectDir, "sessions-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read sessions index: %w", err)
	}

	var index ccSessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse sessions index: %w", err)
	}

	// Filter out sidechains, sort by modified desc
	var sessions []ccSessionEntry
	for _, s := range index.Entries {
		if !s.IsSidechain && s.MessageCount > 0 {
			sessions = append(sessions, s)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified > sessions[j].Modified
	})

	return sessions, nil
}

func pickCCProject(projects []string) (string, error) {
	if len(projects) == 1 {
		return projects[0], nil
	}

	fmt.Println()
	fmt.Println(renderHeader())
	fmt.Println()
	fmt.Println(promptStyle.Render("  Claude Code Projects:"))
	fmt.Println()

	for i, p := range projects {
		// Convert path-encoded dir name back to readable path
		readable := strings.ReplaceAll(p, "-", "/")
		fmt.Println(renderSessionItem(i+1, readable, "", ""))
	}

	fmt.Println()
	fmt.Print(promptStyle.Render("  Select project [1]: "))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		input = "1"
	}

	var choice int
	if _, err := fmt.Sscanf(input, "%d", &choice); err != nil || choice < 1 || choice > len(projects) {
		return "", fmt.Errorf("invalid choice: %s", input)
	}

	return projects[choice-1], nil
}

func pickCCSession(sessions []ccSessionEntry) (ccSessionEntry, error) {
	fmt.Println()
	fmt.Println(renderHeader())
	fmt.Println()

	limit := 10
	if len(sessions) < limit {
		limit = len(sessions)
	}

	for i, s := range sessions[:limit] {
		title := s.Summary
		if title == "" {
			title = s.FirstPrompt
			if len(title) > 60 {
				title = title[:60] + "..."
			}
		}
		modified := s.Modified
		if t, err := time.Parse(time.RFC3339, s.Modified); err == nil {
			modified = t.Format("Jan 02, 2006 15:04")
		}
		slug := fmt.Sprintf("(%d msgs)", s.MessageCount)
		fmt.Println(renderSessionItem(i+1, title, slug, modified))
	}

	fmt.Println()
	fmt.Print(promptStyle.Render("  Select session [1]: "))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ccSessionEntry{}, fmt.Errorf("read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		input = "1"
	}

	var choice int
	if _, err := fmt.Sscanf(input, "%d", &choice); err != nil || choice < 1 || choice > limit {
		return ccSessionEntry{}, fmt.Errorf("invalid choice: %s", input)
	}

	return sessions[choice-1], nil
}

// readCCJSONL reads the JSONL file and returns all text messages
func readCCJSONL(filePath, userAlias, assistantAlias string) ([]textMessage, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []textMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry ccJSONLLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Only process user and assistant messages
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		ts, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if ts.IsZero() {
			ts, _ = time.Parse(time.RFC3339, entry.Timestamp)
		}

		if entry.Type == "user" {
			// User content is a string
			text := ""
			switch v := entry.Message.Content.(type) {
			case string:
				text = v
			case []interface{}:
				// Sometimes user content is array of blocks
				for _, block := range v {
					if m, ok := block.(map[string]interface{}); ok {
						if m["type"] == "text" {
							if t, ok := m["text"].(string); ok {
								text += t + "\n"
							}
						}
					}
				}
			}

			cleaned := stripNoise(text)
			if len(cleaned) < 3 {
				continue
			}

			messages = append(messages, textMessage{
				Role:      userAlias,
				Text:      cleaned,
				Timestamp: ts,
				IsUser:    true,
			})
		}

		if entry.Type == "assistant" {
			// Assistant content is array of blocks
			blocks, ok := entry.Message.Content.([]interface{})
			if !ok {
				continue
			}

			var texts []string
			for _, block := range blocks {
				m, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				// Only text blocks — skip thinking, tool_use, tool_result
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok && t != "" {
						texts = append(texts, t)
					}
				}
			}

			if len(texts) == 0 {
				continue
			}

			cleaned := stripNoise(strings.Join(texts, "\n"))
			if len(cleaned) < 3 {
				continue
			}

			messages = append(messages, textMessage{
				Role:      assistantAlias,
				Text:      cleaned,
				Timestamp: ts,
				IsUser:    false,
			})
		}
	}

	return messages, scanner.Err()
}

func runWatchCC(args []string, mnemeDB, ollamaHost, embedModel, userAlias, assistantAlias string) {
	fs := flag.NewFlagSet("watch-cc", flag.ExitOnError)
	batchSize := fs.Int("batch", 6, "text messages before ingesting")
	pollSec := fs.Int("poll", 3, "poll interval in seconds")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	basePath := claudeCodeBasePath()

	// Discover projects
	projects, err := discoverCCProjects(basePath)
	if err != nil {
		log.Fatalf("discover projects: %v", err)
	}
	if len(projects) == 0 {
		log.Fatal("no Claude Code projects found")
	}

	projectDir, err := pickCCProject(projects)
	if err != nil {
		log.Fatalf("pick project: %v", err)
	}

	// Discover sessions in project
	sessions, err := discoverCCSessions(basePath, projectDir)
	if err != nil {
		log.Fatalf("discover sessions: %v", err)
	}
	if len(sessions) == 0 {
		log.Fatal("no Claude Code sessions found in project")
	}

	session, err := pickCCSession(sessions)
	if err != nil {
		log.Fatalf("pick session: %v", err)
	}

	fmt.Println()
	if err := watchPreflight(ollamaHost, embedModel); err != nil {
		log.Fatalf("preflight: %v", err)
	}

	fmt.Println()
	title := session.Summary
	if title == "" {
		title = session.FirstPrompt
		if len(title) > 60 {
			title = title[:60] + "..."
		}
	}
	fmt.Println(renderWatchStatus(title, session.SessionID, *batchSize, *pollSec, mnemeDB))
	fmt.Println()

	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	// Cleanup orphaned vec_chunks
	db.Exec(`DELETE FROM vec_chunks WHERE chunk_id NOT IN (SELECT id FROM chunks)`)

	// Find batch number
	batchNum := 0
	watchPrefix := fmt.Sprintf("watch-cc://%s/batch-", session.SessionID)
	var maxBatch sql.NullInt64
	_ = db.QueryRow(
		`SELECT MAX(CAST(REPLACE(source_file, ?, '') AS INTEGER)) FROM chunks WHERE source_file LIKE ?`,
		watchPrefix, watchPrefix+"%",
	).Scan(&maxBatch)
	if maxBatch.Valid {
		batchNum = int(maxBatch.Int64) + 1
	}

	// Read existing messages to know where we left off
	existingMsgs, _ := readCCJSONL(session.FullPath, userAlias, assistantAlias)
	seenCount := len(existingMsgs)
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Skipping %d existing messages. Watching for new...", seenCount)))
	fmt.Println()

	var pending []textMessage

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	ticker := time.NewTicker(time.Duration(*pollSec) * time.Second)
	defer ticker.Stop()

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		fmt.Println()
		fmt.Println(infoStyle.Render(fmt.Sprintf("  Flushing %d pending messages...", len(pending))))
		sourceFile := fmt.Sprintf("watch-cc://%s/batch-%d", session.SessionID, batchNum)
		if err := ingestBatch(db, ollama, sourceFile, pending, title); err != nil {
			fmt.Println(renderPreflightStep("fail", fmt.Sprintf("Flush error: %v", err)))
			return
		}
		batchNum++
		fmt.Println(renderIngest(len(pending), batchNum))
		pending = nil
	}

	for {
		select {
		case <-sigCh:
			flushPending()
			fmt.Println()
			fmt.Println(infoStyle.Render("  Stopped."))
			return
		case <-ticker.C:
		}

		allMsgs, err := readCCJSONL(session.FullPath, userAlias, assistantAlias)
		if err != nil {
			continue
		}

		if len(allMsgs) <= seenCount {
			continue
		}

		newMsgs := allMsgs[seenCount:]
		seenCount = len(allMsgs)

		for _, tm := range newMsgs {
			pending = append(pending, tm)
			fmt.Println(renderMessage(tm.Role, tm.Timestamp.Format("15:04:05"), tm.Text, tm.IsUser))
		}

		if len(pending) >= *batchSize {
			sourceFile := fmt.Sprintf("watch-cc://%s/batch-%d", session.SessionID, batchNum)
			if err := ingestBatch(db, ollama, sourceFile, pending, title); err != nil {
				fmt.Println(renderPreflightStep("fail", fmt.Sprintf("Ingest error: %v", err)))
				continue
			}

			batchNum++
			fmt.Println()
			fmt.Println(renderIngest(len(pending), batchNum))
			fmt.Println()
			pending = nil
		}
	}
}
