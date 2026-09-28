package httpapi

import (
	"testing"

	telegramauth "tgstrm/internal/telegram"
)

func TestEnrichTopicEpisodeNumbersWithoutSTRM(t *testing.T) {
	messages := []telegramauth.TopicMessage{
		{ID: "file-name", FileName: "Show.S01E01.mkv"},
		{ID: "caption", Body: "Temporada 1 Capítulo 03 - Final"},
		{ID: "manual", SeasonNumber: 2, EpisodeNumber: 4, FileName: "Show.S01E01.mkv"},
	}

	enrichTopicEpisodeNumbers(messages)

	want := map[string][2]int{
		"file-name": {1, 1},
		"caption":   {1, 3},
		"manual":    {2, 4},
	}
	for _, message := range messages {
		got := [2]int{message.SeasonNumber, message.EpisodeNumber}
		if got != want[message.ID] {
			t.Errorf("episode identity for %q = %v, want %v", message.ID, got, want[message.ID])
		}
	}
}
