package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const CatalogAccountID = accountID

type TelegramFolder struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Shared bool   `json:"shared"`
}

func (s *Service) LibraryFolders(ctx context.Context) ([]TelegramFolder, error) {
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return nil, errors.New("Telegram se está reconectando")
	}
	filters, err := api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return nil, err
	}
	result := []TelegramFolder{}
	for _, item := range filters.Filters {
		switch filter := item.(type) {
		case *tg.DialogFilter:
			result = append(result, TelegramFolder{ID: filter.ID, Title: filter.Title.Text})
		case *tg.DialogFilterChatlist:
			result = append(result, TelegramFolder{ID: filter.ID, Title: filter.Title.Text, Shared: true})
		}
	}
	return result, nil
}

// EnsureCollectionFolder merges managed groups into the real folder. Existing
// pins, filter flags and unrelated chats are preserved. Shared folders are not
// silently converted to ordinary folders.
func (s *Service) EnsureCollectionFolder(ctx context.Context, collectionID string) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	var name, state string
	var folderID sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT name,telegram_filter_id,state FROM tgstrm.library_collections WHERE id::text=$1 AND account_id=$2 AND state<>'archived'`, collectionID, accountID).Scan(&name, &folderID, &state); err != nil {
		return err
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	filters, err := api.MessagesGetDialogFilters(ctx)
	if err != nil {
		return err
	}
	var filter *tg.DialogFilter
	existingFolders := 0
	used := map[int]bool{0: true, 1: true}
	for _, item := range filters.Filters {
		switch value := item.(type) {
		case *tg.DialogFilter:
			existingFolders++
			used[value.ID] = true
			if folderID.Valid && int(folderID.Int64) == value.ID {
				clone := *value
				clone.IncludePeers = append([]tg.InputPeerClass(nil), value.IncludePeers...)
				filter = &clone
			}
		case *tg.DialogFilterChatlist:
			existingFolders++
			used[value.ID] = true
			if folderID.Valid && int(folderID.Int64) == value.ID {
				return errors.New("la carpeta compartida requiere una carpeta propia de destino")
			}
		}
	}
	if state == "draft" && filter != nil && filter.Title.Text != name {
		return errors.New("el identificador reservado de la carpeta está ocupado; revisa la asociación antes de continuar")
	}
	reserved, err := s.DB.QueryContext(ctx, `SELECT telegram_filter_id FROM tgstrm.library_collections WHERE telegram_filter_id IS NOT NULL AND id::text<>$1 AND state<>'archived'`, collectionID)
	if err != nil {
		return err
	}
	for reserved.Next() {
		var id int
		if err = reserved.Scan(&id); err != nil {
			reserved.Close()
			return err
		}
		used[id] = true
	}
	err = reserved.Err()
	reserved.Close()
	if err != nil {
		return err
	}
	if folderID.Valid && filter == nil && state != "draft" {
		return errors.New("la carpeta Telegram fue eliminada; revisa su asociación antes de continuar")
	}
	newFolder := filter == nil
	if filter == nil {
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 12 {
			return errors.New("el nombre de la carpeta Telegram admite hasta 12 caracteres")
		}
		next := 2
		if folderID.Valid {
			next = int(folderID.Int64)
		}
		for used[next] {
			next++
		}
		filter = &tg.DialogFilter{ID: next, Title: tg.TextWithEntities{Text: name}, IncludePeers: []tg.InputPeerClass{}, PinnedPeers: []tg.InputPeerClass{}, ExcludePeers: []tg.InputPeerClass{}}
	}
	previousIncludeCount := len(filter.IncludePeers)
	rows, err := s.DB.QueryContext(ctx, `SELECT c.telegram_peer_id,COALESCE(c.access_hash,0) FROM tgstrm.collection_groups cg JOIN tgstrm.chats c ON c.id=cg.destination_chat_id WHERE cg.collection_id::text=$1 AND cg.account_id=$2 AND cg.state='ready' AND c.is_active ORDER BY c.id`, collectionID, accountID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash int64
		if err = rows.Scan(&id, &hash); err != nil {
			return err
		}
		if hash == 0 {
			return errors.New("el cajón no tiene acceso Telegram disponible")
		}
		for _, excluded := range filter.ExcludePeers {
			if peer, ok := excluded.(*tg.InputPeerChannel); ok && peer.ChannelID == id {
				return errors.New("el cajón está excluido manualmente de la carpeta Telegram")
			}
		}
		found := false
		for _, peers := range [][]tg.InputPeerClass{filter.IncludePeers, filter.PinnedPeers} {
			for _, item := range peers {
				if peer, ok := item.(*tg.InputPeerChannel); ok && peer.ChannelID == id {
					found = true
				}
			}
		}
		if !found {
			filter.IncludePeers = append(filter.IncludePeers, &tg.InputPeerChannel{ChannelID: id, AccessHash: hash})
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(filter.IncludePeers) == 0 && len(filter.PinnedPeers) == 0 {
		return errors.New("la carpeta se creará en Telegram al añadir el primer cajón válido")
	}
	if !newFolder && previousIncludeCount == len(filter.IncludePeers) {
		// Existing folder membership already satisfies the desired state. Avoid
		// reloading limits and rewriting the folder for every topic in a batch.
		_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.library_collections SET state='ready',updated_at=now() WHERE id::text=$1 AND state<>'ready'`, collectionID)
		return err
	}
	limits, err := loadCollectionLimits(ctx, api)
	if err != nil {
		return err
	}
	if err = limits.validate(newFolder, existingFolders, collectionPeerCount(filter)); err != nil {
		return err
	}
	// Persist the reserved ID before the remote mutation. A restart reconciles
	// this identity; it cannot create another folder after an ambiguous response.
	if !folderID.Valid {
		result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.library_collections SET telegram_filter_id=$2,updated_at=now() WHERE id::text=$1 AND telegram_filter_id IS NULL`, collectionID, filter.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return errors.New("la carpeta cambió durante la preparación")
		}
	}
	req := &tg.MessagesUpdateDialogFilterRequest{ID: filter.ID}
	req.SetFilter(filter)
	accepted, err := api.MessagesUpdateDialogFilter(ctx, req)
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("Telegram no confirmó la carpeta")
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.library_collections SET state='ready',updated_at=now() WHERE id::text=$1`, collectionID)
	return err
}

// CreateReplicationGroup is intentionally not retried after an uncertain create:
// channels.createChannel has no random_id. The saved 'creating' state requires
// explicit recovery, preventing duplicate groups after process interruption.
func (s *Service) CreateReplicationGroup(ctx context.Context, replicationID string) (string, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	var title, status, destination string
	err := s.DB.QueryRowContext(ctx, `SELECT destination_title,status,COALESCE(destination_chat_id::text,'') FROM tgstrm.group_replications WHERE id::text=$1 AND account_id=$2`, replicationID, accountID).Scan(&title, &status, &destination)
	if err != nil {
		return "", err
	}
	if destination != "" {
		return destination, nil
	}
	if status == "creating" {
		return "", errors.New("creación de cajón pendiente de reconciliar; no se repetirá automáticamente")
	}
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > 128 {
		return "", errors.New("nombre de cajón inválido")
	}
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return "", errors.New("Telegram se está reconectando")
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE tgstrm.group_replications SET status='creating',updated_at=now() WHERE id::text=$1 AND destination_chat_id IS NULL AND status IN ('review','running')`, replicationID)
	if err != nil {
		return "", err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return "", errors.New("el cajón cambió durante la creación")
	}
	updates, err := api.ChannelsCreateChannel(ctx, &tg.ChannelsCreateChannelRequest{Megagroup: true, Forum: true, Title: title, About: "Biblioteca TgStrm · " + replicationID})
	if err != nil {
		var definite *tgerr.Error
		if errors.As(err, &definite) {
			_, _ = s.DB.ExecContext(ctx, `UPDATE tgstrm.group_replications SET status='running' WHERE id::text=$1 AND status='creating'`, replicationID)
		}
		return "", fmt.Errorf("crear cajón: %w", err)
	}
	var chats []tg.ChatClass
	switch value := updates.(type) {
	case *tg.Updates:
		chats = value.Chats
	case *tg.UpdatesCombined:
		chats = value.Chats
	}
	for _, item := range chats {
		channel, ok := item.(*tg.Channel)
		if !ok || !channel.Creator || !channel.Forum {
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		err = tx.QueryRowContext(ctx, `INSERT INTO tgstrm.chats(account_id,telegram_peer_type,telegram_peer_id,access_hash,title,is_forum) VALUES($1,'channel',$2,$3,$4,true) ON CONFLICT(account_id,telegram_peer_type,telegram_peer_id) DO UPDATE SET access_hash=EXCLUDED.access_hash,title=EXCLUDED.title,is_active=true,is_forum=true RETURNING id::text`, accountID, channel.ID, channel.AccessHash, channel.Title).Scan(&destination)
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tgstrm.group_replications SET destination_chat_id=$2::bigint,status='running',updated_at=now() WHERE id::text=$1`, replicationID, destination)
		if err != nil {
			return "", err
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return destination, nil
	}
	return "", errors.New("Telegram aceptó la creación pero falta reconciliar su identidad")
}

func (s *Service) RenameCollectionGroup(ctx context.Context, groupID, previous, desired string) error {
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("Telegram se está reconectando")
	}
	var id, hash int64
	if err := s.DB.QueryRowContext(ctx, `SELECT telegram_peer_id,COALESCE(access_hash,0) FROM tgstrm.managed_catalog_chats WHERE id::text=$1 AND account_id=$2 AND is_active`, groupID, accountID).Scan(&id, &hash); err != nil {
		return err
	}
	full, err := api.ChannelsGetChannels(ctx, []tg.InputChannelClass{&tg.InputChannel{ChannelID: id, AccessHash: hash}})
	if err != nil {
		return err
	}
	if len(full.GetChats()) != 1 {
		return errors.New("no se pudo verificar el nombre actual del cajón")
	}
	channel, ok := full.GetChats()[0].(*tg.Channel)
	if !ok || !channel.Creator {
		return errors.New("se requiere un cajón propio para renombrarlo")
	}
	if channel.Title != desired && channel.Title != previous {
		return errors.New("el nombre cambió en Telegram; revisa el conflicto")
	}
	if channel.Title != desired {
		if _, err = api.ChannelsEditTitle(ctx, &tg.ChannelsEditTitleRequest{Channel: &tg.InputChannel{ChannelID: id, AccessHash: hash}, Title: desired}); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tgstrm.chats SET title=$2,updated_at=now() WHERE id::text=$1`, groupID, desired)
	return err
}
