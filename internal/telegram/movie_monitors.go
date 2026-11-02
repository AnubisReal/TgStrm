package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// EnsureWholeGroupTopic is a local catalog scope, never a Telegram forum topic.
func (s *Service) EnsureWholeGroupTopic(ctx context.Context, groupID string, movies bool) (string, error) {
	var id string
	kind := "unclassified"
	if movies {
		kind = "movie_collection"
	}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO tgstrm.topics(chat_id,telegram_topic_id,title,library_kind)
 SELECT id,0,title,$3 FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active AND NOT is_forum AND telegram_peer_type IN ('channel','chat')
 ON CONFLICT(chat_id,telegram_topic_id) DO UPDATE SET is_active=true,library_kind=CASE WHEN $3='movie_collection' THEN $3 ELSE tgstrm.topics.library_kind END
 RETURNING id::text`, groupID, accountID, kind).Scan(&id)
	return id, err
}

// ReadMovieScopePage persists videos only; non-video documents are ignored by
// the same classifier used for series. Completion requires an empty page.
func (s *Service) ReadMovieScopePage(ctx context.Context, groupID, topicID string, before int, filterKind ...string) (int, int, error) {
	var peerID, hash int64
	var thread int
	if err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id FROM tgstrm.chats c JOIN tgstrm.topics t ON t.chat_id=c.id WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active`, groupID, topicID, accountID).Scan(&peerID, &hash, &thread); err != nil {
		return 0, 0, err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return 0, 0, errors.New("Telegram se está reconectando")
	}
	peer := &tg.InputPeerChannel{ChannelID: peerID, AccessHash: hash}
	var result tg.MessagesMessagesClass
	var err error
	if len(filterKind) > 0 {
		var filter tg.MessagesFilterClass = &tg.InputMessagesFilterVideo{}
		if filterKind[0] == "document" {
			filter = &tg.InputMessagesFilterDocument{}
		}
		request := &tg.MessagesSearchRequest{Peer: peer, Q: "", Filter: filter, OffsetID: before, Limit: 100}
		if thread > 0 {
			request.SetTopMsgID(thread)
		}
		result, err = api.MessagesSearch(ctx, request)
	} else if thread == 0 {
		result, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, OffsetID: before, Limit: 100})
	} else {
		result, err = api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: peer, MsgID: thread, OffsetID: before, Limit: 100})
	}
	if err != nil {
		return 0, 0, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return 0, 0, errors.New("historial sin verificar")
	}
	low, high := 0, 0
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, item := range modified.GetMessages() {
		id := item.GetID()
		if id > 0 && (low == 0 || id < low) {
			low = id
		}
		if id > high {
			high = id
		}
		if m, ok := item.(*tg.Message); ok {
			if _, err = storeVideoMessage(ctx, tx, groupID, topicID, m); err != nil {
				return 0, 0, err
			}
		}
	}
	if before > 0 && low >= before {
		return 0, 0, errors.New("la paginación de Telegram no avanzó")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::text))`, topicID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::text))`, groupID); err != nil {
		return 0, 0, err
	}
	return low, high, tx.Commit()
}

// A persisted creating marker prevents automatic duplicate group creation after
// an uncertain response; an operator must reconcile such an interruption.
func (s *Service) EnsureMovieCollectionGroup(ctx context.Context, collectionID string) (string, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	var id, status string
	if err := s.DB.QueryRowContext(ctx, `SELECT c.id::text FROM tgstrm.collection_groups cg JOIN tgstrm.chats c ON c.id=cg.destination_chat_id WHERE cg.collection_id::text=$1 AND cg.media_kind='movies' AND cg.state='ready' AND c.is_active AND NOT c.is_forum LIMIT 1`, collectionID).Scan(&id); err == nil {
		return id, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO tgstrm.movie_destination_jobs(collection_id) SELECT id FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2 AND state='ready' ON CONFLICT DO NOTHING`, collectionID, accountID); err != nil {
		return "", err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT status,COALESCE(destination_chat_id::text,'') FROM tgstrm.movie_destination_jobs WHERE collection_id::text=$1`, collectionID).Scan(&status, &id); err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	if status == "creating" || status == "attention" {
		return "", errors.New("creación anterior pendiente de reconciliar; no se duplicará el grupo")
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return "", errors.New("Telegram se está reconectando")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.movie_destination_jobs SET status='creating',updated_at=now() WHERE collection_id::text=$1 AND status='pending'`, collectionID)
	if err != nil {
		return "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if affected != 1 {
		return "", errors.New("otro proceso está creando el destino; espera antes de reintentar")
	}
	updates, err := api.ChannelsCreateChannel(ctx, &tg.ChannelsCreateChannelRequest{Megagroup: true, Title: "Películas", About: "Biblioteca TgStrm · películas · " + collectionID})
	if err != nil {
		var rpc *tgerr.Error
		if errors.As(err, &rpc) {
			_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.movie_destination_jobs SET status='pending',last_error=$2 WHERE collection_id::text=$1`, collectionID, err.Error())
		}
		return "", err
	}
	var chats []tg.ChatClass
	switch u := updates.(type) {
	case *tg.Updates:
		chats = u.Chats
	case *tg.UpdatesCombined:
		chats = u.Chats
	}
	for _, item := range chats {
		c, ok := item.(*tg.Channel)
		if !ok || !c.Creator || c.Forum {
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.chats(account_id,telegram_peer_type,telegram_peer_id,access_hash,title,is_forum) VALUES($1,'channel',$2,$3,$4,false) ON CONFLICT(account_id,telegram_peer_type,telegram_peer_id) DO UPDATE SET access_hash=EXCLUDED.access_hash,is_active=true RETURNING id::text`, accountID, c.ID, c.AccessHash, c.Title).Scan(&id)
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.collection_groups(collection_id,account_id,destination_chat_id,media_kind,bucket,last_applied_title) VALUES($1::uuid,$2,$3::bigint,'movies','','Películas')`, collectionID, accountID, id)
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.movie_destination_jobs SET status='ready',destination_chat_id=$2::bigint,last_error=NULL,updated_at=now() WHERE collection_id::text=$1`, collectionID, id)
		if err != nil {
			return "", err
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		_, err = s.EnsureWholeGroupTopic(ctx, id, true)
		return id, err
	}
	return "", errors.New("Telegram aceptó la creación; identidad pendiente de reconciliar")
}

// SendMonitoredMovie sends a single persisted stage. It reconciles an uncertain
// acceptance by document identity/caption before reusing its stable random_id.
func (s *Service) SendMonitoredMovie(ctx context.Context, destGroup, destTopic, sourceMessage, caption, poster string, randomID int64, presentation bool) (int, error) {
	var channel, hash int64
	var thread int
	if err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id FROM tgstrm.managed_catalog_chats c JOIN tgstrm.topics t ON t.chat_id=c.id WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active`, destGroup, destTopic, accountID).Scan(&channel, &hash, &thread); err != nil {
		return 0, err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return 0, errors.New("Telegram se está reconectando")
	}
	var doc, docHash int64
	var ref []byte
	if !presentation {
		if err := s.DB.QueryRowContext(ctx, `SELECT mm.telegram_file_id,mm.access_hash,mm.file_reference FROM tgstrm.message_media mm JOIN tgstrm.messages m ON m.id=mm.message_id WHERE m.id::text=$1 AND m.is_active AND m.media_type='video'`, sourceMessage).Scan(&doc, &docHash, &ref); err != nil {
			return 0, err
		}
	}
	var deliveryStarted time.Time
	if err := s.DB.QueryRowContext(ctx, `SELECT created_at FROM tgstrm.movie_monitor_deliveries WHERE presentation_random_id=$1 OR video_random_id=$1`, randomID).Scan(&deliveryStarted); err != nil {
		return 0, err
	}
	peer := &tg.InputPeerChannel{ChannelID: channel, AccessHash: hash}
	var history tg.MessagesMessagesClass
	var err error
	if thread == 0 {
		history, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 100})
	} else {
		history, err = api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: peer, MsgID: thread, Limit: 100})
	}
	if err != nil {
		return 0, err
	}
	if h, ok := history.AsModified(); ok {
		for _, item := range h.GetMessages() {
			m, ok := item.(*tg.Message)
			if !ok || !m.Out || m.Message != caption || int64(m.Date) < deliveryStarted.Unix() {
				continue
			}
			var otherDelivery bool
			if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries d JOIN tgstrm.movie_monitors mm ON mm.id=d.monitor_id WHERE mm.destination_chat_id::text=$1 AND ((d.presentation_telegram_message_id=$2 AND d.presentation_random_id<>$3) OR (d.destination_telegram_message_id=$2 AND d.video_random_id<>$3)))`, destGroup, m.ID, randomID).Scan(&otherDelivery); err != nil {
				return 0, err
			}
			if otherDelivery {
				continue
			}
			if presentation {
				if _, ok := m.Media.(*tg.MessageMediaPhoto); ok {
					return m.ID, nil
				}
			} else if f, ok := videoMetadata(m.Media); ok && f.documentID == doc {
				return m.ID, nil
			}
		}
	} else {
		return 0, errors.New("no se pudo verificar el envío anterior")
	}
	var media tg.InputMediaClass
	if presentation {
		if !tmdbPosterPath.MatchString(poster) {
			return 0, errors.New("falta un póster TMDB válido")
		}
		media = &tg.InputMediaPhotoExternal{URL: "https://image.tmdb.org/t/p/w780" + poster}
	} else {
		media = &tg.InputMediaDocument{ID: &tg.InputDocument{ID: doc, AccessHash: docHash, FileReference: ref}}
	}
	request := &tg.MessagesSendMediaRequest{Peer: peer, Media: media, Message: caption, RandomID: randomID}
	if thread > 0 {
		reply := &tg.InputReplyToMessage{ReplyToMsgID: thread}
		reply.SetTopMsgID(thread)
		request.ReplyTo = reply
	}
	updates, err := api.MessagesSendMedia(ctx, request)
	// A definite remote-fetch failure can be retried with the same random ID
	// using an uploaded photo, without relying on Telegram's URL downloader.
	if presentation && tgerr.Is(err, "WEBPAGE_CURL_FAILED") {
		photo, fetchErr := fetchMoviePoster(ctx, moviePosterClient, poster)
		if fetchErr != nil {
			return 0, fmt.Errorf("%w: %v", ErrMoviePosterPreparation, fetchErr)
		}
		uploaded, uploadErr := uploader.NewUploader(api).FromBytes(ctx, "poster.jpg", photo)
		if uploadErr != nil {
			return 0, fmt.Errorf("%w: %v", ErrMoviePosterPreparation, uploadErr)
		}
		request.Media = &tg.InputMediaUploadedPhoto{File: uploaded}
		updates, err = api.MessagesSendMedia(ctx, request)
	}

	if !presentation && tgerr.Is(err, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID") {
		var location VideoFileLocation
		location.VideoID = sourceMessage
		location.DocumentID = doc
		location.AccessHash = docHash
		location.FileReference = ref
		refreshErr := s.DB.QueryRowContext(ctx, `SELECT m.chat_id::text,m.topic_id::text,m.telegram_message_id,c.telegram_peer_id,COALESCE(c.access_hash,0),COALESCE(mm.file_name,''),COALESCE(mm.telegram_dc_id,0) FROM tgstrm.messages m JOIN tgstrm.chats c ON c.id=m.chat_id JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE m.id::text=$1 AND c.account_id=$2`, sourceMessage, accountID).Scan(&location.GroupID, &location.TopicID, &location.MessageID, &location.ChannelID, &location.ChannelHash, &location.FileName, &location.DCID)
		if refreshErr == nil {
			var fresh VideoFileLocation
			fresh, refreshErr = s.refreshExpiredVideoLocation(ctx, location)
			if refreshErr == nil {
				request.Media = &tg.InputMediaDocument{ID: &tg.InputDocument{ID: fresh.DocumentID, AccessHash: fresh.AccessHash, FileReference: fresh.FileReference}}
				updates, err = api.MessagesSendMedia(ctx, request)
			}
		}
		if refreshErr != nil {
			err = refreshErr
		}
	}
	if err != nil {
		return 0, err
	}
	id := messageIDFromUpdates(updates, randomID)
	if id <= 0 {
		return 0, errors.New("Telegram no confirmó el ID del envío")
	}
	return id, nil
}

func (s *Service) IndexMonitoredMovie(ctx context.Context, groupID, topicID string, remoteID int) (string, error) {
	var channel, hash int64
	if err := s.DB.QueryRowContext(ctx, `SELECT telegram_peer_id,COALESCE(access_hash,0) FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2`, groupID, accountID).Scan(&channel, &hash); err != nil {
		return "", err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return "", errors.New("Telegram se está reconectando")
	}
	result, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{Channel: &tg.InputChannel{ChannelID: channel, AccessHash: hash}, ID: []tg.InputMessageClass{&tg.InputMessageID{ID: remoteID}}})
	if err != nil {
		return "", err
	}
	h, ok := result.AsModified()
	if !ok {
		return "", errors.New("mensaje enviado sin verificar")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	found := false
	for _, item := range h.GetMessages() {
		if m, ok := item.(*tg.Message); ok && m.ID == remoteID {
			found, err = storeVideoMessage(ctx, tx, groupID, topicID, m)
			if err != nil {
				return "", err
			}
		}
	}
	if !found {
		return "", errors.New("Telegram no devolvió el vídeo enviado")
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.messages WHERE chat_id::text=$1 AND telegram_message_id=$2`, groupID, remoteID).Scan(&id)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('videos.snapshot','topic',$1::bigint,jsonb_build_object('topic_id',$1::text))`, topicID)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.outbox_events(event_type,aggregate_type,aggregate_id,payload) VALUES('topics.snapshot','group',$1::bigint,jsonb_build_object('group_id',$1::text))`, groupID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
