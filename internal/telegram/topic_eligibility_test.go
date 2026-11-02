package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/tg"
	"tgstrm/internal/library"
)

type manifestReader struct {
	pages   [][]tg.MessageClass
	offsets []int
	fail    bool
}

func (r *manifestReader) MessagesGetReplies(_ context.Context, req *tg.MessagesGetRepliesRequest) (tg.MessagesMessagesClass, error) {
	r.offsets = append(r.offsets, req.OffsetID)
	if r.fail {
		return nil, errors.New("connection lost")
	}
	index := len(r.offsets) - 1
	if index >= len(r.pages) {
		return &tg.MessagesMessages{}, nil
	}
	return &tg.MessagesMessagesSlice{Count: 1000, Messages: r.pages[index]}, nil
}
func manifestDocument(id int, name, mime string) *tg.Message {
	media := &tg.MessageMediaDocument{}
	media.SetDocument(&tg.Document{ID: int64(id), MimeType: mime, Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}}})
	return &tg.Message{ID: id, Media: media}
}
func TestManifestRejectsArchiveOnLaterShortPage(t *testing.T) {
	reader := &manifestReader{pages: [][]tg.MessageClass{{manifestDocument(30, "episode.mkv", "application/octet-stream")}, {manifestDocument(10, "episode.zip", "application/zip")}, nil}}
	manifest, err := scanTopicManifest(context.Background(), reader, &tg.InputPeerChannel{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Evidence.Complete || library.TopicEligibility(manifest.Evidence) != "excluded_nonvideo" || manifest.Evidence.Videos != 1 {
		t.Fatalf("unsafe manifest: %+v", manifest)
	}
	if len(reader.offsets) != 3 || reader.offsets[1] != 30 || reader.offsets[2] != 10 {
		t.Fatalf("pagination: %v", reader.offsets)
	}
}
func TestManifestDoesNotTreatInterruptedOrRepeatedHistoryAsComplete(t *testing.T) {
	for _, reader := range []*manifestReader{{fail: true}, {pages: [][]tg.MessageClass{{manifestDocument(10, "a.mp4", "video/mp4")}, {manifestDocument(10, "a.mp4", "video/mp4")}}}} {
		manifest, err := scanTopicManifest(context.Background(), reader, &tg.InputPeerChannel{}, 1)
		if err == nil || manifest.Evidence.Complete || library.TopicEligibility(manifest.Evidence) != "scan_failed" {
			t.Fatalf("unsafe scan: %+v %v", manifest, err)
		}
	}
}
func TestManifestAcceptsVideoDocumentsAndIgnoresPresentation(t *testing.T) {
	reader := &manifestReader{pages: [][]tg.MessageClass{{manifestDocument(30, "video.mkv", "application/octet-stream"), manifestDocument(20, "poster.jpg", "image/jpeg"), &tg.Message{ID: 10, Message: "https://www.themoviedb.org/tv/4658"}}, nil}}
	manifest, err := scanTopicManifest(context.Background(), reader, &tg.InputPeerChannel{}, 1)
	if err != nil || library.TopicEligibility(manifest.Evidence) != "eligible" || len(manifest.VideoIDs) != 1 {
		t.Fatalf("manifest: %+v %v", manifest, err)
	}
}
func TestManifestUnknownDocumentRequiresReview(t *testing.T) {
	reader := &manifestReader{pages: [][]tg.MessageClass{{manifestDocument(20, "a.mp4", "video/mp4"), manifestDocument(10, "notes.pdf", "application/pdf")}, nil}}
	manifest, err := scanTopicManifest(context.Background(), reader, &tg.InputPeerChannel{}, 1)
	if err != nil || library.TopicEligibility(manifest.Evidence) != "review" {
		t.Fatalf("manifest: %+v %v", manifest, err)
	}
}
