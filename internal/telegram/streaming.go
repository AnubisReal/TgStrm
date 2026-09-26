package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const StreamChunkSize int64 = 1024 * 1024

// InitialStreamChunkSize favors a fast first byte. Once the response has
// started, subsequent reads use Telegram's full 1 MiB part size.
const InitialStreamChunkSize int64 = 256 * 1024

// StreamCacheBlockSize is deliberately stable across HTTP clients. Players
// often probe the same MP4 bytes using slightly different Range boundaries;
// caching caller-shaped ranges would miss in that common case.
const StreamCacheBlockSize int64 = InitialStreamChunkSize

// PersistentStreamHeadSize keeps only the first MiB needed by players for
// format probing and initial playback. Ordinary playback remains ephemeral;
// MP4 indexes are persisted explicitly by the index preparer.
const PersistentStreamHeadSize int64 = 1024 * 1024

const (
	streamCacheLimit = int64(64 * 1024 * 1024)
	streamCacheTTL   = 5 * time.Minute
)

type streamCacheEntry struct {
	data      []byte
	expiresAt time.Time
	usedAt    time.Time
}

type streamCall struct {
	done chan struct{}
	data []byte
	err  error
}

type VideoFileLocation struct {
	VideoID       string
	FileName      string
	MIMEType      string
	ByteSize      int64
	DocumentID    int64
	AccessHash    int64
	FileReference []byte
	DCID          int
	GroupID       string
	TopicID       string
	MessageID     int
	ChannelID     int64
	ChannelHash   int64
}

// warmStreamRoute pays the one-time media-DC connection cost before a player
// opens an STRM. Failure is harmless: the normal request path remains the
// source of truth and will retry when a stream is requested.
func (s *Service) warmStreamRoute(parent context.Context) {
	var videoID string
	err := s.DB.QueryRowContext(parent, `SELECT m.id::text
		FROM tgstrm.messages m
		JOIN tgstrm.message_media mm ON mm.message_id=m.id
		JOIN tgstrm.topics t ON t.id=m.topic_id
		JOIN tgstrm.chats c ON c.id=m.chat_id
		WHERE c.account_id=$1 AND m.is_active AND m.media_type='video' AND t.is_active AND c.is_active
		  AND COALESCE(mm.byte_size,0)>0 AND COALESCE(mm.telegram_file_id,0)<>0
		ORDER BY m.updated_at DESC LIMIT 1`, accountID).Scan(&videoID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && parent.Err() == nil {
			log.Printf("warm telegram stream route: %v", err)
		}
		return
	}
	location, err := s.VideoLocation(parent, videoID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	started := time.Now()
	if _, err = s.mediaAPI(ctx, location.DCID); err != nil {
		if parent.Err() == nil {
			log.Printf("warm Telegram media pool video=%s: %v", videoID, err)
		}
		return
	}
	type warmResult struct{ err error }
	results := make(chan warmResult, 2)
	ranges := [][2]int64{
		{0, min(location.ByteSize-1, StreamCacheBlockSize-1)},
		{(location.ByteSize - 1) / StreamCacheBlockSize * StreamCacheBlockSize, location.ByteSize - 1},
	}
	for _, byteRange := range ranges {
		byteRange := byteRange
		go func() {
			_, downloadErr := s.DownloadVideoRange(ctx, location, byteRange[0], byteRange[1])
			results <- warmResult{err: downloadErr}
		}()
	}
	for range ranges {
		if result := <-results; result.err != nil && err == nil {
			err = result.err
		}
	}
	if err != nil {
		if parent.Err() == nil {
			log.Printf("warm telegram stream route video=%s: %v", videoID, err)
		}
		return
	}
	log.Printf("Telegram media route ready in %s", time.Since(started).Round(time.Millisecond))
	s.PrepareVideoIndex(location)
}

func (s *Service) VideoLocation(ctx context.Context, videoID string) (VideoFileLocation, error) {
	return s.videoLocation(ctx, `m.id::text=$1`, videoID)
}

func (s *Service) PublicVideoLocation(ctx context.Context, assetID string) (VideoFileLocation, error) {
	return s.videoLocation(ctx, `EXISTS(SELECT 1 FROM tgstrm.stream_assets sa WHERE sa.message_id=m.id AND sa.id::text=$1)`, assetID)
}

func (s *Service) videoLocation(ctx context.Context, predicate, identity string) (VideoFileLocation, error) {
	var result VideoFileLocation
	query := `SELECT m.id::text,COALESCE(mm.file_name,''),COALESCE(mm.mime_type,'application/octet-stream'),
	 COALESCE(mm.byte_size,0),COALESCE(mm.telegram_file_id,0),COALESCE(mm.access_hash,0),COALESCE(mm.file_reference,''::bytea),
	 COALESCE(mm.telegram_dc_id,0),c.id::text,t.id::text,m.telegram_message_id,c.telegram_peer_id,COALESCE(c.access_hash,0)
	 FROM tgstrm.messages m JOIN tgstrm.message_media mm ON mm.message_id=m.id
	 JOIN tgstrm.topics t ON t.id=m.topic_id JOIN tgstrm.chats c ON c.id=m.chat_id
	 WHERE ` + predicate + ` AND m.is_active AND m.media_type='video' AND t.is_active AND c.is_active AND c.account_id=$2`
	err := s.DB.QueryRowContext(ctx, query, identity, accountID).
		Scan(&result.VideoID, &result.FileName, &result.MIMEType, &result.ByteSize, &result.DocumentID, &result.AccessHash,
			&result.FileReference, &result.DCID, &result.GroupID, &result.TopicID, &result.MessageID, &result.ChannelID, &result.ChannelHash)
	if err != nil {
		return VideoFileLocation{}, err
	}
	if result.ByteSize <= 0 || result.DocumentID == 0 || result.AccessHash == 0 || result.DCID <= 0 || len(result.FileReference) == 0 {
		return VideoFileLocation{}, errors.New("video location is incomplete")
	}
	return result, nil
}

func (s *Service) DownloadVideoRange(ctx context.Context, location VideoFileLocation, start, end int64) ([]byte, error) {
	return s.downloadVideoRange(ctx, location, start, end, false)
}

func (s *Service) downloadPersistentVideoRange(ctx context.Context, location VideoFileLocation, start, end int64) ([]byte, error) {
	return s.downloadVideoRange(ctx, location, start, end, true)
}

func (s *Service) downloadVideoRange(ctx context.Context, location VideoFileLocation, start, end int64, forcePersistent bool) ([]byte, error) {
	if start < 0 || end < start || end >= location.ByteSize || end-start+1 > StreamCacheBlockSize {
		return nil, errors.New("invalid video range")
	}
	blockStart := start / StreamCacheBlockSize * StreamCacheBlockSize
	blockEnd := min(location.ByteSize-1, blockStart+StreamCacheBlockSize-1)
	key := fmt.Sprintf("%s:%d:%d", location.VideoID, location.DocumentID, blockStart)
	persistentKey := StreamCacheKey(location, blockStart)
	if data, ok := s.cachedStreamRange(key); ok {
		return sliceStreamBlock(data, blockStart, start, end)
	}
	call, leader := s.beginStreamCall(key)
	if !leader {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			if call.err != nil {
				return nil, call.err
			}
			return sliceStreamBlock(call.data, blockStart, start, end)
		}
	}
	if data, ok := s.PersistentCache.Get(persistentKey, blockEnd-blockStart+1); ok {
		s.finishStreamCall(key, call, data, nil)
		return sliceStreamBlock(data, blockStart, start, end)
	}
	data, err := s.downloadVideoRangeWithRefresh(ctx, location, blockStart, blockEnd)
	s.finishStreamCall(key, call, data, err)
	if err != nil {
		return nil, err
	}
	if s.PersistentCache != nil && shouldPersistStreamBlock(blockStart, forcePersistent) {
		stable := append([]byte(nil), data...)
		go func() {
			if cacheErr := s.PersistentCache.Put(persistentKey, stable); cacheErr != nil {
				log.Printf("persist telegram stream block: %v", cacheErr)
			}
		}()
	}
	return sliceStreamBlock(data, blockStart, start, end)
}

func shouldPersistStreamBlock(blockStart int64, force bool) bool {
	return force || blockStart < PersistentStreamHeadSize
}

func (s *Service) PersistentCacheStats() PersistentCacheStats {
	return s.PersistentCache.Stats()
}

func (s *Service) SetPersistentCacheLimit(limit int64) {
	s.PersistentCache.SetLimit(limit)
}

func (s *Service) ClearPersistentCache() error {
	err := s.PersistentCache.Clear()
	s.streamMu.Lock()
	s.streamCache = make(map[string]streamCacheEntry)
	s.streamCacheBytes = 0
	s.streamMu.Unlock()
	s.indexWarmMu.Lock()
	s.indexPrepared = make(map[string]struct{})
	s.indexWarmMu.Unlock()
	return err
}

func sliceStreamBlock(data []byte, blockStart, start, end int64) ([]byte, error) {
	from, to := start-blockStart, end-blockStart+1
	if from < 0 || from >= int64(len(data)) || to > int64(len(data)) {
		return nil, errors.New("telegram returned an incomplete range")
	}
	return data[from:to], nil
}

func (s *Service) downloadVideoRangeWithRefresh(ctx context.Context, location VideoFileLocation, start, end int64) ([]byte, error) {
	data, err := s.downloadVideoRangeOnce(ctx, location, start, end)
	if !tgerr.Is(err, "FILE_REFERENCE_EXPIRED", "FILE_REFERENCE_INVALID") {
		return data, err
	}
	location, err = s.refreshExpiredVideoLocation(ctx, location)
	if err != nil {
		return nil, fmt.Errorf("refresh telegram file reference: %w", err)
	}
	return s.downloadVideoRangeOnce(ctx, location, start, end)
}

func (s *Service) cachedStreamRange(key string) ([]byte, bool) {
	now := time.Now()
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	entry, ok := s.streamCache[key]
	if !ok {
		return nil, false
	}
	if now.After(entry.expiresAt) {
		delete(s.streamCache, key)
		s.streamCacheBytes -= int64(len(entry.data))
		return nil, false
	}
	entry.usedAt = now
	s.streamCache[key] = entry
	return entry.data, true
}

func (s *Service) beginStreamCall(key string) (*streamCall, bool) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.streamInflight == nil {
		s.streamInflight = make(map[string]*streamCall)
	}
	if call, ok := s.streamInflight[key]; ok {
		return call, false
	}
	call := &streamCall{done: make(chan struct{})}
	s.streamInflight[key] = call
	return call, true
}

func (s *Service) finishStreamCall(key string, call *streamCall, data []byte, err error) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	call.data, call.err = data, err
	delete(s.streamInflight, key)
	if err == nil && len(data) > 0 {
		if s.streamCache == nil {
			s.streamCache = make(map[string]streamCacheEntry)
		}
		now := time.Now()
		s.streamCache[key] = streamCacheEntry{data: data, expiresAt: now.Add(streamCacheTTL), usedAt: now}
		s.streamCacheBytes += int64(len(data))
		s.evictStreamCacheLocked(now)
	}
	close(call.done)
}

func (s *Service) evictStreamCacheLocked(now time.Time) {
	for key, entry := range s.streamCache {
		if now.After(entry.expiresAt) {
			delete(s.streamCache, key)
			s.streamCacheBytes -= int64(len(entry.data))
		}
	}
	for s.streamCacheBytes > streamCacheLimit {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range s.streamCache {
			if oldestKey == "" || entry.usedAt.Before(oldest) {
				oldestKey, oldest = key, entry.usedAt
			}
		}
		if oldestKey == "" {
			break
		}
		entry := s.streamCache[oldestKey]
		delete(s.streamCache, oldestKey)
		s.streamCacheBytes -= int64(len(entry.data))
	}
}

func (s *Service) downloadVideoRangeOnce(ctx context.Context, location VideoFileLocation, start, end int64) ([]byte, error) {
	if start < 0 || end < start || end >= location.ByteSize {
		return nil, errors.New("invalid video range")
	}
	if end-start+1 > StreamChunkSize {
		end = start + StreamChunkSize - 1
	}
	if end >= location.ByteSize {
		end = location.ByteSize - 1
	}
	const alignment int64 = 4096
	alignedStart := start / alignment * alignment
	wanted := end - alignedStart + 1
	alignedLimit := (wanted + alignment - 1) / alignment * alignment
	megabyteEnd := (alignedStart/(1024*1024) + 1) * 1024 * 1024
	if alignedStart+alignedLimit > megabyteEnd {
		alignedLimit = megabyteEnd - alignedStart
	}
	if alignedLimit <= 0 || alignedLimit > 1024*1024 {
		return nil, errors.New("invalid telegram range")
	}

	api, err := s.mediaAPI(ctx, location.DCID)
	if err != nil {
		return nil, err
	}
	response, err := api.UploadGetFile(ctx, &tg.UploadGetFileRequest{Precise: true, Location: &tg.InputDocumentFileLocation{
		ID: location.DocumentID, AccessHash: location.AccessHash, FileReference: location.FileReference,
	}, Offset: alignedStart, Limit: int(alignedLimit)})
	if err != nil {
		return nil, err
	}
	file, ok := response.(*tg.UploadFile)
	if !ok {
		return nil, fmt.Errorf("unexpected telegram file response %T", response)
	}
	from, to := start-alignedStart, end-alignedStart+1
	if from < 0 || from >= int64(len(file.Bytes)) {
		return nil, errors.New("telegram returned an incomplete range")
	}
	if to > int64(len(file.Bytes)) {
		to = int64(len(file.Bytes))
	}
	return file.Bytes[from:to], nil
}

// refreshExpiredVideoLocation reloads the source message, as required by Telegram
// when a file_reference expires. The mutex coalesces simultaneous range failures;
// callers arriving after the first refresh reuse the newly persisted reference.
func (s *Service) refreshExpiredVideoLocation(ctx context.Context, stale VideoFileLocation) (VideoFileLocation, error) {
	s.fileReferenceMu.Lock()
	defer s.fileReferenceMu.Unlock()

	current, err := s.VideoLocation(ctx, stale.VideoID)
	if err != nil {
		return VideoFileLocation{}, err
	}
	if !bytes.Equal(current.FileReference, stale.FileReference) {
		return current, nil
	}

	s.apiMu.RLock()
	api := s.runtimeAPI
	s.apiMu.RUnlock()
	if api == nil {
		return VideoFileLocation{}, errors.New("telegram is reconnecting")
	}
	result, err := api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
		Channel: &tg.InputChannel{ChannelID: current.ChannelID, AccessHash: current.ChannelHash},
		ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: current.MessageID}},
	})
	if err != nil {
		return VideoFileLocation{}, err
	}
	modified, ok := result.AsModified()
	if !ok {
		return VideoFileLocation{}, errors.New("telegram returned no source message")
	}
	var source *tg.Message
	for _, item := range modified.GetMessages() {
		message, isMessage := item.(*tg.Message)
		if isMessage && message.ID == current.MessageID {
			source = message
			break
		}
	}
	if source == nil {
		return VideoFileLocation{}, errors.New("telegram source message is unavailable")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return VideoFileLocation{}, err
	}
	defer tx.Rollback()
	stored, err := storeVideoMessage(ctx, tx, current.GroupID, current.TopicID, source)
	if err != nil {
		return VideoFileLocation{}, err
	}
	if !stored {
		return VideoFileLocation{}, errors.New("telegram source message is no longer a video")
	}
	if err = tx.Commit(); err != nil {
		return VideoFileLocation{}, err
	}
	return s.VideoLocation(ctx, current.VideoID)
}
