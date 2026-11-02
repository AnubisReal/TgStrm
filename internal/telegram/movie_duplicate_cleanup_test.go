package telegram

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestDuplicateMoviePairValidation(t *testing.T) {
	video := func(id int, doc int64) *tg.Message {
		media := &tg.MessageMediaDocument{}
		media.SetDocument(&tg.Document{ID: doc, MimeType: "video/mp4"})
		return &tg.Message{ID: id, Out: true, Media: media}
	}
	makePair := func() map[int]*tg.Message {
		return map[int]*tg.Message{1: video(1, 42), 2: video(2, 42), 3: {ID: 3, Out: true, Message: "Película (2000)\nhttps://www.themoviedb.org/movie/77", Media: &tg.MessageMediaPhoto{}}}
	}
	for _, test := range []struct {
		name string
		edit func(map[int]*tg.Message)
		want int
		bad  bool
	}{
		{"verified pair", func(m map[int]*tg.Message) {}, 2, false},
		{"already deleted", func(m map[int]*tg.Message) { delete(m, 2); delete(m, 3) }, 0, false},
		{"different version", func(m map[int]*tg.Message) { m[2] = video(2, 43) }, 0, true},
		{"missing survivor", func(m map[int]*tg.Message) { delete(m, 1) }, 0, true},
		{"foreign poster", func(m map[int]*tg.Message) { m[3].Out = false }, 0, true},
		{"wrong link prefix", func(m map[int]*tg.Message) { m[3].Message = "https://www.themoviedb.org/movie/770" }, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := makePair()
			test.edit(m)
			ids, err := validateDuplicateMoviePair(m, 1, 2, 3, 42, 77)
			if (err != nil) != test.bad || len(ids) != test.want {
				t.Fatalf("ids=%v err=%v", ids, err)
			}
		})
	}
	if _, err := validateDuplicateMoviePair(makePair(), 1, 1, 3, 42, 77); err == nil {
		t.Fatal("survivor must not be deleted")
	}
}
