package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
	"tgstrm/internal/library"
)

// TopicManifest describes the complete thread, including documents deliberately
// absent from the video catalog. It does not create destinations or send media.
type TopicManifest struct {
	Evidence          library.TopicEvidence `json:"evidence"`
	MessageSignatures map[string]string     `json:"message_signatures,omitempty"`
	MessageIDs        []int                 `json:"message_ids,omitempty"`
	VideoIDs          []int                 `json:"video_ids"`
	UpperMessageID    int                   `json:"upper_message_id"`
}

type repliesReader interface {
	MessagesGetReplies(context.Context, *tg.MessagesGetRepliesRequest) (tg.MessagesMessagesClass, error)
}

func (s *Service) ScanTopicEligibility(ctx context.Context, groupID, topicID string) (TopicManifest, error) {
	var peerID, accessHash int64
	var threadID int
	err := s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0),t.telegram_topic_id
 FROM tgstrm.topics t JOIN tgstrm.chats c ON c.id=t.chat_id
 WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3 AND c.is_active AND t.is_active AND c.is_forum`, groupID, topicID, accountID).Scan(&peerID, &accessHash, &threadID)
	if err != nil {
		return TopicManifest{}, err
	}
	if accessHash == 0 {
		return TopicManifest{}, errors.New("forum peer unavailable")
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return TopicManifest{}, errors.New("telegram is reconnecting")
	}
	return scanTopicManifest(ctx, catalogManifestReader{service: s, api: api, groupID: groupID, topicID: topicID}, &tg.InputPeerChannel{ChannelID: peerID, AccessHash: accessHash}, threadID)
}

func scanTopicManifest(ctx context.Context, api repliesReader, peer tg.InputPeerClass, threadID int) (TopicManifest, error) {
	manifest := TopicManifest{VideoIDs: make([]int, 0), MessageSignatures: make(map[string]string)}
	seen := make(map[int]struct{})
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			manifest.Evidence.ScanFailed = true
			return manifest, err
		}
		result, err := api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: peer, MsgID: threadID, OffsetID: offset, Limit: 100})
		if err != nil {
			manifest.Evidence.ScanFailed = true
			return manifest, err
		}
		modified, ok := result.AsModified()
		if !ok {
			manifest.Evidence.ScanFailed = true
			return manifest, errors.New("thread history returned an unverified cached response")
		}
		messages := modified.GetMessages()
		if len(messages) == 0 {
			manifest.Evidence.Complete = true
			return manifest, nil
		}
		next := 0
		for _, item := range messages {
			id := item.GetID()
			if id <= 0 {
				continue
			}
			if next == 0 || id < next {
				next = id
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			manifest.MessageIDs = append(manifest.MessageIDs, id)
			if id > manifest.UpperMessageID {
				manifest.UpperMessageID = id
			}
			message, ok := item.(*tg.Message)
			if !ok {
				continue
			}
			manifest.MessageSignatures[strconv.Itoa(id)] = manifestMessageSignature(message)
			collectTopicReferences(&manifest, message)
			classifyManifestMedia(&manifest, message)
		}
		// A short page does not establish completion. Follow the cursor until an
		// empty page, and fail closed if Telegram repeats a non-progressing cursor.
		if next == 0 || (offset > 0 && next >= offset) {
			manifest.Evidence.ScanFailed = true
			return manifest, errors.New("thread history pagination did not advance")
		}
		offset = next
	}
}

func classifyManifestMedia(manifest *TopicManifest, message *tg.Message) {
	if isVideoMedia(message.Media) {
		manifest.Evidence.Videos++
		manifest.VideoIDs = append(manifest.VideoIDs, message.ID)
		return
	}
	media, ok := message.Media.(*tg.MessageMediaDocument)
	if !ok {
		return
	}
	documentClass, ok := media.GetDocument()
	if !ok {
		manifest.Evidence.UnclassifiedDocuments++
		return
	}
	document, ok := documentClass.AsNotEmpty()
	if !ok {
		manifest.Evidence.UnclassifiedDocuments++
		return
	}
	// Photos, image documents and presentation stickers are not episode files.
	if strings.HasPrefix(strings.ToLower(document.MimeType), "image/") {
		return
	}
	for _, attribute := range document.Attributes {
		if _, ok := attribute.(*tg.DocumentAttributeSticker); ok {
			return
		}
	}
	name := ""
	for _, attribute := range document.Attributes {
		if value, ok := attribute.(*tg.DocumentAttributeFilename); ok {
			name = value.FileName
		}
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar", ".7z", ".001", ".r00", ".r01", ".tar", ".gz", ".bz2", ".xz":
		manifest.Evidence.NonVideoEpisodes++
	default:
		manifest.Evidence.UnclassifiedDocuments++
	}
}

// Reuse the complete eligibility response for catalog indexing; avoid reading
// the same thread through three additional video/search passes during review.
type catalogManifestReader struct {
	service          *Service
	api              repliesReader
	groupID, topicID string
}

func (r catalogManifestReader) MessagesGetReplies(ctx context.Context, req *tg.MessagesGetRepliesRequest) (tg.MessagesMessagesClass, error) {
	result, err := r.service.historyReadGate.read(ctx, func() (tg.MessagesMessagesClass, error) { return r.api.MessagesGetReplies(ctx, req) })
	if err != nil {
		return nil, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return result, nil
	}
	tx, err := r.service.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range modified.GetMessages() {
		if message, ok := item.(*tg.Message); ok {
			if _, err = storeVideoMessage(ctx, tx, r.groupID, r.topicID, message); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
