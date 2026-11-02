package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	telegramauth "tgstrm/internal/telegram"
)

func TestDirectGroupHistoryAndTransferValidationDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	a := &API{db: db, telegram: &telegramauth.Service{DB: db}}
	// The user-reported group must become browseable without any Telegram write.
	var sourceGroup string
	if err := db.QueryRow(`SELECT id::text FROM tgstrm.chats WHERE id=103 AND NOT is_forum AND is_active`).Scan(&sourceGroup); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("groupID", sourceGroup)
	w := httptest.NewRecorder()
	a.groupTopics(w, r)
	if w.Code != 200 {
		t.Fatalf("group history: %d %s", w.Code, w.Body)
	}
	var data struct {
		Topics []struct {
			ID         string `json:"id"`
			TelegramID int    `json:"telegram_id"`
		} `json:"topics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	var sourceTopic string
	for _, topic := range data.Topics {
		if topic.TelegramID == 0 {
			sourceTopic = topic.ID
		}
	}
	if sourceTopic == "" {
		t.Fatal("non-forum group has no history scope")
	}
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"self copy", `{"destination_group_id":"103","message_ids":["-1"]}`, 422},
		{"foreign destination", `{"destination_group_id":"-1","message_ids":["-1"]}`, 422},
		{"empty selection", `{"destination_group_id":"103","message_ids":[]}`, 400},
		{"ambiguous all selection", `{"destination_group_id":"103","all":true,"message_ids":["-1"]}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			r.SetPathValue("groupID", sourceGroup)
			r.SetPathValue("topicID", sourceTopic)
			w := httptest.NewRecorder()
			a.createFileTransfer(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
		})
	}
	// Read model must include the new queue without SQL errors.
	r = httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	a.jobs(w, r)
	if w.Code != 200 {
		t.Fatalf("jobs: %d %s", w.Code, w.Body)
	}
}

func TestFileTransferLeaseAndManifestDatabase(t *testing.T) {
	db := correctionsAuditDB(t)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var topic, group string
	var messageIDs []byte
	err = tx.QueryRowContext(ctx, `SELECT m.topic_id::text,t.chat_id::text,jsonb_agg(m.id::text ORDER BY m.telegram_message_id) FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id WHERE m.is_active AND m.media_type='video' GROUP BY m.topic_id,t.chat_id HAVING count(*)>1 LIMIT 1`).Scan(&topic, &group, &messageIDs)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	if err = json.Unmarshal(messageIDs, &ids); err != nil {
		t.Fatal(err)
	}
	var job string
	if err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.file_transfers(source_topic_id,destination_chat_id,message_ids,created_at) VALUES($1::bigint,$2::bigint,$3::bigint[],'2000-01-01') RETURNING id::text`, topic, group, ids[:2]).Scan(&job); err != nil {
		t.Fatal(err)
	}
	var got, sg, st, dg, dt string
	var completed int
	if err = tx.QueryRowContext(ctx, fileTransferClaimSQL).Scan(&got, &sg, &st, &dg, &dt, &completed); err != nil {
		t.Fatal(err)
	}
	if got != job || sg != group || st != topic || dg != group || dt != "" || completed != 0 {
		t.Fatalf("incorrect claim: %s %s %s %s %s %d", got, sg, st, dg, dt, completed)
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM tgstrm.file_transfers WHERE id::text=$1`, job).Scan(&status); err != nil || status != "running" {
		t.Fatalf("claim %s: %v", status, err)
	}
	// Rollback leaves no real transfer for the live worker to send.
}
