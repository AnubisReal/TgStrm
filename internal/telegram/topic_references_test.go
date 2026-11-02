package telegram

import (
	"github.com/gotd/td/tg"
	"testing"
	"tgstrm/internal/library"
)

func TestTopicReferencesUseTelegramPreviewAndHiddenLinks(t *testing.T) {
	manifest := TopicManifest{Evidence: library.TopicEvidence{Complete: true, Videos: 1}}
	message := &tg.Message{Message: "https://www.filmaffinity.com/es/film123.html", Media: &tg.MessageMediaWebPage{Webpage: &tg.WebPage{URL: "https://www.filmaffinity.com/es/film123.html", Title: "ALF (Serie de TV) (1986) - FilmAffinity"}}, Entities: []tg.MessageEntityClass{&tg.MessageEntityTextURL{URL: "https://www.themoviedb.org/tv/4658-alf"}}}
	collectTopicReferences(&manifest, message)
	if len(manifest.Evidence.References) != 2 || manifest.Evidence.References[0].Title != "ALF (Serie de TV) (1986) - FilmAffinity" {
		t.Fatalf("lost metadata: %+v", manifest.Evidence.References)
	}
	if library.TopicEligibility(manifest.Evidence) != "eligible" {
		t.Fatal("references affected video eligibility")
	}
}
func TestTopicReferencesBoundedAndRejectLookalikeHosts(t *testing.T) {
	manifest := TopicManifest{}
	collectTopicReferences(&manifest, &tg.Message{Message: "https://filmaffinity.com.evil.test/es/film1.html https://user:pass@imdb.com/title/tt1/ https://localhost/"})
	if len(manifest.Evidence.References) != 0 {
		t.Fatal("accepted untrusted reference")
	}
	for i := 0; i < 20; i++ {
		collectTopicReferences(&manifest, &tg.Message{Media: &tg.MessageMediaWebPage{Webpage: &tg.WebPage{URL: "https://www.imdb.com/title/tt" + string(rune('a'+i)) + "/"}}})
	}
	if len(manifest.Evidence.References) != 8 {
		t.Fatal("reference bound not enforced")
	}
}
