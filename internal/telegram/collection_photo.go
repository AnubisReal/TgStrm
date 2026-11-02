package telegram

import (
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"os"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// ApplyCollectionGroupPhoto only addresses a persisted collection destination.
// Existing Telegram photos are retained (including an accepted uncertain retry).
func (s *Service) ApplyCollectionGroupPhoto(ctx context.Context, groupID, path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil || info.Size() > 2*1024*1024 {
		file.Close()
		return 0, errors.New("imagen de cajón demasiado grande")
	}
	cfg, _, err := image.DecodeConfig(file)
	file.Close()
	if err != nil || cfg.Width != 512 || cfg.Height != 512 {
		return 0, errors.New("imagen de cajón no válida")
	}
	var channelID, hash int64
	err = s.DB.QueryRowContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0) FROM tgstrm.chats c JOIN tgstrm.collection_groups cg ON cg.destination_chat_id=c.id WHERE c.id::text=$1 AND c.account_id=$2 AND c.is_active AND cg.state='ready' LIMIT 1`, groupID, accountID).Scan(&channelID, &hash)
	if err != nil {
		return 0, err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return 0, errors.New("Telegram se está reconectando")
	}
	channel := &tg.InputChannel{ChannelID: channelID, AccessHash: hash}
	getPhoto := func() (int64, error) {
		chats, e := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{channel})
		if e != nil {
			return 0, e
		}
		for _, item := range chats.GetChats() {
			if c, ok := item.(*tg.Channel); ok && c.ID == channelID {
				if photo, ok := c.Photo.(*tg.ChatPhoto); ok {
					return photo.PhotoID, nil
				}
				return 0, nil
			}
		}
		return 0, errors.New("no se pudo verificar el cajón de destino")
	}
	existing, err := getPhoto()
	if err != nil || existing != 0 {
		return existing, err
	}
	uploaded, err := uploader.NewUploader(api).FromPath(ctx, path)
	if err != nil {
		return 0, err
	}
	photo := &tg.InputChatUploadedPhoto{}
	photo.SetFile(uploaded)
	if _, err = api.ChannelsEditPhoto(ctx, &tg.ChannelsEditPhotoRequest{Channel: channel, Photo: photo}); err != nil {
		return 0, err
	}
	id, err := getPhoto()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, errors.New("foto enviada; pendiente de verificar en Telegram")
	}
	return id, nil
}
