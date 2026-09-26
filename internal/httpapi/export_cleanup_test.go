package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveEmptyExportParentsRemovesOnlyKnownMetadata(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "Series", "Example", "Season 01")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	strm := filepath.Join(season, "Example S01E01.strm")
	if err := os.WriteFile(filepath.Join(season, ".DS_Store"), []byte("finder"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeEmptyExportParents(strm, root)
	if _, err := os.Stat(filepath.Join(root, "Series")); !os.IsNotExist(err) {
		t.Fatalf("metadata-only export tree remains: %v", err)
	}

	preserved := filepath.Join(root, "Series", "Preserved")
	if err := os.MkdirAll(preserved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(preserved, "poster.jpg"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeEmptyExportParents(filepath.Join(preserved, "old.strm"), root)
	if _, err := os.Stat(filepath.Join(preserved, "poster.jpg")); err != nil {
		t.Fatalf("user content was removed: %v", err)
	}
}

func TestDeletedTopicTreesAreRecursiveButNeverSharedOrRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shows")
	exports := []reconciledExport{
		{rootPath: root, relativePath: "Deleted/Season 01/E01.strm", topicActive: false, chatActive: true},
		{rootPath: root, relativePath: "Shared/Season 01/E01.strm", topicActive: false, chatActive: true},
		{rootPath: root, relativePath: "Shared/Season 02/E01.strm", topicActive: true, chatActive: true},
		{rootPath: root, relativePath: ".", topicActive: false, chatActive: true},
	}
	trees := deletedTopicTrees(exports)
	if _, ok := trees[filepath.Join(root, "Deleted")]; !ok {
		t.Fatal("deleted topic directory was not selected")
	}
	if _, ok := trees[filepath.Join(root, "Shared")]; ok {
		t.Fatal("directory shared by an active topic was selected")
	}
	if _, ok := trees[root]; ok {
		t.Fatal("library root was selected")
	}
}
