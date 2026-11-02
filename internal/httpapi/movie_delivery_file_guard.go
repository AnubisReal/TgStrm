package httpapi

import (
	"context"
	"database/sql"
	"errors"
)

var errMovieFileReserved = errors.New("el mismo archivo tiene otro envío en curso; se esperará a su resultado")

var errMovieFileIdentityChanged = errors.New("la identidad del archivo cambió después de reservar su envío")

type movieGuardDB interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const movieExistingFileSQL = `SELECT EXISTS(SELECT 1 FROM tgstrm.messages m
 JOIN tgstrm.message_media media ON media.message_id=m.id
 WHERE m.chat_id::text=$1 AND m.topic_id::text=$2 AND m.is_active AND m.media_type='video'
 AND media.telegram_file_id=(SELECT telegram_file_id FROM tgstrm.message_media WHERE message_id::text=$3))`

// Called before presentation, under the worker delivery lock. A document can
// belong to only one delivery even if two monitors discover different messages.
func reserveMovieDeliveryFile(ctx context.Context, db movieGuardDB, d movieDelivery) (bool, error) {
	var file int64
	if err := db.QueryRowContext(ctx, `SELECT telegram_file_id FROM tgstrm.message_media WHERE message_id::text=$1 AND telegram_file_id>0`, d.source).Scan(&file); err != nil {
		return false, err
	}
	var err error
	// Existing content (including direct uploads) is sufficient proof before a
	// new delivery starts. In-flight stages retain their original acceptance flow.
	if d.stage == "ready" {
		var existing bool
		if err = db.QueryRowContext(ctx, movieExistingFileSQL, d.group, d.topic, d.source).Scan(&existing); err != nil {
			return false, err
		}
		if existing {
			return false, nil
		}
	}
	var ownChat, ownTopic string
	var ownFile int64
	err = db.QueryRowContext(ctx, `SELECT destination_chat_id::text,destination_topic_id::text,telegram_file_id FROM tgstrm.movie_delivery_files WHERE delivery_id::text=$1`, d.id).Scan(&ownChat, &ownTopic, &ownFile)
	if err == nil {
		if ownChat != d.group || ownTopic != d.topic || ownFile != file {
			return false, errMovieFileIdentityChanged
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var owner string
	err = db.QueryRowContext(ctx, `INSERT INTO tgstrm.movie_delivery_files(destination_chat_id,destination_topic_id,telegram_file_id,delivery_id)
 VALUES($1::bigint,$2::bigint,$3,$4::uuid)
 ON CONFLICT(destination_chat_id,destination_topic_id,telegram_file_id) DO UPDATE SET delivery_id=EXCLUDED.delivery_id
 WHERE movie_delivery_files.delivery_id=EXCLUDED.delivery_id OR EXISTS(SELECT 1 FROM tgstrm.movie_monitor_deliveries old
 JOIN tgstrm.messages target ON target.id=old.destination_message_id
 WHERE old.id=movie_delivery_files.delivery_id AND old.status='completed' AND NOT target.is_active)
 RETURNING delivery_id::text`, d.group, d.topic, file, d.id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		// A reservation is not proof of a successful remote send. Retry rather
		// than dropping this source if its owner later fails or disappears.
		return false, errMovieFileReserved
	}
	return owner == d.id, err
}

func (a *API) guardMovieDeliveryFile(ctx context.Context, d movieDelivery) bool {
	allowed, err := reserveMovieDeliveryFile(ctx, a.db, d)
	if err != nil {
		if errors.Is(err, errMovieFileIdentityChanged) || errors.Is(err, errMovieFileReserved) && d.stage != "ready" {
			a.finishMovieStage(ctx, d, "review", err.Error())
		} else {
			a.failMovieDelivery(ctx, d, err)
		}
		return false
	}
	if !allowed {
		a.finishMovieStage(ctx, d, "excluded", "Archivo idéntico ya presente o reservado en este destino.")
		return false
	}
	return true
}
