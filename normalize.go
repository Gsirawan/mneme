package main

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/client9/misspell"
)

var normalizer *misspell.Replacer
var customTypos map[string]string
var typosMutex sync.RWMutex

func init() {
	normalizer = misspell.New()
	loadCustomTypos()
}

func getTyposPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "typos.txt"
	}
	return filepath.Join(filepath.Dir(exe), "typos.txt")
}

func loadCustomTypos() {
	typosMutex.Lock()
	defer typosMutex.Unlock()

	customTypos = make(map[string]string)

	typosPath := getTyposPath()
	data, err := os.ReadFile(typosPath)
	if err != nil {
		log.Printf("Warning: Could not load %s: %v", typosPath, err)
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "→")
		if len(parts) != 2 {
			continue
		}

		typo := strings.TrimSpace(parts[0])
		correct := strings.TrimSpace(parts[1])
		if typo != "" && correct != "" {
			customTypos[typo] = correct
		}
	}

	log.Printf("Loaded %d custom typos from %s", len(customTypos), typosPath)
}

func normalizeText(text string) string {
	if text == "" {
		return text
	}

	normalized, _ := normalizer.Replace(text)
	normalized = applyCustomTypos(normalized)

	return normalized
}

func applyCustomTypos(text string) string {
	typosMutex.RLock()
	defer typosMutex.RUnlock()

	result := text
	for typo, correct := range customTypos {
		result = strings.ReplaceAll(result, typo, correct)
		result = strings.ReplaceAll(result, strings.Title(typo), strings.Title(correct))
	}

	return result
}

func findTyposInMessages(messages []textMessage) map[string]string {
	typoMap := make(map[string]string)
	r := misspell.New()

	for _, msg := range messages {
		if !msg.IsUser {
			continue
		}

		words := strings.Fields(msg.Text)
		for _, word := range words {
			clean := strings.Trim(word, ".,;:!?\"'()[]{}")
			if clean == "" || len(clean) < 3 {
				continue
			}

			corrected, _ := r.Replace(clean)
			if corrected != clean && corrected != "" {
				typoMap[clean] = corrected
			}
		}
	}

	return typoMap
}

func updateTyposFile(newTypos map[string]string) error {
	if len(newTypos) == 0 {
		return nil
	}

	typosPath := getTyposPath()

	existing, err := os.ReadFile(typosPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	existingMap := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			existingMap[line] = true
		}
	}

	f, err := os.OpenFile(typosPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	added := 0
	for typo, correct := range newTypos {
		line := "  " + typo + " → " + correct
		if !existingMap[line] {
			if _, err := f.WriteString(line + "\n"); err != nil {
				return err
			}
			added++
		}
	}

	if added > 0 {
		log.Printf("Added %d new typos to %s", added, typosPath)
		loadCustomTypos()
	}

	return nil
}
