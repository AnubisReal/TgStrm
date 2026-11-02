package httpapi

import (
	"context"
	"encoding/json"
	"testing"
)

// Opt-in, read-only: exercises the same active catalogue membership used by
// the endpoint without creating imports or contacting Telegram.
func TestManualImportDestinationsDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	a := &API{db: db}
	ctx := context.Background()
	var sourceGroup, sourceTopic, group, topic string
	var tmdbID int64
	err := db.QueryRowContext(ctx, `SELECT s.chat_id::text,s.id::text,c.id::text,t.id::text,tm.tmdb_id
 FROM tgstrm.active_catalog_chats c JOIN tgstrm.topics t ON t.chat_id=c.id
 JOIN tgstrm.tmdb_matches tm ON tm.topic_id=t.id AND tm.message_id IS NULL AND tm.media_type='tv'
 JOIN tgstrm.chats sc ON sc.account_id=c.account_id AND sc.is_active
 JOIN tgstrm.topics s ON s.chat_id=sc.id AND s.is_active AND s.id<>t.id
 WHERE c.is_active AND c.is_forum AND t.is_active AND t.library_kind<>'movie_collection' LIMIT 1`).Scan(&sourceGroup, &sourceTopic, &group, &topic, &tmdbID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, requestedGroup, requestedTopic string
		match                                int64
		wantError                            bool
	}{
		{"existing topic explicit group", group, topic, tmdbID, false},
		{"legacy topic only", "", topic, tmdbID, false},
		{"create in chosen group", group, "", tmdbID, false},
		{"different series", group, topic, tmdbID + 100000000, true},
		{"mismatched group", "-1", topic, tmdbID, true},
		{"missing group", "-1", "", tmdbID, true},
		{"missing topic", group, "-1", tmdbID, true},
		{"self copy", sourceGroup, sourceTopic, tmdbID, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.resolveManualImportDestination(ctx, sourceGroup, sourceTopic, tc.requestedGroup, tc.requestedTopic, "tv", tc.match)
			if (err != nil) != tc.wantError {
				t.Fatalf("group=%s error=%v", got, err)
			}
			if !tc.wantError && got != group {
				t.Fatalf("wrong group %s", got)
			}
		})
	}
	groups := a.importDestinationGroups(ctx, sourceGroup)
	if len(groups) == 0 {
		t.Fatal("no destination groups returned")
	}
	if len(groups) > 1 {
		if _, err := a.resolveManualImportDestination(ctx, sourceGroup, sourceTopic, "", "", "tv", tmdbID); err == nil {
			t.Fatal("ambiguous group accepted")
		}
	}
	if _, err := a.resolveManualImportDestination(ctx, sourceGroup, sourceTopic, group, topic, "movie", tmdbID); err == nil {
		t.Fatal("movie accepted into series topic")
	}
}

func TestManualImportPendingSelectionDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	var id, group, topic, title, media string
	var match int64
	var encoded []byte
	err := db.QueryRow(`SELECT ci.id::text,ci.destination_chat_id::text,COALESCE(ci.destination_topic_id::text,''),ci.destination_title,COALESCE(ci.tmdb_id,0),COALESCE(ci.tmdb_media_type,''),
 COALESCE((SELECT jsonb_agg(source_message_id::text) FROM tgstrm.catalog_import_messages WHERE import_id=ci.id AND selected_for_run),'[]'::jsonb)
 FROM tgstrm.catalog_imports ci WHERE ci.destination_chat_id IS NOT NULL ORDER BY ci.created_at DESC LIMIT 1`).Scan(&id, &group, &topic, &title, &match, &media, &encoded)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err = json.Unmarshal(encoded, &ids); err != nil {
		t.Fatal(err)
	}
	var same bool
	if err = db.QueryRow(manualImportRequestMatchesSQL, id, group, topic, title, match, media, ids).Scan(&same); err != nil || !same {
		t.Fatalf("same request: %v %v", same, err)
	}
	if err = db.QueryRow(manualImportRequestMatchesSQL, id, group, topic, title, match, media, append(ids, "-1")).Scan(&same); err != nil || same {
		t.Fatalf("changed selection accepted: %v %v", same, err)
	}
	if err = db.QueryRow(manualImportRequestMatchesSQL, id, "-1", topic, title, match, media, ids).Scan(&same); err != nil || same {
		t.Fatalf("changed group accepted: %v %v", same, err)
	}
}
