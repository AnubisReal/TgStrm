package telegram

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gotd/td/telegram/updates"
)

type updateStorage struct{ s *Service }

func (p updateStorage) GetState(ctx context.Context, userID int64) (updates.State, bool, error) {
	var state updates.State
	err := p.s.DB.QueryRowContext(ctx, `SELECT pts,qts,update_date,seq FROM tgstrm.telegram_update_states
	 WHERE account_id=$1 AND telegram_user_id=$2`, accountID, userID).Scan(&state.Pts, &state.Qts, &state.Date, &state.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	return state, err == nil, err
}
func (p updateStorage) SetState(ctx context.Context, userID int64, state updates.State) error {
	_, err := p.s.DB.ExecContext(ctx, `INSERT INTO tgstrm.telegram_update_states(account_id,telegram_user_id,pts,qts,update_date,seq)
	 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id) DO UPDATE SET telegram_user_id=EXCLUDED.telegram_user_id,
	 pts=EXCLUDED.pts,qts=EXCLUDED.qts,update_date=EXCLUDED.update_date,seq=EXCLUDED.seq,updated_at=now()`,
		accountID, userID, state.Pts, state.Qts, state.Date, state.Seq)
	return err
}
func (p updateStorage) setField(ctx context.Context, userID int64, field string, values ...any) error {
	args := []any{accountID, userID}
	args = append(args, values...)
	_, err := p.s.DB.ExecContext(ctx, `UPDATE tgstrm.telegram_update_states SET `+field+`,updated_at=now() WHERE account_id=$1 AND telegram_user_id=$2`, args...)
	return err
}
func (p updateStorage) SetPts(ctx context.Context, userID int64, pts int) error {
	return p.setField(ctx, userID, "pts=$3", pts)
}
func (p updateStorage) SetQts(ctx context.Context, userID int64, qts int) error {
	return p.setField(ctx, userID, "qts=$3", qts)
}
func (p updateStorage) SetDate(ctx context.Context, userID int64, date int) error {
	return p.setField(ctx, userID, "update_date=$3", date)
}
func (p updateStorage) SetSeq(ctx context.Context, userID int64, seq int) error {
	return p.setField(ctx, userID, "seq=$3", seq)
}
func (p updateStorage) SetDateSeq(ctx context.Context, userID int64, date, seq int) error {
	return p.setField(ctx, userID, "update_date=$3,seq=$4", date, seq)
}
func (p updateStorage) GetChannelPts(ctx context.Context, userID, channelID int64) (int, bool, error) {
	var pts int
	err := p.s.DB.QueryRowContext(ctx, `SELECT pts FROM tgstrm.telegram_channel_states WHERE account_id=$1 AND telegram_user_id=$2 AND telegram_channel_id=$3 AND pts IS NOT NULL`, accountID, userID, channelID).Scan(&pts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return pts, err == nil, err
}
func (p updateStorage) SetChannelPts(ctx context.Context, userID, channelID int64, pts int) error {
	_, err := p.s.DB.ExecContext(ctx, `INSERT INTO tgstrm.telegram_channel_states(account_id,telegram_user_id,telegram_channel_id,pts) VALUES($1,$2,$3,$4)
	 ON CONFLICT(account_id,telegram_user_id,telegram_channel_id) DO UPDATE SET pts=EXCLUDED.pts,updated_at=now()`, accountID, userID, channelID, pts)
	return err
}
func (p updateStorage) ForEachChannels(ctx context.Context, userID int64, f func(context.Context, int64, int) error) error {
	rows, err := p.s.DB.QueryContext(ctx, `SELECT telegram_channel_id,pts FROM tgstrm.telegram_channel_states WHERE account_id=$1 AND telegram_user_id=$2 AND pts IS NOT NULL`, accountID, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pts int
		if err = rows.Scan(&id, &pts); err != nil {
			return err
		}
		if err = f(ctx, id, pts); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (p updateStorage) SetChannelAccessHash(ctx context.Context, userID, channelID, hash int64) error {
	_, err := p.s.DB.ExecContext(ctx, `INSERT INTO tgstrm.telegram_channel_states(account_id,telegram_user_id,telegram_channel_id,access_hash) VALUES($1,$2,$3,$4)
	 ON CONFLICT(account_id,telegram_user_id,telegram_channel_id) DO UPDATE SET access_hash=EXCLUDED.access_hash,updated_at=now()`, accountID, userID, channelID, hash)
	return err
}
func (p updateStorage) GetChannelAccessHash(ctx context.Context, userID, channelID int64) (int64, bool, error) {
	var hash int64
	err := p.s.DB.QueryRowContext(ctx, `SELECT access_hash FROM tgstrm.telegram_channel_states WHERE account_id=$1 AND telegram_user_id=$2 AND telegram_channel_id=$3 AND access_hash IS NOT NULL`, accountID, userID, channelID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		err = p.s.DB.QueryRowContext(ctx, `SELECT access_hash FROM tgstrm.chats WHERE account_id=$1 AND telegram_peer_type='channel' AND telegram_peer_id=$2 AND access_hash IS NOT NULL`, accountID, channelID).Scan(&hash)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
	}
	return hash, err == nil, err
}
