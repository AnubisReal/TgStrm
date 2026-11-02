package telegram

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/gotd/td/tg"
)

var tmdbPosterPath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

func (s *Service) SendTopicPresentation(ctx context.Context, groupID, topicID, mediaType string, tmdbID int64, posterPath string) error {
	if tmdbID <= 0 || (mediaType != "tv" && mediaType != "movie") || !tmdbPosterPath.MatchString(posterPath) {
		return errors.New("presentación TMDB no válida")
	}
	var channelID, hash int64
	var threadID int
	if err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active`, groupID, topicID, accountID).Scan(&channelID, &hash, &threadID); err != nil {
		return err
	}
	randomID := stableRandomID("tmdb-presentation", topicID, mediaType, fmt.Sprint(tmdbID))
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tgstrm.topic_presentations(topic_id,random_id,tmdb_id,media_type,poster_path) VALUES($1::bigint,$2,$3,$4,$5) ON CONFLICT(topic_id) DO NOTHING`, topicID, randomID, tmdbID, mediaType, posterPath)
	if err != nil {
		return err
	}
	var status, storedType, storedPath string
	var storedID, storedRandom int64
	if err = s.DB.QueryRowContext(ctx, `SELECT status,tmdb_id,media_type,poster_path,random_id FROM tgstrm.topic_presentations WHERE topic_id::text=$1`, topicID).Scan(&status, &storedID, &storedType, &storedPath, &storedRandom); err != nil {
		return err
	}
	if storedID != tmdbID || storedType != mediaType {
		return errors.New("la presentación guardada pertenece a otra coincidencia; requiere revisión")
	}
	if status == "sent" || status == "skipped" {
		return nil
	}
	// Retry the persisted presentation, even if TMDB changed its preferred
	// poster meanwhile. The same random_id must refer to the same payload.
	posterPath = storedPath
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	peer := &tg.InputPeerChannel{ChannelID: channelID, AccessHash: hash}
	caption := fmt.Sprintf("https://www.themoviedb.org/%s/%d", mediaType, tmdbID)
	// Reconcile an accepted photo before retrying an uncertain network response.
	history, err := api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: peer, MsgID: threadID, Limit: 100})
	if err != nil {
		return err
	}
	if modified, ok := history.AsModified(); ok {
		for _, item := range modified.GetMessages() {
			if message, ok := item.(*tg.Message); ok && message.Out && message.Message == caption {
				if _, photo := message.Media.(*tg.MessageMediaPhoto); photo {
					_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.topic_presentations SET status='sent',telegram_message_id=$2,last_error=NULL,updated_at=now() WHERE topic_id::text=$1`, topicID, message.ID)
					return err
				}
			}
		}
	} else {
		return errors.New("no se pudo verificar la presentación anterior")
	}
	reply := &tg.InputReplyToMessage{ReplyToMsgID: threadID}
	reply.SetTopMsgID(threadID)
	updates, err := api.MessagesSendMedia(ctx, &tg.MessagesSendMediaRequest{Peer: peer, ReplyTo: reply, Media: &tg.InputMediaPhotoExternal{URL: "https://image.tmdb.org/t/p/w780" + posterPath}, Message: caption, RandomID: storedRandom})
	if err != nil {
		return err
	}
	var messageID int
	var list []tg.UpdateClass
	switch result := updates.(type) {
	case *tg.Updates:
		list = result.Updates
	case *tg.UpdatesCombined:
		list = result.Updates
	case *tg.UpdateShort:
		list = []tg.UpdateClass{result.Update}
	}
	for _, item := range list {
		if update, ok := item.(*tg.UpdateNewChannelMessage); ok {
			if message, ok := update.Message.(*tg.Message); ok && message.Message == caption {
				messageID = message.ID
			}
		}
		if update, ok := item.(*tg.UpdateMessageID); ok && update.RandomID == storedRandom {
			messageID = update.ID
		}
	}
	if messageID == 0 {
		return errors.New("presentación enviada; falta verificar su identidad antes de copiar vídeos")
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.topic_presentations SET status='sent',telegram_message_id=$2,last_error=NULL,updated_at=now() WHERE topic_id::text=$1`, topicID, messageID)
	return err
}
