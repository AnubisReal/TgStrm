package telegram

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	maxMP4TopLevelBoxes = 64
	maxMP4IndexWarmSize = int64(32 * 1024 * 1024)
	mp4IndexWorkers     = 3
)

type mp4BoxHeader struct {
	kind       string
	size       int64
	headerSize int64
}

func parseMP4BoxHeader(data []byte, remaining int64) (mp4BoxHeader, error) {
	if len(data) < 8 {
		return mp4BoxHeader{}, errors.New("short MP4 box header")
	}
	size := int64(binary.BigEndian.Uint32(data[:4]))
	headerSize := int64(8)
	if size == 1 {
		if len(data) < 16 {
			return mp4BoxHeader{}, errors.New("short extended MP4 box header")
		}
		extended := binary.BigEndian.Uint64(data[8:16])
		if extended > uint64(^uint64(0)>>1) {
			return mp4BoxHeader{}, errors.New("MP4 box is too large")
		}
		size, headerSize = int64(extended), 16
	} else if size == 0 {
		size = remaining
	}
	if size < headerSize || size > remaining {
		return mp4BoxHeader{}, errors.New("invalid MP4 box size")
	}
	return mp4BoxHeader{kind: string(data[4:8]), size: size, headerSize: headerSize}, nil
}

// PrepareVideoIndex schedules an on-demand MP4 index warm-up. Calls for the
// same immutable Telegram document are coalesced for the process lifetime.
func (s *Service) PrepareVideoIndex(location VideoFileLocation) {
	if location.MIMEType != "video/mp4" && location.MIMEType != "application/mp4" {
		return
	}
	key := fmt.Sprintf("%d:%d", location.DocumentID, location.ByteSize)
	s.indexWarmMu.Lock()
	if s.indexWarming == nil {
		s.indexWarming = make(map[string]struct{})
	}
	if s.indexPrepared == nil {
		s.indexPrepared = make(map[string]struct{})
	}
	if _, ok := s.indexWarming[key]; ok {
		s.indexWarmMu.Unlock()
		return
	}
	if _, ok := s.indexPrepared[key]; ok {
		s.indexWarmMu.Unlock()
		return
	}
	s.indexWarming[key] = struct{}{}
	s.indexWarmMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		offset, size, err := s.findMP4Index(ctx, location)
		if err == nil && size > 0 {
			err = s.warmMP4Index(ctx, location, offset, size)
		}
		s.indexWarmMu.Lock()
		delete(s.indexWarming, key)
		if err == nil {
			s.indexPrepared[key] = struct{}{}
		}
		s.indexWarmMu.Unlock()
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				log.Printf("prepare MP4 index video=%s: %v", location.VideoID, err)
			}
			return
		}
		if size > 0 {
			log.Printf("MP4 index ready video=%s offset=%d size=%d in %s", location.VideoID, offset, size, time.Since(started).Round(time.Millisecond))
		}
	}()
}

func (s *Service) findMP4Index(ctx context.Context, location VideoFileLocation) (int64, int64, error) {
	if location.ByteSize < 8 {
		return 0, 0, errors.New("file is too small for MP4")
	}
	var offset int64
	for range maxMP4TopLevelBoxes {
		headerBytes, err := s.readVideoBytes(ctx, location, offset, min(location.ByteSize-1, offset+15))
		if err != nil {
			return 0, 0, err
		}
		header, err := parseMP4BoxHeader(headerBytes, location.ByteSize-offset)
		if err != nil {
			return 0, 0, err
		}
		if header.kind == "moov" {
			return offset, header.size, nil
		}
		next := offset + header.size
		if next <= offset || next >= location.ByteSize {
			return 0, 0, nil
		}
		offset = next
	}
	return 0, 0, errors.New("too many top-level MP4 boxes")
}

func (s *Service) readVideoBytes(ctx context.Context, location VideoFileLocation, start, end int64) ([]byte, error) {
	result := make([]byte, 0, end-start+1)
	for offset := start; offset <= end; {
		blockEnd := ((offset/StreamCacheBlockSize)+1)*StreamCacheBlockSize - 1
		partEnd := min(end, blockEnd)
		part, err := s.DownloadVideoRange(ctx, location, offset, partEnd)
		if err != nil {
			return nil, err
		}
		result = append(result, part...)
		offset = partEnd + 1
	}
	return result, nil
}

func (s *Service) warmMP4Index(ctx context.Context, location VideoFileLocation, offset, size int64) error {
	if size > maxMP4IndexWarmSize {
		return fmt.Errorf("MP4 index exceeds warm-up limit: %d bytes", size)
	}
	end := offset + size - 1
	firstBlock := offset / StreamCacheBlockSize * StreamCacheBlockSize
	type byteRange struct{ start, end int64 }
	jobs := make(chan byteRange)
	errOnce := make(chan error, 1)
	var workers sync.WaitGroup
	for range mp4IndexWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if _, err := s.downloadPersistentVideoRange(ctx, location, job.start, job.end); err != nil {
					select {
					case errOnce <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for start := firstBlock; start <= end; start += StreamCacheBlockSize {
		select {
		case jobs <- byteRange{start: start, end: min(location.ByteSize-1, start+StreamCacheBlockSize-1)}:
		case err := <-errOnce:
			close(jobs)
			workers.Wait()
			return err
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	select {
	case err := <-errOnce:
		return err
	default:
		return nil
	}
}
