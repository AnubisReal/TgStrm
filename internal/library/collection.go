// Package library defines collection rules independently of Telegram transport.
package library

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Buckets are explicit coverage labels, never inferred from a renamed chat.
const Buckets = "ABCDEFGHIJKLMNÑOPQRSTUVWXYZ"

// NormalizeBucket validates the user-selected section of a collection.
func NormalizeBucket(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "0-9" || value == "OTHER" {
		return value, nil
	}
	for _, letter := range Buckets {
		if value == string(letter) {
			return value, nil
		}
	}
	return "", fmt.Errorf("invalid collection bucket %q", value)
}

// MissingBuckets reports sections not linked yet; it does not imply every
// section exists at the source or that a linked section finished importing.
func MissingBuckets(linked []string) ([]string, error) {
	present := make(map[string]bool, len(linked))
	for _, value := range linked {
		bucket, err := NormalizeBucket(value)
		if err != nil {
			return nil, err
		}
		present[bucket] = true
	}
	missing := make([]string, 0)
	for _, letter := range Buckets {
		if bucket := string(letter); !present[bucket] {
			missing = append(missing, bucket)
		}
	}
	return missing, nil
}

// DestinationName uses an explicit label; remote title changes cannot rename
// the destination or silently reassign its alphabetical section.
func DestinationName(kind, bucket string, part int) (string, error) {
	pattern := "Series {letra}"
	if kind == "movies" {
		pattern = "Películas {letra}"
	} else if kind != "series" {
		return "", fmt.Errorf("invalid collection kind %q", kind)
	}
	return FormatDestinationName(pattern, bucket, part)
}

// FormatDestinationName expands only supported placeholders, never expressions.
// An empty bucket is allowed for an unclassified movie cabinet.
func FormatDestinationName(pattern, bucket string, part int) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("name format must not be empty")
	}
	remaining := strings.ReplaceAll(strings.ReplaceAll(pattern, "{letra}", ""), "{parte}", "")
	if strings.ContainsAny(remaining, "{}") {
		return "", fmt.Errorf("unknown name placeholder")
	}
	if !utf8.ValidString(pattern) {
		return "", fmt.Errorf("invalid title encoding")
	}
	for _, char := range pattern {
		if unicode.IsControl(char) {
			return "", fmt.Errorf("title contains control characters")
		}
	}
	if part < 1 {
		return "", fmt.Errorf("part must be positive")
	}
	normalized := ""
	var err error
	if strings.TrimSpace(bucket) != "" {
		normalized, err = NormalizeBucket(bucket)
	}
	if err != nil {
		return "", err
	}
	if strings.Contains(pattern, "{letra}") && normalized == "" {
		return "", fmt.Errorf("format requires a bucket")
	}
	if normalized == "OTHER" {
		normalized = "Otros"
	}
	partLabel := ""
	if part > 1 {
		partLabel = fmt.Sprint(part)
	}
	name := strings.ReplaceAll(strings.ReplaceAll(pattern, "{letra}", normalized), "{parte}", partLabel)
	if part > 1 && !strings.Contains(pattern, "{parte}") {
		name += " " + partLabel
	}
	name = strings.Join(strings.Fields(name), " ")
	// Local conservative bound; server still validates the current Telegram limit.
	if name == "" || utf8.RuneCountInString(name) > 128 {
		return "", fmt.Errorf("title must have between 1 and 128 characters")
	}
	return name, nil
}

// LinkState separates proven removal from temporary loss of access. A rename
// needs no recovery because the persisted peer/topic identity stays the same.
func LinkState(sourceDeleted, sourceAccessible, destinationDeleted bool) string {
	if destinationDeleted {
		return "destination_missing"
	}
	if sourceDeleted {
		return "source_missing"
	}
	if !sourceAccessible {
		return "access_lost"
	}
	return "linked"
}
