package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Preserve incorrect generated files in a confined recovery directory. Never
// remove videos, manual files or the recovery copy during normal retirement.
func (a *API) archiveIncorrectEpisodeExports(ctx context.Context, message string, ep episodeIdentity, identified, suspect bool) error {
	if !suspect {
		return nil
	}
	a.libraryFilesystem.Lock()
	defer a.libraryFilesystem.Unlock()
	rows, err := a.db.QueryContext(ctx, `SELECT e.id::text,lt.root_path,e.relative_path,e.content_hash FROM tgstrm.stream_exports e JOIN tgstrm.library_targets lt ON lt.id=e.library_id AND lt.media_type='shows' JOIN tgstrm.stream_assets sa ON sa.id=e.asset_id WHERE sa.message_id::text=$1`, message)
	if err != nil {
		return err
	}
	type item struct {
		id, root, path string
		hash           []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if err = rows.Scan(&it.id, &it.root, &it.path, &it.hash); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, it := range items {
		if strings.HasPrefix(it.path, ".tgstrm-recovery/") {
			continue
		}
		old, ok := detectEpisode(filepath.Base(it.path), "")
		if !ok || (identified && old.Season == ep.Season && old.Episode == ep.Episode && old.lastEpisode() == ep.lastEpisode()) {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(it.path)) || !strings.EqualFold(filepath.Ext(it.path), ".strm") {
			return fmt.Errorf("ruta STRM no válida")
		}
		root, err := os.OpenRoot(it.root)
		if err != nil {
			return err
		}

		backup := filepath.Join(".tgstrm-recovery", "episode-corrections", it.id, filepath.Base(it.path))
		moved, err := archiveOwnedSTRM(root, filepath.FromSlash(it.path), backup, it.hash)
		if err != nil {
			root.Close()
			return err
		}

		if _, err = a.db.ExecContext(ctx, `UPDATE tgstrm.stream_exports SET relative_path=$2,status='missing',last_error='Archivado para corregir identificación de episodio',updated_at=now() WHERE id=$1::uuid`, it.id, filepath.ToSlash(backup)); err != nil {
			var restoreErr error
			if moved {
				restoreErr = root.Rename(backup, filepath.FromSlash(it.path))
			}
			root.Close()
			if restoreErr != nil {
				return fmt.Errorf("guardar archivo recuperable: %w; restaurar ruta: %v", err, restoreErr)
			}
			return err
		}
		root.Close()
	}
	return nil
}

// Resume after a process stopped between Rename and the PostgreSQL acknowledgement.
// A pre-existing recovery file is accepted only when its contents match exactly.
func archiveOwnedSTRM(root *os.Root, relative, backup string, expected []byte) (bool, error) {
	if !filepath.IsLocal(relative) || !filepath.IsLocal(backup) || !strings.EqualFold(filepath.Ext(relative), ".strm") {
		return false, fmt.Errorf("ruta STRM no válida")
	}
	content, err := root.ReadFile(relative)
	if os.IsNotExist(err) {
		content, err = root.ReadFile(backup)
		if err != nil {
			return false, err
		}
		hash := sha256.Sum256(content)
		if !bytes.Equal(hash[:], expected) {
			return false, fmt.Errorf("la copia recuperable cambió; se conserva")
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(content)
	if !bytes.Equal(hash[:], expected) {
		return false, fmt.Errorf("el STRM cambió manualmente; no se archivará")
	}
	if _, err = root.Lstat(backup); !os.IsNotExist(err) {
		return false, fmt.Errorf("el destino de recuperación ya existe o no se puede comprobar")
	}
	if err = root.MkdirAll(filepath.Dir(backup), 0700); err != nil {
		return false, err
	}
	if err = root.Rename(relative, backup); err != nil {
		return false, err
	}
	return true, nil
}
