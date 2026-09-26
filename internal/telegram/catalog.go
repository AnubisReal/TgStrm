package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type Group struct {
	ID         string `json:"id"`
	TelegramID string `json:"telegram_id"`
	Title      string `json:"title"`
	Username   string `json:"username,omitempty"`
	Type       string `json:"type"`
	Forum      bool   `json:"forum"`
	Active     bool   `json:"active"`
}

type remoteGroup struct {
	peerType        string
	id, accessHash  int64
	title, username string
	forum           bool
}

func (s *Service) syncDialogs(ctx context.Context, api *tg.Client) error {
	groups := make([]remoteGroup, 0, 64)
	err := query.GetDialogs(api).BatchSize(100).ForEach(ctx, func(_ context.Context, elem dialogs.Elem) error {
		switch peer := elem.Peer.(type) {
		case *tg.InputPeerChannel:
			if channel, ok := elem.Entities.Channel(peer.ChannelID); ok && !channel.Left {
				groups = append(groups, remoteGroup{"channel", channel.ID, channel.AccessHash, channel.Title, channel.Username, channel.Forum})
			}
		case *tg.InputPeerChat:
			if chat, ok := elem.Entities.Chat(peer.ChatID); ok && !chat.Left && !chat.Deactivated {
				if _, migrated := chat.GetMigratedTo(); migrated {
					return nil
				}
				groups = append(groups, remoteGroup{peerType: "chat", id: chat.ID, title: chat.Title})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE tgstrm.chats SET is_active=false,updated_at=now() WHERE account_id=$1", accountID); err != nil {
		return err
	}
	for _, g := range groups {
		_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.chats(account_id,telegram_peer_type,telegram_peer_id,access_hash,title,username,is_forum)
		 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7) ON CONFLICT(account_id,telegram_peer_type,telegram_peer_id) DO UPDATE SET
		 access_hash=EXCLUDED.access_hash,title=EXCLUDED.title,username=EXCLUDED.username,is_forum=EXCLUDED.is_forum,is_active=true,last_seen_at=now(),updated_at=now()`, accountID, g.peerType, g.id, g.accessHash, g.title, g.username, g.forum)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.sync_states(account_id,scope,status,completed_at) VALUES($1,'dialogs','ready',now())
	 ON CONFLICT(account_id,scope,resource_id) DO UPDATE SET status='ready',last_error=NULL,completed_at=now(),updated_at=now()`, accountID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RefreshDialogs performs the same authoritative dialog reconciliation used by
// the background service. It is exposed for an explicit remote-search action:
// a result may come from a group that has never been opened in the web UI yet.
func (s *Service) RefreshDialogs(ctx context.Context) error {
	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return errors.New("telegram is reconnecting")
	}
	return s.syncDialogs(ctx, api)
}

// IndexRemoteTopic makes a remote search result part of the local read model.
// It never forwards or writes a Telegram message: it only resolves the known
// forum topic and synchronizes its metadata/media through the normal idempotent
// catalog path. Importing or exporting remains an explicit separate action.
func (s *Service) IndexRemoteTopic(ctx context.Context, peerType string, peerID int64, telegramTopicID int) (string, string, error) {
	if peerType != "channel" || peerID <= 0 || telegramTopicID <= 0 {
		return "", "", errors.New("el resultado no pertenece a un topic indexable")
	}
	var groupID string
	err := s.DB.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.chats
		WHERE account_id=$1 AND telegram_peer_type=$2 AND telegram_peer_id=$3 AND is_active`, accountID, peerType, peerID).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		refreshErr := s.RefreshDialogs(refreshCtx)
		cancel()
		if refreshErr != nil {
			return "", "", refreshErr
		}
		err = s.DB.QueryRowContext(ctx, `SELECT id::text FROM tgstrm.chats
			WHERE account_id=$1 AND telegram_peer_type=$2 AND telegram_peer_id=$3 AND is_active`, accountID, peerType, peerID).Scan(&groupID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", errors.New("el grupo remoto no está disponible en esta cuenta")
		}
		return "", "", err
	}
	if err = s.RefreshTopics(ctx, groupID); err != nil {
		return "", "", err
	}
	var topicID string
	err = s.DB.QueryRowContext(ctx, `SELECT t.id::text FROM tgstrm.topics t
		WHERE t.chat_id::text=$1 AND t.telegram_topic_id=$2 AND t.is_active`, groupID, telegramTopicID).Scan(&topicID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errors.New("el topic remoto ya no está disponible")
	}
	if err != nil {
		return "", "", err
	}
	if err = s.SyncTopicVideoCatalog(ctx, groupID, topicID); err != nil {
		return "", "", err
	}
	return groupID, topicID, nil
}

func (s *Service) Groups(ctx context.Context) ([]Group, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,telegram_peer_id::text,title,COALESCE(username,''),telegram_peer_type,is_forum
	 FROM tgstrm.chats WHERE account_id=$1 AND is_active ORDER BY lower(title),id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	activeID := ""
	var value []byte
	if err = s.DB.QueryRowContext(ctx, "SELECT value FROM tgstrm.settings WHERE key='active_chat_id'").Scan(&value); err == nil {
		_ = json.Unmarshal(value, &activeID)
	}
	result := []Group{}
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.ID, &g.TelegramID, &g.Title, &g.Username, &g.Type, &g.Forum); err != nil {
			return nil, err
		}
		g.Active = g.ID == activeID
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *Service) SelectGroup(ctx context.Context, id string) error {
	var exists bool
	if err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tgstrm.chats WHERE id::text=$1 AND account_id=$2 AND is_active)", id, accountID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	raw, _ := json.Marshal(id)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO tgstrm.settings(key,value) VALUES('active_chat_id',$1) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,revision=tgstrm.settings.revision+1,updated_at=now()`, raw); err != nil {
		return fmt.Errorf("save active group: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE tgstrm.topics SET library_kind=CASE
		WHEN lower(trim(title)) IN ('peliculas','películas','movies','films') THEN 'movie_collection'
		WHEN lower(trim(title))='general' THEN 'ignored' ELSE 'series' END,
		classification_source='automatic',classified_at=now(),updated_at=now()
		WHERE chat_id::text=$1 AND classification_source<>'manual'`, id)
	if err != nil {
		return fmt.Errorf("classify active group: %w", err)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM tgstrm.tmdb_topic_scan_states ss USING tgstrm.topics t
		WHERE ss.topic_id=t.id AND t.chat_id::text=$1 AND t.library_kind='series'
		AND NOT EXISTS (SELECT 1 FROM tgstrm.tmdb_matches tm WHERE tm.topic_id=t.id AND tm.message_id IS NULL)`, id)
	if err != nil {
		return fmt.Errorf("reset series enrichment: %w", err)
	}
	return tx.Commit()
}
