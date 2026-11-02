package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Remove only a registered, unchanged STRM after its successor is on disk.
// os.Root prevents a linked intermediate directory from escaping the library.
func removeRetiredSTRM(rootPath, relative string, expected []byte) error {
	if filepath.IsAbs(relative) || !filepath.IsLocal(filepath.FromSlash(relative)) || !strings.EqualFold(filepath.Ext(relative), ".strm") {
		return errors.New("ruta antigua no válida")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	content, err := root.ReadFile(filepath.FromSlash(relative))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	hash := sha256.Sum256(content)
	if !bytes.Equal(hash[:], expected) {
		return errors.New("el STRM antiguo cambió; no se borrará")
	}
	if err = root.Remove(filepath.FromSlash(relative)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for dir := filepath.Dir(filepath.FromSlash(relative)); dir != "."; dir = filepath.Dir(dir) {
		for _, name := range []string{".DS_Store", "Thumbs.db"} {
			_ = root.Remove(filepath.Join(dir, name))
		}
		if err := root.Remove(dir); err != nil {
			break
		}
	}
	return nil
}

func (a *API) retireExportPaths(ctx context.Context) {
	a.libraryFilesystem.Lock()
	defer a.libraryFilesystem.Unlock()
	rows, err := a.db.QueryContext(ctx, `SELECT rp.library_id::text,rp.asset_id::text,lt.root_path,rp.relative_path,rp.content_hash,e.relative_path,e.content_hash
 FROM tgstrm.retired_export_paths rp JOIN tgstrm.library_targets lt ON lt.id=rp.library_id
 JOIN tgstrm.stream_exports e ON e.library_id=rp.library_id AND e.asset_id=rp.asset_id AND e.status='written'
 WHERE rp.relative_path NOT LIKE '.tgstrm-recovery/%' AND rp.relative_path<>e.relative_path AND NOT EXISTS(SELECT 1 FROM tgstrm.stream_exports owned JOIN tgstrm.library_targets olt ON olt.id=owned.library_id WHERE olt.root_path=lt.root_path AND owned.relative_path=rp.relative_path)
 ORDER BY rp.created_at LIMIT 50`)
	if err != nil {
		return
	}
	type item struct {
		library, asset, root, old, new string
		oldHash, newHash               []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.library, &it.asset, &it.root, &it.old, &it.oldHash, &it.new, &it.newHash) != nil {
			rows.Close()
			return
		}
		items = append(items, it)
	}
	if rows.Err() != nil {
		rows.Close()
		return
	}
	rows.Close()
	for _, it := range items {
		root, err := os.OpenRoot(it.root)
		if err != nil {
			continue
		}
		content, err := root.ReadFile(filepath.FromSlash(it.new))
		root.Close()
		if err != nil {
			continue
		}
		hash := sha256.Sum256(content)
		if !bytes.Equal(hash[:], it.newHash) {
			continue
		}
		if removeRetiredSTRM(it.root, it.old, it.oldHash) != nil {
			continue
		}
		_, _ = a.db.ExecContext(ctx, `DELETE FROM tgstrm.retired_export_paths WHERE library_id::text=$1 AND asset_id::text=$2 AND relative_path=$3`, it.library, it.asset, it.old)
	}
}
