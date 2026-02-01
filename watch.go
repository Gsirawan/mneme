package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type ocSession struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	ParentID  string `json:"parentID"`
	Directory string `json:"directory"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type ocMessage struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
}

type ocPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var noisePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)\[search-mode\].*?---\s*\n`),
	regexp.MustCompile(`(?s)\[SYSTEM DIRECTIVE[^\]]*\].*?(?:\[Status:[^\]]*\])`),
	regexp.MustCompile(`(?s)# Continuation Prompt.*`),
	regexp.MustCompile(`\(sisyphus\)\s*`),
	regexp.MustCompile(`\(prometheus\)\s*`),
	regexp.MustCompile(`\(oracle\)\s*`),
	regexp.MustCompile(`(?s)\[BACKGROUND TASK COMPLETED\].*?\n`),
	regexp.MustCompile(`(?s)\[Agent Usage Reminder\].*?(?:\n\n|\z)`),
	regexp.MustCompile(`(?s)\[Category\+Skill Reminder\].*?(?:\n\n|\z)`),
}

type textMessage struct {
	Role      string
	Text      string
	Timestamp time.Time
	IsUser    bool
}

func openCodeStoragePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "storage")
}

func discoverSessions(storagePath string) ([]ocSession, error) {
	sessionDir := filepath.Join(storagePath, "session", "global")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("read session dir: %w", err)
	}

	var sessions []ocSession
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionDir, entry.Name()))
		if err != nil {
			continue
		}
		var s ocSession
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.ParentID == "" {
			sessions = append(sessions, s)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Time.Updated > sessions[j].Time.Updated
	})

	return sessions, nil
}

func pickSession(sessions []ocSession) (ocSession, error) {
	fmt.Println()
	fmt.Println(renderHeader())
	fmt.Println()

	limit := 10
	if len(sessions) < limit {
		limit = len(sessions)
	}

	for i, s := range sessions[:limit] {
		updated := time.UnixMilli(s.Time.Updated).Format("Jan 02, 2006 15:04")
		slug := s.Slug
		if slug == "" {
			slug = "(no slug)"
		}
		fmt.Println(renderSessionItem(i+1, s.Title, slug, updated))
	}

	fmt.Println()
	fmt.Print(promptStyle.Render("  Select session [1]: "))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ocSession{}, fmt.Errorf("read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		input = "1"
	}

	var choice int
	if _, err := fmt.Sscanf(input, "%d", &choice); err != nil || choice < 1 || choice > limit {
		return ocSession{}, fmt.Errorf("invalid choice: %s", input)
	}

	return sessions[choice-1], nil
}

func stripNoise(text string) string {
	for _, p := range noisePatterns {
		text = p.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

func readTextFromMessage(storagePath, sessionID, msgID, userAlias, assistantAlias string) (*textMessage, error) {
	msgPath := filepath.Join(storagePath, "message", sessionID, msgID+".json")
	data, err := os.ReadFile(msgPath)
	if err != nil {
		return nil, err
	}
	var msg ocMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	partDir := filepath.Join(storagePath, "part", msgID)
	entries, err := os.ReadDir(partDir)
	if err != nil {
		return nil, err
	}

	var texts []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		pdata, err := os.ReadFile(filepath.Join(partDir, entry.Name()))
		if err != nil {
			continue
		}
		var part ocPart
		if err := json.Unmarshal(pdata, &part); err != nil {
			continue
		}
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}

	if len(texts) == 0 {
		return nil, nil
	}

	cleaned := stripNoise(strings.Join(texts, "\n"))
	if len(cleaned) < 3 {
		return nil, nil
	}

	isUser := msg.Role != "assistant"
	role := userAlias
	if !isUser {
		role = assistantAlias
	}

	return &textMessage{
		Role:      role,
		Text:      cleaned,
		Timestamp: time.UnixMilli(msg.Time.Created),
		IsUser:    isUser,
	}, nil
}

func buildWatchMarkdown(messages []textMessage, sessionTitle string) string {
	if len(messages) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s\n\n", sessionTitle))

	date := messages[0].Timestamp.Format("January 2, 2006")
	b.WriteString(fmt.Sprintf("## %s\n\n", date))

	for _, m := range messages {
		msgDate := m.Timestamp.Format("January 2, 2006")
		if msgDate != date {
			date = msgDate
			b.WriteString(fmt.Sprintf("\n## %s\n\n", date))
		}
		b.WriteString(fmt.Sprintf("**%s** [%s]:\n%s\n\n", m.Role, m.Timestamp.Format("15:04"), m.Text))
	}

	return b.String()
}

func ingestBatch(db *sql.DB, ollama *OllamaClient, sourceFile string, messages []textMessage, sessionTitle string) error {
	md := buildWatchMarkdown(messages, sessionTitle)
	sections := ParseMarkdown(md)
	if len(sections) == 0 {
		return nil
	}

	ctx := context.Background()
	ingestedAt := time.Now().UTC().Format(time.RFC3339)

	db.Exec(`DELETE FROM vec_chunks WHERE chunk_id IN (SELECT id FROM chunks WHERE source_file = ?)`, sourceFile)
	db.Exec(`DELETE FROM chunks WHERE source_file = ?`, sourceFile)

	for _, section := range sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}

		sectionValidAt := section.ValidAt
		var validAtValue sql.NullString
		if sectionValidAt != "" {
			validAtValue = sql.NullString{String: sectionValidAt, Valid: true}
		}

		chunks := ChunkSection(section, 600)
		for _, chunk := range chunks {
			if strings.TrimSpace(chunk.Text) == "" {
				continue
			}

			embedding, err := ollama.Embed(ctx, chunk.Text)
			if err != nil {
				return fmt.Errorf("embed: %w", err)
			}
			serialized, err := sqlite_vec.SerializeFloat32(embedding)
			if err != nil {
				return fmt.Errorf("serialize: %w", err)
			}

			res, err := db.Exec(
				`INSERT INTO chunks (text, source_file, section_title, header_level, parent_title, section_sequence, chunk_sequence, chunk_total, valid_at, ingested_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				chunk.Text, sourceFile, chunk.SectionTitle, chunk.HeaderLevel, chunk.ParentTitle,
				chunk.SectionSequence, chunk.ChunkSequence, chunk.ChunkTotal, validAtValue, ingestedAt,
			)
			if err != nil {
				return fmt.Errorf("insert chunk: %w", err)
			}

			chunkID, _ := res.LastInsertId()
			if _, err := db.Exec(
				"INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)",
				chunkID, serialized,
			); err != nil {
				return fmt.Errorf("insert vec: %w", err)
			}
		}
	}

	return nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

func watchPreflight(ollamaHost, embedModel string) error {
	ctx := context.Background()
	baseURL := "http://" + ollamaHost
	client := &OllamaClient{
		baseURL:    baseURL,
		embedModel: embedModel,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	fmt.Print(renderPreflightStep("wait", "Ollama"))
	if !client.IsHealthy(ctx) {
		fmt.Print("\r" + renderPreflightStep("wait", "Ollama  starting...") + "\n")
		cmd := exec.Command("ollama", "serve")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			fmt.Print("\r" + renderPreflightStep("fail", "Ollama  could not start") + "\n")
			return fmt.Errorf("start ollama: %w", err)
		}
		go func() { _ = cmd.Wait() }()

		deadline := time.Now().Add(15 * time.Second)
		started := false
		for time.Now().Before(deadline) {
			if client.IsHealthy(ctx) {
				started = true
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if !started {
			fmt.Print("\r" + renderPreflightStep("fail", "Ollama  timeout") + "\n")
			return fmt.Errorf("ollama did not start within 15s")
		}
		fmt.Print("\r" + renderPreflightStep("ok", "Ollama  started") + "\n")
	} else {
		fmt.Print("\r" + renderPreflightStep("ok", "Ollama  running") + "\n")
	}

	fmt.Print(renderPreflightStep("wait", "Model   "+embedModel))
	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/api/tags", nil)
	resp, err := httpClient.Do(req)
	modelFound := false
	if err == nil {
		var tags tagsResponse
		if json.NewDecoder(resp.Body).Decode(&tags) == nil {
			for _, m := range tags.Models {
				if m.Name == embedModel {
					modelFound = true
					break
				}
			}
		}
		resp.Body.Close()
	}

	if !modelFound {
		fmt.Print("\r" + renderPreflightStep("wait", "Model   pulling "+embedModel+"...") + "\n")
		cmd := exec.Command("ollama", "pull", embedModel)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Print("\r" + renderPreflightStep("fail", "Model   pull failed") + "\n")
			return fmt.Errorf("pull model: %w", err)
		}
		fmt.Print("\r" + renderPreflightStep("ok", "Model   "+embedModel+" pulled") + "\n")
	} else {
		fmt.Print("\r" + renderPreflightStep("ok", "Model   "+embedModel) + "\n")
	}

	fmt.Print(renderPreflightStep("wait", "Warmup  loading into VRAM"))
	warmupClient := NewOllamaClient(baseURL, embedModel)
	if _, err := warmupClient.Embed(ctx, "warmup"); err != nil {
		fmt.Print("\r" + renderPreflightStep("fail", "Warmup  embed failed") + "\n")
		return fmt.Errorf("warmup: %w", err)
	}
	fmt.Print("\r" + renderPreflightStep("ok", "Warmup  model loaded") + "\n")

	return nil
}

func runWatch(args []string, mnemeDB, ollamaHost, embedModel, userAlias, assistantAlias string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	batchSize := fs.Int("batch", 6, "text messages before ingesting")
	pollSec := fs.Int("poll", 3, "poll interval in seconds")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	loadAliasesFromEnv()
	storagePath := openCodeStoragePath()

	sessions, err := discoverSessions(storagePath)
	if err != nil {
		log.Fatalf("discover sessions: %v", err)
	}
	if len(sessions) == 0 {
		log.Fatal("no OpenCode sessions found")
	}

	session, err := pickSession(sessions)
	if err != nil {
		log.Fatalf("pick session: %v", err)
	}

	fmt.Println()
	if err := watchPreflight(ollamaHost, embedModel); err != nil {
		log.Fatalf("preflight: %v", err)
	}

	fmt.Println()
	fmt.Println(renderWatchStatus(session.Title, session.ID, *batchSize, *pollSec, mnemeDB))
	fmt.Println()

	db, err := InitDB(mnemeDB)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer db.Close()

	ollama := NewOllamaClient("http://"+ollamaHost, embedModel)

	db.Exec(`DELETE FROM vec_chunks WHERE chunk_id NOT IN (SELECT id FROM chunks)`)

	done := make(map[string]bool)
	retry := make(map[string]int)
	var pending []textMessage

	batchNum := 0
	watchPrefix := fmt.Sprintf("watch://%s/batch-", session.ID)
	var maxBatch sql.NullInt64
	_ = db.QueryRow(
		`SELECT MAX(CAST(REPLACE(source_file, ?, '') AS INTEGER)) FROM chunks WHERE source_file LIKE ?`,
		watchPrefix, watchPrefix+"%",
	).Scan(&maxBatch)
	if maxBatch.Valid {
		batchNum = int(maxBatch.Int64) + 1
	}

	msgDir := filepath.Join(storagePath, "message", session.ID)
	if entries, err := os.ReadDir(msgDir); err == nil {
		for _, e := range entries {
			done[strings.TrimSuffix(e.Name(), ".json")] = true
		}
	}
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Skipping %d existing messages. Watching for new...", len(done))))
	fmt.Println()

	ticker := time.NewTicker(time.Duration(*pollSec) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		entries, err := os.ReadDir(msgDir)
		if err != nil {
			continue
		}

		var toProcess []string
		for _, e := range entries {
			msgID := strings.TrimSuffix(e.Name(), ".json")
			if done[msgID] {
				continue
			}
			toProcess = append(toProcess, msgID)
		}

		for _, msgID := range toProcess {
			tm, err := readTextFromMessage(storagePath, session.ID, msgID, userAlias, assistantAlias)
			if err != nil || tm == nil {
				retry[msgID]++
				if retry[msgID] > 60 {
					done[msgID] = true
					delete(retry, msgID)
				}
				continue
			}

			done[msgID] = true
			delete(retry, msgID)
			pending = append(pending, *tm)

			fmt.Println(renderMessage(tm.Role, tm.Timestamp.Format("15:04:05"), tm.Text, tm.IsUser))
		}

		if len(pending) >= *batchSize {
			sourceFile := fmt.Sprintf("watch://%s/batch-%d", session.ID, batchNum)
			if err := ingestBatch(db, ollama, sourceFile, pending, session.Title); err != nil {
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
