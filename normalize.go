package main

import (
	"bufio"
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sajari/fuzzy"
)

//go:embed words.txt
var embeddedWords string

var spellModel *fuzzy.Model
var customTypos map[string]string
var typosMutex sync.RWMutex

func init() {
	initSpellModel()
	loadCustomTypos()
}

func initSpellModel() {
	spellModel = fuzzy.NewModel()
	spellModel.SetThreshold(1)
	spellModel.SetDepth(2)

	// Train with embedded dictionary
	words := strings.Split(embeddedWords, "\n")
	var cleaned []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" {
			cleaned = append(cleaned, w)
		}
	}
	spellModel.Train(cleaned)
	log.Printf("Spell model trained with %d words", len(cleaned))
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

	// Apply custom typos first
	normalized := applyCustomTypos(text)

	// Then apply spell correction word by word
	words := strings.Fields(normalized)
	for i, word := range words {
		// Preserve punctuation
		prefix, core, suffix := splitPunctuation(word)
		if len(core) >= 3 {
			corrected := spellModel.SpellCheck(strings.ToLower(core))
			if corrected != "" && corrected != strings.ToLower(core) {
				// Preserve original case if it was capitalized
				if len(core) > 0 && core[0] >= 'A' && core[0] <= 'Z' {
					corrected = strings.Title(corrected)
				}
				words[i] = prefix + corrected + suffix
			}
		}
	}

	return strings.Join(words, " ")
}

func splitPunctuation(word string) (prefix, core, suffix string) {
	start := 0
	end := len(word)

	for start < end && !isAlpha(word[start]) {
		start++
	}
	for end > start && !isAlpha(word[end-1]) {
		end--
	}

	return word[:start], word[start:end], word[end:]
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
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

	for _, msg := range messages {
		if !msg.IsUser {
			continue
		}

		words := strings.Fields(msg.Text)
		for _, word := range words {
			_, core, _ := splitPunctuation(word)
			clean := strings.ToLower(core)
			if clean == "" || len(clean) < 3 {
				continue
			}

			corrected := spellModel.SpellCheck(clean)
			if corrected != "" && corrected != clean {
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
