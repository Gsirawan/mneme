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
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type ocSession struct {
	ID       string
	Slug     string
	Title    string
	ParentID sql.NullString
	Updated  int64
}

type ocPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var noisePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)\[search-mode\].*?---\s*\n`),
	regexp.MustCompile(`(?s)\[analyze-mode\].*?---\s*\n`),
	regexp.MustCompile(`(?s)\[SYSTEM DIRECTIVE[^\]]*\].*?(?:\[Status:[^\]]*\])`),
	regexp.MustCompile(`(?s)# Continuation Prompt.*`),
	regexp.MustCompile(`\(sisyphus\)\s*`),
	regexp.MustCompile(`\(prometheus\)\s*`),
	regexp.MustCompile(`\(oracle\)\s*`),
	regexp.MustCompile(`(?s)\[BACKGROUND TASK COMPLETED\].*?\n`),
	regexp.MustCompile(`(?s)\[Agent Usage Reminder\].*?(?:\n\n|\z)`),
	regexp.MustCompile(`(?s)\[Category\+Skill Reminder\].*?(?:\n\n|\z)`),
	regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`),
	regexp.MustCompile(`(?s)\[ALL BACKGROUND TASKS COMPLETE\].*?(?:\n\n|\z)`),
	regexp.MustCompile(`(?s)\[SYSTEM REMINDER[^\]]*\].*?(?:\n\n|\z)`),
}

type textMessage struct {
	Role      string
	Text      string
	Timestamp time.Time
	IsUser    bool
	MessageID string // Phase 2: unique message identifier
	SessionID string // Phase 2: session this message belongs to
}

func openCodeDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func discoverSessions(ocDB *sql.DB) ([]ocSession, error) {
	rows, err := ocDB.Query(`
		SELECT id, slug, title, parent_id, time_updated 
		FROM session 
		WHERE parent_id IS NULL 
		ORDER BY time_updated DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ocSession
	for rows.Next() {
		var s ocSession
		if err := rows.Scan(&s.ID, &s.Slug, &s.Title, &s.ParentID, &s.Updated); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}

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
		updated := time.UnixMilli(s.Updated).Format("Jan 02, 2006 15:04")
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

func getExistingMessageIDs(ocDB *sql.DB, sessionID string) (map[string]bool, error) {
	rows, err := ocDB.Query(`SELECT id FROM message WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids[id] = true
	}
	return ids, nil
}

func readTextFromDB(ocDB *sql.DB, sessionID, msgID, userAlias, assistantAlias string) (*textMessage, error) {
	var data string
	var timeCreated int64
	err := ocDB.QueryRow(`
		SELECT data, time_created FROM message WHERE id = ? AND session_id = ?
	`, msgID, sessionID).Scan(&data, &timeCreated)
	if err != nil {
		return nil, err
	}

	var msgData struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal([]byte(data), &msgData); err != nil {
		return nil, err
	}

	rows, err := ocDB.Query(`
		SELECT data FROM part 
		WHERE message_id = ? AND session_id = ?
		ORDER BY time_created
	`, msgID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var texts []string
	for rows.Next() {
		var partData string
		if err := rows.Scan(&partData); err != nil {
			continue
		}
		var part ocPart
		if err := json.Unmarshal([]byte(partData), &part); err != nil {
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

	isUser := msgData.Role != "assistant"
	role := userAlias
	if !isUser {
		role = assistantAlias
	}

	return &textMessage{
		Role:      role,
		Text:      cleaned,
		Timestamp: time.UnixMilli(timeCreated),
		IsUser:    isUser,
		MessageID: msgID,
		SessionID: sessionID,
	}, nil
}

func getNewMessages(ocDB *sql.DB, sessionID string, done map[string]bool) ([]string, error) {
	rows, err := ocDB.Query(`
		SELECT id FROM message 
		WHERE session_id = ? 
		ORDER BY time_created
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newMsgs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if !done[id] {
			newMsgs = append(newMsgs, id)
		}
	}
	return newMsgs, nil
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

type preparedChunk struct {
	chunk      ChunkData
	validAt    sql.NullString
	serialized []byte
}

func ingestBatch(db *sql.DB, ollama *OllamaClient, sourceFile string, messages []textMessage, sessionTitle string) error {
	// Phase 2: Store individual messages with embeddings for direct search
	if inserted, err := insertMessages(db, ollama, messages); err != nil {
		log.Printf("Warning: message insert failed: %v", err)
	} else if inserted > 0 {
		fmt.Println(renderPreflightStep("ok", fmt.Sprintf("Stored %d messages", inserted)))
	}

	md := buildWatchMarkdown(messages, sessionTitle)
	sections := ParseMarkdown(md)
	if len(sections) == 0 {
		return nil
	}

	ctx := context.Background()
	ingestedAt := time.Now().UTC().Format(time.RFC3339)

	// Phase 1: embed everything BEFORE touching the DB — safe to fail here
	var prepared []preparedChunk
	for _, section := range sections {
		if strings.TrimSpace(section.Content) == "" {
			continue
		}

		var validAtValue sql.NullString
		if section.ValidAt != "" {
			validAtValue = sql.NullString{String: section.ValidAt, Valid: true}
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

			prepared = append(prepared, preparedChunk{
				chunk:      chunk,
				validAt:    validAtValue,
				serialized: serialized,
			})
		}
	}

	if len(prepared) == 0 {
		return nil
	}

	db.Exec(`DELETE FROM vec_chunks WHERE chunk_id IN (SELECT id FROM chunks WHERE source_file = ?)`, sourceFile)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tx.Exec(`DELETE FROM chunks WHERE source_file = ?`, sourceFile)

	chunkIDs := make([]int64, 0, len(prepared))
	for _, pc := range prepared {
		res, err := tx.Exec(
			`INSERT INTO chunks (text, source_file, section_title, header_level, parent_title, section_sequence, chunk_sequence, chunk_total, valid_at, ingested_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pc.chunk.Text, sourceFile, pc.chunk.SectionTitle, pc.chunk.HeaderLevel, pc.chunk.ParentTitle,
			pc.chunk.SectionSequence, pc.chunk.ChunkSequence, pc.chunk.ChunkTotal, pc.validAt, ingestedAt,
		)
		if err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
		chunkID, _ := res.LastInsertId()
		chunkIDs = append(chunkIDs, chunkID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	for i, pc := range prepared {
		if _, err := db.Exec(
			"INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)",
			chunkIDs[i], pc.serialized,
		); err != nil {
			return fmt.Errorf("insert vec: %w", err)
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
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Own process group, survives watcher Ctrl+C
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
	if err := ValidateEmbedDimension(warmupClient); err != nil {
		fmt.Print("\r" + renderPreflightStep("fail", "Warmup  "+err.Error()) + "\n")
		return fmt.Errorf("warmup: %w", err)
	}
	fmt.Print("\r" + renderPreflightStep("ok", fmt.Sprintf("Warmup  model loaded (%d dims)", EmbedDimension)) + "\n")

	return nil
}

func runWatch(args []string, hanaDB, ollamaHost, embedModel, userAlias, assistantAlias string) {
	fs := flag.NewFlagSet("watch-oc", flag.ExitOnError)
	batchSize := fs.Int("batch", 6, "text messages before ingesting")
	pollSec := fs.Int("poll", 3, "poll interval in seconds")

	if err := fs.Parse(args); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	ocDBPath := openCodeDBPath()
	ocDB, err := sql.Open("sqlite3", ocDBPath+"?mode=ro")
	if err != nil {
		log.Fatalf("open opencode db: %v", err)
	}
	defer ocDB.Close()

	sessions, err := discoverSessions(ocDB)
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
	fmt.Println(renderWatchStatus(session.Title, session.ID, *batchSize, *pollSec, hanaDB))
	fmt.Println()

	db, err := InitDB(hanaDB)
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

	done, err = getExistingMessageIDs(ocDB, session.ID)
	if err != nil {
		log.Fatalf("get existing messages: %v", err)
	}
	fmt.Println(infoStyle.Render(fmt.Sprintf("  Skipping %d existing messages. Watching for new...", len(done))))
	fmt.Println()

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
		sourceFile := fmt.Sprintf("watch://%s/batch-%d", session.ID, batchNum)
		if err := ingestBatch(db, ollama, sourceFile, pending, session.Title); err != nil {
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

		newMsgs, err := getNewMessages(ocDB, session.ID, done)
		if err != nil {
			continue
		}

		for _, msgID := range newMsgs {
			tm, err := readTextFromDB(ocDB, session.ID, msgID, userAlias, assistantAlias)
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
			// Normalize text before ingestion
			for i := range pending {
				pending[i].Text = normalizeText(pending[i].Text)
			}

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
