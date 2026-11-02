package telegram

import "context"

// Export state is derived from existing records; it never changes the library.
const exportStateColumns = `COALESCE(NULLIF(sa.last_export_error,''),latest.last_error,''),
 CASE WHEN NULLIF(sa.last_export_error,'') IS NOT NULL OR latest.status='error' THEN 'error'
 WHEN latest.status='written' AND sa.processed_signature=sa.source_signature THEN 'written' ELSE 'pending' END`

const latestExportJoin = `LEFT JOIN LATERAL (SELECT e.status,e.last_error FROM tgstrm.stream_exports e
 WHERE e.asset_id=sa.id ORDER BY e.updated_at DESC,e.id DESC LIMIT 1) latest ON true`

type TopicExportState struct {
	ID           string `json:"id"`
	ExportError  string `json:"export_error,omitempty"`
	ExportStatus string `json:"export_status"`
}

func (s *Service) TopicExportStates(ctx context.Context, groupID, topicID string) ([]TopicExportState, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id::text,`+exportStateColumns+`
 FROM tgstrm.messages m JOIN tgstrm.topics t ON t.id=m.topic_id
 JOIN tgstrm.chats c ON c.id=t.chat_id LEFT JOIN tgstrm.stream_assets sa ON sa.message_id=m.id
 `+latestExportJoin+`
 WHERE c.id::text=$1 AND t.id::text=$2 AND c.account_id=$3
 AND c.is_active AND t.is_active AND m.is_active AND m.media_type='video'`, groupID, topicID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]TopicExportState, 0)
	for rows.Next() {
		var state TopicExportState
		if err = rows.Scan(&state.ID, &state.ExportError, &state.ExportStatus); err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, rows.Err()
}
