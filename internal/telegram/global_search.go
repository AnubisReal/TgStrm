package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

// GlobalSearchItem is a read-only result from Telegram's account-wide search.
// It is deliberately not inserted into the catalogue until the user chooses
// to synchronize/import it.
type GlobalSearchItem struct {
	PeerID          int64
	PeerType        string
	PeerTitle       string
	MessageID       int
	TopicTelegramID int
	FileName        string
	Caption         string
	MIMEType        string
	ByteSize        int64
	Date            int
}

func globalPeerKey(peer tg.PeerClass) string {
	switch value := peer.(type) {
	case *tg.PeerChannel:
		return "channel:" + strconvInt64(value.ChannelID)
	case *tg.PeerChat:
		return "chat:" + strconvInt64(value.ChatID)
	case *tg.PeerUser:
		return "user:" + strconvInt64(value.UserID)
	default:
		return ""
	}
}

func strconvInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func (s *Service) GlobalSearch(ctx context.Context, query string, limit int) ([]GlobalSearchItem, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("empty Telegram search")
	}
	if limit < 1 || limit > 50 {
		limit = 25
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return nil, errors.New("telegram is reconnecting")
	}
	result, err := api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{Q: query, Filter: &tg.InputMessagesFilterEmpty{}, OffsetPeer: &tg.InputPeerEmpty{}, Limit: limit})
	if err != nil {
		return nil, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return nil, nil
	}
	titles := make(map[string]string, len(modified.GetChats()))
	for _, entity := range modified.GetChats() {
		switch value := entity.(type) {
		case *tg.Channel:
			titles["channel:"+strconvInt64(value.ID)] = value.Title
		case *tg.Chat:
			titles["chat:"+strconvInt64(value.ID)] = value.Title
		}
	}
	items := make([]GlobalSearchItem, 0, len(modified.GetMessages()))
	for _, entity := range modified.GetMessages() {
		message, ok := entity.(*tg.Message)
		if !ok {
			continue
		}
		media, ok := message.GetMedia()
		if !ok {
			continue
		}
		file, isVideo := videoMetadata(media)
		if !isVideo {
			continue
		}
		peer := message.GetPeerID()
		peerType, peerID := "", int64(0)
		switch value := peer.(type) {
		case *tg.PeerChannel:
			peerType, peerID = "channel", value.ChannelID
		case *tg.PeerChat:
			peerType, peerID = "chat", value.ChatID
		default:
			continue
		}
		topicID := 0
		if reply, ok := message.GetReplyTo(); ok {
			if header, ok := reply.(*tg.MessageReplyHeader); ok {
				if top, ok := header.GetReplyToTopID(); ok {
					topicID = top
				} else if parent, ok := header.GetReplyToMsgID(); ok {
					topicID = parent
				}
			}
		}
		items = append(items, GlobalSearchItem{PeerID: peerID, PeerType: peerType, PeerTitle: titles[globalPeerKey(peer)], MessageID: message.ID, TopicTelegramID: topicID, FileName: file.fileName, Caption: message.Message, MIMEType: file.mimeType, ByteSize: file.byteSize, Date: message.Date})
	}
	return items, nil
}
