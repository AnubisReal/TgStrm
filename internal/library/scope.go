package library

import (
	"context"
	"database/sql"
	"errors"
)

// Scope separates the destination for new operations from managed groups whose
// existing jobs must continue even when the selected collection changes.
type Scope struct {
	Mode         string `json:"mode"`
	GroupID      string `json:"group_id,omitempty"`
	CollectionID string `json:"collection_id,omitempty"`
	Revision     int64  `json:"revision"`
}

type ScopeDatabase interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type Resolver struct{ DB ScopeDatabase }

func (r Resolver) Active(ctx context.Context, accountID string) (Scope, error) {
	var scope Scope
	err := r.DB.QueryRowContext(ctx, `SELECT mode,COALESCE(group_chat_id::text,''),COALESCE(collection_id::text,''),revision
 FROM tgstrm.catalog_targets WHERE account_id=$1`, accountID).Scan(&scope.Mode, &scope.GroupID, &scope.CollectionID, &scope.Revision)
	if !errors.Is(err, sql.ErrNoRows) {
		return scope, err
	}
	// Legacy settings stay authoritative until an explicit collection target exists.
	scope.Mode = "group"
	err = r.DB.QueryRowContext(ctx, `SELECT c.id::text,s.revision FROM tgstrm.settings s
 JOIN tgstrm.chats c ON c.id::text=(s.value #>> '{}') AND c.account_id=$1
 WHERE s.key='active_chat_id'`, accountID).Scan(&scope.GroupID, &scope.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, nil
	}
	return scope, err
}

func (r Resolver) Contains(ctx context.Context, accountID, groupID string) (bool, error) {
	var managed bool
	err := r.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tgstrm.managed_catalog_chats WHERE account_id=$1 AND id::text=$2)`, accountID, groupID).Scan(&managed)
	return managed, err
}

// DestinationGroups resolves a confirmed scope without redirecting it to a newly
// selected destination. Callers persist this scope in their job manifest.
func (r Resolver) DestinationGroups(ctx context.Context, accountID string, scope Scope) ([]string, error) {
	if scope.Mode != "group" && scope.Mode != "collection" {
		return nil, errors.New("invalid library scope")
	}
	if scope.Mode == "group" && (scope.GroupID == "" || scope.CollectionID != "") {
		return nil, errors.New("group target unavailable")
	}
	if scope.Mode == "collection" && (scope.CollectionID == "" || scope.GroupID != "") {
		return nil, errors.New("invalid collection target")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT c.id::text FROM tgstrm.chats c
 WHERE c.account_id=$1 AND c.is_active AND c.is_forum AND (
 ($2='group' AND c.id::text=$3) OR ($2='collection' AND EXISTS(
 SELECT 1 FROM tgstrm.collection_groups cg JOIN tgstrm.library_collections lc ON lc.id=cg.collection_id
 WHERE cg.destination_chat_id=c.id AND cg.account_id=$1 AND cg.collection_id::text=$4
 AND cg.state='ready' AND lc.state='ready')))
 ORDER BY c.id`, accountID, scope.Mode, scope.GroupID, scope.CollectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		groups = append(groups, id)
	}
	return groups, rows.Err()
}
