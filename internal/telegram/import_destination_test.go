package telegram

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestImportDestinationRequiresAccessibleExactForum(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chat  tg.ChatClass
		valid bool
	}{
		{"exact", &tg.Channel{ID: 42, Forum: true}, true},
		{"other", &tg.Channel{ID: 43, Forum: true}, false},
		{"left", &tg.Channel{ID: 42, Forum: true, Left: true}, false},
		{"partial", &tg.Channel{ID: 42, Forum: true, Min: true}, false},
		{"not_forum", &tg.Channel{ID: 42}, false},
		{"forbidden", &tg.ChannelForbidden{ID: 42}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := confirmedImportForum(tc.chat, 42)
			if ok != tc.valid {
				t.Fatalf("accepted=%v", ok)
			}
		})
	}
}
