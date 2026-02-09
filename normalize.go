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

	if len(customTypos) > 0 {
		log.Printf("Loaded %d custom typos from %s", len(customTypos), typosPath)
	}
}

func normalizeText(text string) string {
	if text == "" {
		return text
	}

	// Apply misspell library (common typos)
	normalized, _ := normalizer.Replace(text)

	// Apply custom typos from typos.txt
	normalized = applyCustomTypos(normalized)

	return normalized
}

func applyCustomTypos(text string) string {
	typosMutex.RLock()
	defer typosMutex.RUnlock()

	result := text
	for typo, correct := range customTypos {
		// Case-insensitive replacement
		result = strings.ReplaceAll(result, typo, correct)
		result = strings.ReplaceAll(result, strings.Title(typo), strings.Title(correct))
		result = strings.ReplaceAll(result, strings.ToUpper(typo), strings.ToUpper(correct))
	}

	return result
}

// findTyposInMessages - not used with manual approach
func findTyposInMessages(messages []textMessage) map[string]string {
	return nil
}

// updateTyposFile - not used with manual approach
func updateTyposFile(newTypos map[string]string) error {
	return nil
}
