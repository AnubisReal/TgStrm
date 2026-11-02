package telegram

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"tgstrm/internal/config"
	"tgstrm/internal/storage/postgres"

	"github.com/gotd/td/tg"
)

func TestDeletedManifestScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ids      []int
		resolved []deletedMessageTopic
		topics   []int
		unknown  bool
	}{
		{"one topic", []int{10, 11}, []deletedMessageTopic{{10, 7}, {11, 7}}, []int{7}, false},
		{"batch across topics", []int{10, 11}, []deletedMessageTopic{{10, 7}, {11, 9}}, []int{7, 9}, false},
		{"repeated ID", []int{10, 10}, []deletedMessageTopic{{10, 7}}, []int{7}, false},
		{"unknown in mixed batch", []int{10, 12}, []deletedMessageTopic{{10, 7}}, nil, true},
		{"plain group", []int{10}, []deletedMessageTopic{{10, 0}}, []int{0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, unknown := deletedManifestScope(tc.ids, tc.resolved)
			if !reflect.DeepEqual(got, tc.topics) || unknown != tc.unknown {
				t.Fatalf("topics=%v unknown=%v", got, unknown)
			}
		})
	}
}

func TestManifestInventoryIncludesNonvideoAndReferences(t *testing.T) {
	reader := &manifestReader{pages: [][]tg.MessageClass{{manifestDocument(30, "episode.mkv", "application/octet-stream"), manifestDocument(20, "episode.zip", "application/zip"), &tg.Message{ID: 10, Message: "https://www.themoviedb.org/tv/123"}}, nil}}
	manifest, err := scanTopicManifest(context.Background(), reader, &tg.InputPeerChannel{}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.MessageIDs, []int{30, 20, 10}) {
		t.Fatal(manifest.MessageIDs)
	}
	data, err := marshalManifestEvidence(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Videos int   `json:"videos"`
		IDs    []int `json:"message_ids"`
	}
	if err = json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Videos != 1 || !reflect.DeepEqual(evidence.IDs, manifest.MessageIDs) {
		t.Fatalf("evidence=%s", data)
	}
}

// Read-only regression against real catalogue IDs. Never writes or invalidates.
func TestDeletedManifestLookupReadOnly(t *testing.T) {
	dir := os.Getenv("TGSTRM_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("opt-in PostgreSQL audit")
	}
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadDatabase()
	if err != nil || cfg == nil {
		t.Fatal("database config unavailable")
	}
	ctx := context.Background()
	db, err := postgres.Connect(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var channel int64
	var message, topic int
	err = db.QueryRowContext(ctx, `SELECT c.telegram_peer_id,m.telegram_message_id,t.telegram_topic_id FROM tgstrm.messages m JOIN tgstrm.chats c ON c.id=m.chat_id JOIN tgstrm.topics t ON t.id=m.topic_id WHERE c.account_id=$1 AND c.telegram_peer_type='channel' AND c.is_forum LIMIT 1`, accountID).Scan(&channel, &message, &topic)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, deletedManifestTopicsSQL, accountID, channel, []int{message})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var v deletedMessageTopic
		if err = rows.Scan(&v.message, &v.topic); err != nil {
			t.Fatal(err)
		}
		if v.message != message || v.topic != topic {
			t.Fatalf("unrelated topic: %+v", v)
		}
		found = true
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("known message not resolved")
	}
}
