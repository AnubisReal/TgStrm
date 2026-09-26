package telegram

import "testing"

func TestSortImportDocumentsBySeasonAndEpisode(t *testing.T) {
	docs := []importDocument{
		{telegramID: 1, caption: "ALF S02E03 Título"},
		{telegramID: 2, caption: "ALF S01E02 Título"},
		{telegramID: 3, caption: "ALF S02E01 Título"},
		{telegramID: 4, caption: "ALF S01E01 Título"},
	}
	sortImportDocuments(docs)
	want := []string{"ALF S01E01 Título", "ALF S01E02 Título", "ALF S02E01 Título", "ALF S02E03 Título"}
	for index := range want {
		if docs[index].caption != want[index] {
			t.Fatalf("position %d: got %q, want %q", index, docs[index].caption, want[index])
		}
	}
}

func TestSortImportDocumentsKeepsUnknownChronological(t *testing.T) {
	docs := []importDocument{
		{telegramID: 8, caption: "extra posterior"},
		{telegramID: 3, caption: "ALF S01E02"},
		{telegramID: 2, caption: "extra anterior"},
		{telegramID: 9, caption: "ALF S01E01"},
	}
	sortImportDocuments(docs)
	wantIDs := []int{9, 3, 2, 8}
	for index := range wantIDs {
		if docs[index].telegramID != wantIDs[index] {
			t.Fatalf("position %d: got Telegram ID %d, want %d", index, docs[index].telegramID, wantIDs[index])
		}
	}
}
