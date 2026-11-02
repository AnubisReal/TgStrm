package telegram

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotd/td/tg"
)

// Defaults and configuration keys are defined by Telegram's client config:
// https://core.telegram.org/api/config. Server-side validation remains final.
type collectionLimits struct{ folders, chats int }

func parseCollectionLimits(config tg.JSONValueClass, premium bool) collectionLimits {
	limits := collectionLimits{folders: 10, chats: 100}
	suffix := "_default"
	if premium {
		limits = collectionLimits{folders: 30, chats: 200}
		suffix = "_premium"
	}
	object, ok := config.(*tg.JSONObject)
	if !ok {
		return limits
	}
	for _, entry := range object.Value {
		number, ok := entry.Value.(*tg.JSONNumber)
		if !ok || number.Value < 1 || number.Value > 100000 || math.Trunc(number.Value) != number.Value {
			continue
		}
		switch entry.Key {
		case "dialog_filters_limit" + suffix:
			limits.folders = int(number.Value)
		case "dialog_filters_chats_limit" + suffix:
			limits.chats = int(number.Value)
		}
	}
	return limits
}

func loadCollectionLimits(ctx context.Context, api *tg.Client) (collectionLimits, error) {
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
	if err != nil {
		return collectionLimits{}, err
	}
	var self *tg.User
	for _, item := range users {
		if user, ok := item.(*tg.User); ok && user.Self {
			self = user
			break
		}
	}
	if self == nil {
		return collectionLimits{}, errors.New("no se pudo comprobar el límite de carpetas de la cuenta")
	}
	config, err := api.HelpGetAppConfig(ctx, 0)
	if err != nil {
		// Do not swallow FLOOD_WAIT or network failure. A later queue retry obtains
		// the account configuration rather than mutating with uncertain limits.
		return collectionLimits{}, err
	}
	if value, ok := config.(*tg.HelpAppConfig); ok {
		return parseCollectionLimits(value.Config, self.Premium), nil
	}
	return parseCollectionLimits(nil, self.Premium), nil
}

func (limits collectionLimits) validate(newFolder bool, existingFolders, chats int) error {
	if newFolder && existingFolders >= limits.folders {
		return fmt.Errorf("Telegram admite %d carpetas en esta cuenta; libera una carpeta o selecciona una existente", limits.folders)
	}
	if chats > limits.chats {
		return fmt.Errorf("la carpeta supera el límite de %d chats de esta cuenta; divide la biblioteca en varias carpetas", limits.chats)
	}
	return nil
}

func collectionPeerCount(filter *tg.DialogFilter) int {
	unique := map[string]struct{}{}
	for _, peers := range [][]tg.InputPeerClass{filter.IncludePeers, filter.PinnedPeers} {
		for _, item := range peers {
			switch peer := item.(type) {
			case *tg.InputPeerChannel:
				unique[fmt.Sprintf("channel:%d", peer.ChannelID)] = struct{}{}
			case *tg.InputPeerChat:
				unique[fmt.Sprintf("chat:%d", peer.ChatID)] = struct{}{}
			case *tg.InputPeerUser:
				unique[fmt.Sprintf("user:%d", peer.UserID)] = struct{}{}
			}
		}
	}
	return len(unique)
}
