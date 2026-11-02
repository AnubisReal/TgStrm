package telegram

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gotd/td/tg"
	"tgstrm/internal/library"
)

var topicReferenceURL = regexp.MustCompile(`https?://[^\s<>"\[\]]+`)

// Keep bounded metadata returned by Telegram. Never fetch arbitrary message URLs.
func collectTopicReferences(manifest *TopicManifest, message *tg.Message) {
	add := func(raw, title string) {
		raw = strings.TrimRight(raw, ").,;!?")
		if len(raw) > 1024 || len(manifest.Evidence.References) >= 8 {
			return
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return
		}
		switch strings.ToLower(parsed.Hostname()) {
		case "filmaffinity.com", "www.filmaffinity.com", "themoviedb.org", "www.themoviedb.org", "imdb.com", "www.imdb.com":
		default:
			return
		}
		title = strings.TrimSpace(title)
		if utf8.RuneCountInString(title) > 250 {
			title = string([]rune(title)[:250])
		}
		for i, ref := range manifest.Evidence.References {
			if ref.URL == raw {
				if ref.Title == "" && title != "" {
					manifest.Evidence.References[i].Title = title
				}
				return
			}
		}
		manifest.Evidence.References = append(manifest.Evidence.References, library.TopicReference{URL: raw, Title: title})
	}
	if media, ok := message.Media.(*tg.MessageMediaWebPage); ok {
		if page, ok := media.Webpage.(*tg.WebPage); ok {
			add(page.URL, page.Title)
		}
	}
	for _, entity := range message.Entities {
		if link, ok := entity.(*tg.MessageEntityTextURL); ok {
			add(link.URL, "")
		}
	}
	for _, raw := range topicReferenceURL.FindAllString(message.Message, 8) {
		add(raw, "")
	}
}
