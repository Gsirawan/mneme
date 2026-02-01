package main

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type Section struct {
	Title       string
	HeaderLevel int
	ParentTitle string
	Content     string
	Sequence    int
	ValidAt     string
}

type ChunkData struct {
	Text            string
	SourceFile      string
	SectionTitle    string
	HeaderLevel     int
	ParentTitle     string
	SectionSequence int
	ChunkSequence   int
	ChunkTotal      int
	ValidAt         string
}

type IngestResult struct {
	SectionsFound    int
	ChunksCreated    int
	SubChunksCreated int
	DeletedChunks    int64
}

func ExtractDateFromHeader(header string) string {
	pattern := `\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+([0-9]{1,2}),\s*([0-9]{4})\b`
	re := regexp.MustCompile(pattern)
	match := re.FindString(header)
	if match == "" {
		return ""
	}
	parsed, err := time.Parse("January 2, 2006", match)
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01-02")
}

func ParseMarkdown(content string) []Section {
	lines := strings.Split(content, "\n")
	sections := []Section{}
	seq := 1
	seenHeader := false

	preambleLines := []string{}

	currentH2Title := ""
	currentH2Content := []string{}
	currentH2HasH3 := false
	currentH2ValidAt := ""

	currentH3Title := ""
	currentH3Content := []string{}
	currentH3ValidAt := ""
	inH3 := false

	addSection := func(title string, headerLevel int, parentTitle string, sectionContent string, validAt string) {
		sections = append(sections, Section{
			Title:       title,
			HeaderLevel: headerLevel,
			ParentTitle: parentTitle,
			Content:     sectionContent,
			Sequence:    seq,
			ValidAt:     validAt,
		})
		seq++
	}

	flushPreamble := func() {
		if len(preambleLines) == 0 {
			return
		}
		content := strings.TrimSpace(strings.Join(preambleLines, "\n"))
		if content != "" {
			addSection("Preamble", 2, "", content, "")
		}
		preambleLines = nil
	}

	flushH3 := func() {
		if currentH3Title == "" {
			return
		}
		content := strings.TrimSpace(strings.Join(currentH3Content, "\n"))
		addSection(currentH3Title, 3, currentH2Title, content, currentH3ValidAt)
		currentH3Title = ""
		currentH3Content = nil
		currentH3ValidAt = ""
		inH3 = false
	}

	flushH2 := func() {
		if currentH2Title == "" {
			return
		}
		if !currentH2HasH3 {
			content := strings.TrimSpace(strings.Join(currentH2Content, "\n"))
			addSection(currentH2Title, 2, "", content, currentH2ValidAt)
		}
		currentH2Title = ""
		currentH2Content = nil
		currentH2HasH3 = false
		currentH2ValidAt = ""
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "### ") {
			if !seenHeader {
				seenHeader = true
				flushPreamble()
			}
			flushH3()
			if currentH2Title != "" && !currentH2HasH3 {
				preamble := strings.TrimSpace(strings.Join(currentH2Content, "\n"))
				if preamble != "" {
					addSection(currentH2Title, 2, "", preamble, currentH2ValidAt)
				}
				currentH2Content = nil
			}
			currentH2HasH3 = true
			inH3 = true
			currentH3Title = strings.TrimSpace(line[4:])
			currentH3ValidAt = ExtractDateFromHeader(currentH3Title)
			if currentH3ValidAt == "" {
				currentH3ValidAt = currentH2ValidAt
			}
			currentH3Content = nil
			continue
		}

		if strings.HasPrefix(line, "## ") {
			if !seenHeader {
				seenHeader = true
				flushPreamble()
			}
			flushH3()
			flushH2()
			currentH2Title = strings.TrimSpace(line[3:])
			currentH2Content = nil
			currentH2HasH3 = false
			currentH2ValidAt = ExtractDateFromHeader(currentH2Title)
			inH3 = false
			continue
		}

		if inH3 {
			currentH3Content = append(currentH3Content, line)
		} else if currentH2Title != "" {
			currentH2Content = append(currentH2Content, line)
		} else {
			preambleLines = append(preambleLines, line)
		}
	}

	flushH3()
	flushH2()
	if !seenHeader {
		flushPreamble()
	}

	return sections
}

func ChunkSection(section Section, maxWords int) []ChunkData {
	wordCount := len(strings.Fields(section.Content))
	if wordCount <= maxWords {
		return []ChunkData{
			{
				Text:            strings.TrimSpace(section.Content),
				SectionTitle:    section.Title,
				HeaderLevel:     section.HeaderLevel,
				ParentTitle:     section.ParentTitle,
				SectionSequence: section.Sequence,
				ChunkSequence:   1,
				ChunkTotal:      1,
				ValidAt:         section.ValidAt,
			},
		}
	}

	paragraphs := strings.Split(section.Content, "\n\n")
	chunkTexts := []string{}
	currentParts := []string{}
	currentWords := 0

	countWords := func(text string) int {
		return len(strings.Fields(text))
	}

	flushChunk := func() {
		if len(currentParts) == 0 {
			return
		}
		chunkTexts = append(chunkTexts, strings.Join(currentParts, "\n\n"))
		currentParts = nil
		currentWords = 0
	}

	for _, paragraph := range paragraphs {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed == "" {
			continue
		}
		paraWords := countWords(trimmed)
		if currentWords == 0 && paraWords > maxWords {
			chunkTexts = append(chunkTexts, trimmed)
			continue
		}
		if currentWords+paraWords > maxWords {
			flushChunk()
		}
		currentParts = append(currentParts, trimmed)
		currentWords += paraWords
	}

	flushChunk()

	chunks := make([]ChunkData, 0, len(chunkTexts))
	for idx, text := range chunkTexts {
		chunks = append(chunks, ChunkData{
			Text:            text,
			SectionTitle:    section.Title,
			HeaderLevel:     section.HeaderLevel,
			ParentTitle:     section.ParentTitle,
			SectionSequence: section.Sequence,
			ChunkSequence:   idx + 1,
			ChunkTotal:      len(chunkTexts),
			ValidAt:         section.ValidAt,
		})
	}

	return chunks
}

func IngestFile(db *sql.DB, ollama *OllamaClient, filePath string, validAt string) (IngestResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return IngestResult{}, err
	}

	sections := ParseMarkdown(string(data))
	result := IngestResult{SectionsFound: len(sections)}

	ctx := context.Background()
	ingestedAt := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return IngestResult{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(
		"DELETE FROM vec_chunks WHERE chunk_id IN (SELECT id FROM chunks WHERE source_file = ?)",
		filePath,
	); err != nil {
		return IngestResult{}, err
	}
	delResult, err := tx.Exec("DELETE FROM chunks WHERE source_file = ?", filePath)
	if err != nil {
		return IngestResult{}, err
	}
	deletedCount, _ := delResult.RowsAffected()
	result.DeletedChunks = deletedCount

	for _, section := range sections {
		sectionValidAt := section.ValidAt
		if sectionValidAt == "" {
			sectionValidAt = validAt
		}
		var validAtValue sql.NullString
		if sectionValidAt != "" {
			validAtValue = sql.NullString{String: sectionValidAt, Valid: true}
		}

		chunks := ChunkSection(section, 600)
		result.ChunksCreated += len(chunks)
		if len(chunks) > 1 {
			result.SubChunksCreated += len(chunks) - 1
		}
		for _, chunk := range chunks {
			chunk.SourceFile = filePath
			chunk.ValidAt = sectionValidAt

			if strings.TrimSpace(chunk.Text) == "" {
				result.ChunksCreated--
				continue
			}

			embedding, err := ollama.Embed(ctx, chunk.Text)
			if err != nil {
				return IngestResult{}, err
			}
			serialized, err := sqlite_vec.SerializeFloat32(embedding)
			if err != nil {
				return IngestResult{}, err
			}

			res, err := tx.Exec(
				`INSERT INTO chunks (text, source_file, section_title, header_level, parent_title, section_sequence, chunk_sequence, chunk_total, valid_at, ingested_at)
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				chunk.Text,
				chunk.SourceFile,
				chunk.SectionTitle,
				chunk.HeaderLevel,
				chunk.ParentTitle,
				chunk.SectionSequence,
				chunk.ChunkSequence,
				chunk.ChunkTotal,
				validAtValue,
				ingestedAt,
			)
			if err != nil {
				return IngestResult{}, err
			}

			chunkID, err := res.LastInsertId()
			if err != nil {
				return IngestResult{}, err
			}

			if _, err := tx.Exec(
				"INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)",
				chunkID,
				serialized,
			); err != nil {
				return IngestResult{}, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return IngestResult{}, err
	}

	return result, nil
}
