package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// entityAliases maps entity names to their known aliases.
// When searching for any name in a group, all aliases in that group are searched.
var entityAliases = map[string][]string{}

func loadAliasesFromEnv() {
	aliasEnv := strings.TrimSpace(os.Getenv("MNEME_ALIASES"))
	if aliasEnv == "" {
		return
	}

	for _, group := range strings.Split(aliasEnv, ";") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		parts := strings.SplitN(group, "=", 2)
		if len(parts) != 2 {
			continue
		}
		alias := strings.ToLower(strings.TrimSpace(parts[0]))
		if alias == "" {
			continue
		}
		rawNames := strings.Split(parts[1], ",")
		names := make([]string, 0, len(rawNames))
		for _, name := range rawNames {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			names = append(names, name)
		}
		if len(names) == 0 {
			continue
		}
		for _, name := range names {
			entityAliases[strings.ToLower(name)] = names
		}
	}
}

// resolveAliases returns all names to search for a given entity.
// If the entity has aliases, returns all of them. Otherwise returns just the entity.
func resolveAliases(entity string) []string {
	key := strings.ToLower(strings.TrimSpace(entity))
	if aliases, ok := entityAliases[key]; ok {
		return aliases
	}
	return []string{entity}
}

type HistoryResult struct {
	ID           int
	Text         string
	SourceFile   string
	SectionTitle string
	ParentTitle  string
	ValidAt      string
	IngestedAt   string
}

// History searches chunks for entity (and its aliases) and returns results in chronological order.
// NULLs in valid_at come first (timeless before dated), then sorted by valid_at ASC, then section_sequence ASC.
// If limit <= 0, defaults to 20.
func History(db *sql.DB, entity string, limit int) ([]HistoryResult, error) {
	if limit <= 0 {
		limit = 20
	}

	names := resolveAliases(entity)

	conditions := make([]string, len(names))
	args := make([]any, len(names))
	for i, name := range names {
		conditions[i] = "text LIKE ? ESCAPE '\\' COLLATE NOCASE"
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(name)
		args[i] = "%" + escaped + "%"
	}
	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT id, text, source_file, section_title, parent_title, valid_at, ingested_at
		 FROM chunks
		 WHERE (%s)
		 ORDER BY CASE WHEN valid_at IS NULL THEN 0 ELSE 1 END, valid_at ASC, section_sequence ASC
		 LIMIT ?`,
		strings.Join(conditions, " OR "),
	)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []HistoryResult{}
	for rows.Next() {
		var result HistoryResult
		var parentTitle sql.NullString
		var validAt sql.NullString
		if err := rows.Scan(
			&result.ID,
			&result.Text,
			&result.SourceFile,
			&result.SectionTitle,
			&parentTitle,
			&validAt,
			&result.IngestedAt,
		); err != nil {
			return nil, err
		}
		if parentTitle.Valid {
			result.ParentTitle = parentTitle.String
		}
		if validAt.Valid {
			result.ValidAt = validAt.String
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
