package telegram

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/tg"
)

var ErrTopicNotFound = errors.New("topic not found")

type TopicMessage struct {
	ID               string     `json:"id"`
	TelegramID       int        `json:"telegram_id"`
	SenderPeerType   string     `json:"sender_peer_type,omitempty"`
	SenderPeerID     string     `json:"sender_peer_id,omitempty"`
	Body             string     `json:"body"`
	MediaType        string     `json:"media_type,omitempty"`
	ReplyToMessageID int        `json:"reply_to_message_id,omitempty"`
	SentAt           time.Time  `json:"sent_at"`
	EditedAt         *time.Time `json:"edited_at,omitempty"`
	Outgoing         bool       `json:"outgoing"`
	Pinned           bool       `json:"pinned"`
	FileName         string     `json:"file_name"`
	MIMEType         string     `json:"mime_type"`
	ByteSize         int64      `json:"byte_size"`
	Width            int        `json:"width,omitempty"`
	Height           int        `json:"height,omitempty"`
	DurationSeconds  int        `json:"duration_seconds,omitempty"`
	ExportError      string     `json:"export_error,omitempty"`
	SeasonNumber     int        `json:"season_number,omitempty"`
	EpisodeNumber    int        `json:"episode_number,omitempty"`
}

type VideoDetail struct {
	ID                string     `json:"id"`
	TelegramMessageID int        `json:"telegram_message_id"`
	Caption           string     `json:"caption"`
	FileName          string     `json:"file_name"`
	MIMEType          string     `json:"mime_type"`
	ByteSize          int64      `json:"byte_size"`
	Width             int        `json:"width,omitempty"`
	Height            int        `json:"height,omitempty"`
	DurationSeconds   int        `json:"duration_seconds,omitempty"`
	SentAt            time.Time  `json:"sent_at"`
	EditedAt          *time.Time `json:"edited_at,omitempty"`
}

type VideoDetailResponse struct {
	Group GroupDetail `json:"group"`
	Topic Topic       `json:"topic"`
	Video VideoDetail `json:"video"`
}

type videoFile struct {
	documentID, accessHash, byteSize int64
	fileReference                    []byte
	fileName, mimeType               string
	width, height, duration          int
	dcID                             int
}

func peerIdentity(peer tg.PeerClass) (string, int64) {
	switch value := peer.(type) {
	case *tg.PeerUser:
		return "user", value.UserID
	case *tg.PeerChannel:
		return "channel", value.ChannelID
	case *tg.PeerChat:
		return "chat", value.ChatID
	default:
		return "", 0
	}
}

func isVideoMedia(media tg.MessageMediaClass) bool {
	_, ok := videoMetadata(media)
	return ok
}

func videoMetadata(media tg.MessageMediaClass) (videoFile, bool) {
	value, ok := media.(*tg.MessageMediaDocument)
	if !ok {
		return videoFile{}, false
	}
	documentClass, ok := value.GetDocument()
	if !ok {
		return videoFile{}, false
	}
	document, ok := documentClass.AsNotEmpty()
	if !ok {
		return videoFile{}, false
	}
	result := videoFile{documentID: document.ID, accessHash: document.AccessHash, fileReference: document.FileReference,
		mimeType: document.MimeType, byteSize: document.Size, dcID: document.DCID}
	isVideo := value.Video || value.Round || strings.HasPrefix(strings.ToLower(document.MimeType), "video/")
	for _, attribute := range document.GetAttributes() {
		switch item := attribute.(type) {
		case *tg.DocumentAttributeVideo:
			isVideo = true
			result.width, result.height, result.duration = item.W, item.H, int(item.Duration+0.5)
		case *tg.DocumentAttributeFilename:
			result.fileName = item.FileName
		}
	}
	if !isVideo {
		switch strings.ToLower(filepath.Ext(result.fileName)) {
		case ".mp4", ".mkv", ".avi", ".mov", ".m4v", ".webm", ".wmv", ".flv", ".ts", ".m2ts", ".mpeg", ".mpg", ".3gp", ".ogv":
			isVideo = true
		}
	}
	return result, isVideo
}

func (s *Service) Topic(ctx context.Context, groupID, topicID string) (Topic, error) {
	var topic Topic
	err := s.DB.QueryRowContext(ctx, `SELECT t.id::text,t.telegram_topic_id,t.title,COALESCE(t.icon_color,0),
	 COALESCE(t.icon_emoji_id,0)::text,t.is_closed,t.is_hidden,t.is_pinned,t.unread_count,
	 t.unread_mentions_count,COALESCE(t.top_message_id,0),t.telegram_created_at,t.last_message_at,
	 COALESCE(stats.video_count,0),COALESCE(stats.total_bytes,0),COALESCE(exports.exported_count,0),
	 COALESCE(stats.unresolved_count,0),t.library_kind
	 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
	 LEFT JOIN LATERAL (
	   SELECT
	     count(DISTINCT m.id) FILTER (WHERE m.is_active AND m.media_type='video') AS video_count,
	     COALESCE(sum(mm.byte_size) FILTER (WHERE m.is_active AND m.media_type='video'),0) AS total_bytes,
	     count(DISTINCT sa.id) FILTER (WHERE m.is_active AND m.media_type='video' AND sa.last_export_error IS NOT NULL) AS unresolved_count
	   FROM tgstrm.messages m
	   LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
	   LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	   WHERE m.topic_id=t.id
	 ) stats ON true
	 LEFT JOIN LATERAL (
	   SELECT count(DISTINCT sa.id) AS exported_count
	   FROM tgstrm.messages m JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	   JOIN tgstrm.stream_exports e ON e.asset_id=sa.id AND e.status='written'
	   WHERE m.topic_id=t.id AND m.is_active
	 ) exports ON true
	 WHERE t.id::text=$1 AND t.chat_id::text=$2 AND t.is_active AND c.account_id=$3 AND c.is_active`, topicID, groupID, accountID).
		Scan(&topic.ID, &topic.TelegramID, &topic.Title, &topic.IconColor, &topic.IconEmojiID,
			&topic.Closed, &topic.Hidden, &topic.Pinned, &topic.UnreadCount, &topic.UnreadMentionsCount,
			&topic.TopMessageID, &topic.CreatedAt, &topic.LastMessageAt, &topic.VideoCount, &topic.TotalBytes,
			&topic.ExportedCount, &topic.UnresolvedCount, &topic.LibraryKind)
	if errors.Is(err, sql.ErrNoRows) {
		return Topic{}, ErrTopicNotFound
	}
	return topic, err
}

type TopicRemotePage struct {
	HasMore    bool
	NextOffset int
}

func (s *Service) RefreshTopicMessages(ctx context.Context, groupID, topicID string, before int) (TopicRemotePage, error) {
	var peerID, accessHash int64
	var telegramTopicID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id
	 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
	 WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.account_id=$3`, topicID, groupID, accountID).
		Scan(&peerID, &accessHash, &telegramTopicID)
	if errors.Is(err, sql.ErrNoRows) {
		return TopicRemotePage{}, ErrTopicNotFound
	}
	if err != nil {
		return TopicRemotePage{}, err
	}
	if accessHash == 0 {
		return TopicRemotePage{}, errors.New("forum peer unavailable")
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return TopicRemotePage{}, errors.New("telegram is reconnecting")
	}
	result, err := api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: &tg.InputPeerChannel{ChannelID: peerID, AccessHash: accessHash}, MsgID: telegramTopicID, OffsetID: before, Limit: 50})
	if err != nil {
		return TopicRemotePage{}, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return TopicRemotePage{}, nil
	}
	remoteMessages := modified.GetMessages()
	totalCount := len(remoteMessages)
	switch value := result.(type) {
	case *tg.MessagesMessagesSlice:
		totalCount = value.Count
	case *tg.MessagesChannelMessages:
		totalCount = value.Count
	}
	// Telegram may return fewer items than Limit even though the result is a
	// partial slice. Count is the authoritative total for slice constructors.
	// A final empty request closes pagination after a short last page.
	page := TopicRemotePage{HasMore: len(remoteMessages) > 0 && (len(remoteMessages) == 50 || totalCount > len(remoteMessages))}
	for _, item := range remoteMessages {
		if message, ok := item.(*tg.Message); ok && (page.NextOffset == 0 || message.ID < page.NextOffset) {
			page.NextOffset = message.ID
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return TopicRemotePage{}, err
	}
	defer tx.Rollback()
	for _, item := range remoteMessages {
		message, ok := item.(*tg.Message)
		if !ok {
			continue
		}
		if _, err = storeVideoMessage(ctx, tx, groupID, topicID, message); err != nil {
			return TopicRemotePage{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return TopicRemotePage{}, err
	}
	return page, nil
}

// RefreshTopicVideos uses Telegram's server-side video filter. It is intended
// for catalog reconciliation, where reading unrelated text messages would be
// wasteful. Interactive history loading keeps using getReplies above.
func (s *Service) RefreshTopicVideos(ctx context.Context, groupID, topicID string, before int) (TopicRemotePage, error) {
	return s.refreshTopicFilteredVideos(ctx, groupID, topicID, before, &tg.InputMessagesFilterVideo{})
}

func (s *Service) RefreshTopicVideoDocuments(ctx context.Context, groupID, topicID string, before int) (TopicRemotePage, error) {
	return s.refreshTopicFilteredVideos(ctx, groupID, topicID, before, &tg.InputMessagesFilterDocument{})
}

// SyncTopicVideoCatalog reconciles a forum thread through both the canonical
// replies history and Telegram's media filters. The filtered passes are
// intentional: some newly populated topics expose only the first replies page
// briefly, while search already exposes the complete video/document indexes.
// Upserts make the combined passes idempotent, and local classification keeps
// non-video documents out of the catalog.
func (s *Service) SyncTopicVideoCatalog(ctx context.Context, groupID, topicID string) error {
	for _, refresh := range []func(context.Context, string, string, int) (TopicRemotePage, error){
		s.RefreshTopicMessages,
		s.RefreshTopicVideos,
		s.RefreshTopicVideoDocuments,
	} {
		if err := scanTopicPages(ctx, groupID, topicID, refresh); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) refreshTopicFilteredVideos(ctx context.Context, groupID, topicID string, before int, filter tg.MessagesFilterClass) (TopicRemotePage, error) {
	var peerID, accessHash int64
	var telegramTopicID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id
		FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
		WHERE t.id::text=$1 AND c.id::text=$2 AND t.is_active AND c.account_id=$3`, topicID, groupID, accountID).
		Scan(&peerID, &accessHash, &telegramTopicID)
	if errors.Is(err, sql.ErrNoRows) {
		return TopicRemotePage{}, ErrTopicNotFound
	}
	if err != nil {
		return TopicRemotePage{}, err
	}
	if accessHash == 0 {
		return TopicRemotePage{}, errors.New("forum peer unavailable")
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return TopicRemotePage{}, errors.New("telegram is reconnecting")
	}
	request := &tg.MessagesSearchRequest{
		Peer: &tg.InputPeerChannel{ChannelID: peerID, AccessHash: accessHash},
		Q:    "", Filter: filter, OffsetID: before, Limit: 100,
	}
	request.SetTopMsgID(telegramTopicID)
	result, err := api.MessagesSearch(ctx, request)
	if err != nil {
		return TopicRemotePage{}, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return TopicRemotePage{}, nil
	}
	messages := modified.GetMessages()
	page := TopicRemotePage{HasMore: len(messages) == request.Limit}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return TopicRemotePage{}, err
	}
	defer tx.Rollback()
	for _, item := range messages {
		message, ok := item.(*tg.Message)
		if !ok {
			continue
		}
		if page.NextOffset == 0 || message.ID < page.NextOffset {
			page.NextOffset = message.ID
		}
		if _, err = storeVideoMessage(ctx, tx, groupID, topicID, message); err != nil {
			return TopicRemotePage{}, err
		}
	}
	return page, tx.Commit()
}

func storeVideoMessage(ctx context.Context, tx *sql.Tx, groupID, topicID string, message *tg.Message) (bool, error) {
	media, ok := message.GetMedia()
	file, video := videoMetadata(media)
	if !ok || !video {
		return false, nil
	}
	senderType, senderID, replyTo, groupedID := "", int64(0), 0, int64(0)
	if sender, ok := message.GetFromID(); ok {
		senderType, senderID = peerIdentity(sender)
	}
	if grouped, ok := message.GetGroupedID(); ok {
		groupedID = grouped
	}
	var editedAt *time.Time
	if edited, ok := message.GetEditDate(); ok {
		timestamp := time.Unix(int64(edited), 0).UTC()
		editedAt = &timestamp
	}
	if reply, ok := message.GetReplyTo(); ok {
		if header, ok := reply.(*tg.MessageReplyHeader); ok {
			replyTo, _ = header.GetReplyToMsgID()
		}
	}
	var storedMessageID int64
	err := tx.QueryRowContext(ctx, `INSERT INTO tgstrm.messages(chat_id,topic_id,telegram_message_id,sender_peer_type,sender_peer_id,
		 body,media_type,reply_to_message_id,grouped_id,sent_at,edited_at,is_outgoing,is_pinned,is_active)
		 VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5::bigint,0),$6,NULLIF($7,''),NULLIF($8,0),NULLIF($9::bigint,0),$10,$11,$12,$13,true)
		 ON CONFLICT(chat_id,telegram_message_id) DO UPDATE SET topic_id=EXCLUDED.topic_id,sender_peer_type=EXCLUDED.sender_peer_type,
		 sender_peer_id=EXCLUDED.sender_peer_id,body=EXCLUDED.body,media_type=EXCLUDED.media_type,reply_to_message_id=EXCLUDED.reply_to_message_id,
		 grouped_id=EXCLUDED.grouped_id,sent_at=EXCLUDED.sent_at,edited_at=EXCLUDED.edited_at,is_outgoing=EXCLUDED.is_outgoing,
		 is_pinned=EXCLUDED.is_pinned,is_active=true,last_seen_at=now(),updated_at=now()
		 RETURNING id`,
		groupID, topicID, message.ID, senderType, senderID, message.Message, "video", replyTo, groupedID,
		time.Unix(int64(message.Date), 0).UTC(), editedAt, message.Out, message.Pinned).Scan(&storedMessageID)
	if err != nil {
		return false, fmt.Errorf("store topic message: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.message_media(message_id,media_type,telegram_file_id,access_hash,file_reference,
		 file_name,mime_type,byte_size,width,height,duration_seconds,telegram_dc_id)
		 VALUES($1,'video',NULLIF($2::bigint,0),NULLIF($3::bigint,0),$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7::bigint,0),
		 NULLIF($8,0),NULLIF($9,0),NULLIF($10,0),NULLIF($11,0))
		 ON CONFLICT(message_id) DO UPDATE SET telegram_file_id=EXCLUDED.telegram_file_id,access_hash=EXCLUDED.access_hash,
		 file_reference=EXCLUDED.file_reference,file_name=EXCLUDED.file_name,mime_type=EXCLUDED.mime_type,byte_size=EXCLUDED.byte_size,
		 width=EXCLUDED.width,height=EXCLUDED.height,duration_seconds=EXCLUDED.duration_seconds,telegram_dc_id=EXCLUDED.telegram_dc_id,updated_at=now()`,
		storedMessageID, file.documentID, file.accessHash, file.fileReference, file.fileName, file.mimeType, file.byteSize,
		file.width, file.height, file.duration, file.dcID)
	if err != nil {
		return false, fmt.Errorf("store video metadata: %w", err)
	}
	sourceSignature := sha256.Sum256([]byte(message.Message + "\x00" + file.fileName))
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.stream_assets(message_id,source_signature) VALUES($1,$2)
		ON CONFLICT(message_id) DO UPDATE SET source_signature=EXCLUDED.source_signature,
		updated_at=CASE WHEN tgstrm.stream_assets.source_signature IS DISTINCT FROM EXCLUDED.source_signature THEN now() ELSE tgstrm.stream_assets.updated_at END`, storedMessageID, sourceSignature[:]); err != nil {
		return false, fmt.Errorf("ensure permanent stream identity: %w", err)
	}
	return true, nil
}

func (s *Service) Video(ctx context.Context, videoID string) (VideoDetailResponse, error) {
	var result VideoDetailResponse
	err := s.DB.QueryRowContext(ctx, `SELECT c.id::text,c.telegram_peer_id::text,c.title,COALESCE(c.username,''),c.telegram_peer_type,c.is_forum,
	 t.id::text,t.telegram_topic_id,t.title,COALESCE(t.icon_color,0),COALESCE(t.icon_emoji_id,0)::text,t.is_closed,t.is_hidden,t.is_pinned,
	 t.unread_count,t.unread_mentions_count,COALESCE(t.top_message_id,0),t.telegram_created_at,t.last_message_at,t.library_kind,
	 m.id::text,m.telegram_message_id,m.body,COALESCE(mm.file_name,''),COALESCE(mm.mime_type,''),COALESCE(mm.byte_size,0),
	 COALESCE(mm.width,0),COALESCE(mm.height,0),COALESCE(mm.duration_seconds,0),m.sent_at,m.edited_at
	 FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
	 JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=m.chat_id
	 WHERE m.id::text=$1 AND m.is_active AND m.media_type='video' AND t.is_active AND c.is_active AND c.account_id=$2`, videoID, accountID).
		Scan(&result.Group.ID, &result.Group.TelegramID, &result.Group.Title, &result.Group.Username, &result.Group.Type, &result.Group.Forum,
			&result.Topic.ID, &result.Topic.TelegramID, &result.Topic.Title, &result.Topic.IconColor, &result.Topic.IconEmojiID, &result.Topic.Closed,
			&result.Topic.Hidden, &result.Topic.Pinned, &result.Topic.UnreadCount, &result.Topic.UnreadMentionsCount, &result.Topic.TopMessageID,
			&result.Topic.CreatedAt, &result.Topic.LastMessageAt, &result.Topic.LibraryKind, &result.Video.ID, &result.Video.TelegramMessageID, &result.Video.Caption,
			&result.Video.FileName, &result.Video.MIMEType, &result.Video.ByteSize, &result.Video.Width, &result.Video.Height,
			&result.Video.DurationSeconds, &result.Video.SentAt, &result.Video.EditedAt)
	return result, err
}

func (s *Service) TopicMessages(ctx context.Context, topicID string, before, limit int) ([]TopicMessage, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id::text,m.telegram_message_id,COALESCE(m.sender_peer_type,''),COALESCE(m.sender_peer_id,0)::text,
	 m.body,COALESCE(m.media_type,''),COALESCE(m.reply_to_message_id,0),m.sent_at,m.edited_at,m.is_outgoing,m.is_pinned,
	 COALESCE(mm.file_name,''),COALESCE(mm.mime_type,''),COALESCE(mm.byte_size,0),COALESCE(mm.width,0),COALESCE(mm.height,0),COALESCE(mm.duration_seconds,0),
	 COALESCE(sa.last_export_error,''),COALESCE(eo.season_number,
		NULLIF(substring(written.relative_path FROM 'S([0-9]{1,2})E[0-9]{1,3}'),'')::integer,0),
	 COALESCE(eo.episode_number,
		NULLIF(substring(written.relative_path FROM 'S[0-9]{1,2}E([0-9]{1,3})'),'')::integer,0)
	 FROM tgstrm.messages m LEFT JOIN tgstrm.message_media mm ON mm.message_id=m.id
	 LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
	 LEFT JOIN tgstrm.stream_episode_overrides eo ON eo.asset_id=sa.id
	 LEFT JOIN LATERAL (SELECT e.relative_path FROM tgstrm.stream_exports e
		WHERE e.asset_id=sa.id AND e.status='written' ORDER BY e.updated_at DESC LIMIT 1) written ON true
	 WHERE m.topic_id=$1 AND m.is_active AND m.media_type='video' AND ($2=0 OR m.telegram_message_id<$2)
	 ORDER BY m.telegram_message_id DESC LIMIT $3`, topicID, before, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]TopicMessage, 0, limit+1)
	for rows.Next() {
		var message TopicMessage
		if err = rows.Scan(&message.ID, &message.TelegramID, &message.SenderPeerType, &message.SenderPeerID, &message.Body, &message.MediaType,
			&message.ReplyToMessageID, &message.SentAt, &message.EditedAt, &message.Outgoing, &message.Pinned,
			&message.FileName, &message.MIMEType, &message.ByteSize, &message.Width, &message.Height, &message.DurationSeconds, &message.ExportError,
			&message.SeasonNumber, &message.EpisodeNumber); err != nil {
			return nil, false, err
		}
		result = append(result, message)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(result) > limit
	if hasMore {
		result = result[:limit]
	}
	return result, hasMore, nil
}
