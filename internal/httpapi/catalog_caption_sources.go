package httpapi

import "context"

type catalogCaptionSource struct{ messageID, fileName, caption string }

// Load and close the result before normalization performs nested queries.
func (a *API) loadCatalogCaptionSources(ctx context.Context, topicID string, selectedIDs []string) ([]catalogCaptionSource, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT m.id::text,COALESCE(mm.file_name,''),COALESCE(m.body,'') FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id WHERE m.topic_id::text=$1 AND m.is_active AND m.media_type='video' ORDER BY m.telegram_message_id`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	selected := make(map[string]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		selected[id] = true
	}
	var sources []catalogCaptionSource
	for rows.Next() {
		var source catalogCaptionSource
		if err := rows.Scan(&source.messageID, &source.fileName, &source.caption); err != nil {
			return nil, err
		}
		if len(selected) == 0 || selected[source.messageID] {
			sources = append(sources, source)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}
