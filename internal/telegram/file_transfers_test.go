package telegram

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestForwardedFileBatchRequiresEveryConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		updates   tg.UpdatesClass
		wantError bool
	}{
		{"all accepted", &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{RandomID: 11, ID: 1}, &tg.UpdateMessageID{RandomID: 12, ID: 2}}}, false},
		{"partial acceptance", &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{RandomID: 11, ID: 1}}}, true},
		{"unrelated updates", &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageID{RandomID: 99, ID: 1}}}, true},
		{"updates unavailable", &tg.UpdatesTooLong{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := confirmForwardedFileBatch(tc.updates, []int64{11, 12})
			if (err != nil) != tc.wantError {
				t.Fatalf("confirmation error = %v", err)
			}
		})
	}
}
