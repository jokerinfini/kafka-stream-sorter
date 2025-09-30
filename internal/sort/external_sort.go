package sort

import (
	"bufio"
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gokafka "github.com/segmentio/kafka-go"
)

// ExternalSort reads from kafkaReader, sorts by key index, and writes sorted records to kafkaWriter.
// sortKeyIndex: 0=id, 1=name, 3=continent
func ExternalSort(kafkaReader *gokafka.Reader, kafkaWriter *gokafka.Writer, sortKeyIndex int, tempDir string) error {
	if sortKeyIndex != 0 && sortKeyIndex != 1 && sortKeyIndex != 3 {
		return fmt.Errorf("invalid sortKeyIndex: %d", sortKeyIndex)
	}

	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}

	const chunkSize = 1_000_000
	baseCtx := context.Background()
	ctx := baseCtx
	var tempFiles []string

	// Chunking phase
	for {
		records := make([][]byte, 0, chunkSize)
		deadline := time.Now().Add(5 * time.Second)
		for len(records) < chunkSize {
			// Use a timeout context per read (kafka-go Reader supports per-call context deadline)
			readCtx, cancel := context.WithDeadline(baseCtx, deadline)
			msg, err := kafkaReader.ReadMessage(readCtx)
			cancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
					// assume drained for this chunk
					break
				}
				// If EOF-like or timeout, break; else return error
				if isTemporary(err) {
					break
				}
				return err
			}
			// copy value to prevent reuse
			rec := make([]byte, len(msg.Value))
			copy(rec, msg.Value)
			records = append(records, rec)
		}
		if len(records) == 0 {
			break
		}

		// Sort in-memory by key
		if sortKeyIndex == 0 {
			// Numeric compare for id
			sort.Slice(records, func(i, j int) bool {
				return extractID(records[i]) < extractID(records[j])
			})
		} else {
			sort.Slice(records, func(i, j int) bool {
				ki := extractKeyString(records[i], sortKeyIndex)
				kj := extractKeyString(records[j], sortKeyIndex)
				return ki < kj
			})
		}

		// Spill to temp file
		fpath := filepath.Join(tempDir, fmt.Sprintf("chunk_%d.tmp", len(tempFiles)))
		if err := writeChunk(fpath, records); err != nil {
			return err
		}
		tempFiles = append(tempFiles, fpath)
		if len(records) < chunkSize {
			// Drained topic
			break
		}
	}

	if len(tempFiles) == 0 {
		return nil
	}

	// Merge phase
	if err := kWayMergeToKafka(ctx, tempFiles, kafkaWriter, sortKeyIndex); err != nil {
		return err
	}

	// Cleanup
	for _, f := range tempFiles {
		_ = os.Remove(f)
	}
	return nil
}

func writeChunk(path string, records [][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Increase buffer size to reduce syscalls during spill
	bw := bufio.NewWriterSize(f, 4<<20)
	for _, r := range records {
		if _, err := bw.Write(r); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return bw.Flush()
}

type fileScanner struct {
	f  *os.File
	br *bufio.Reader
}

func newFileScanner(path string) (*fileScanner, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Larger read buffer reduces read syscalls during merge
	return &fileScanner{f: f, br: bufio.NewReaderSize(f, 4<<20)}, nil
}

func (s *fileScanner) next() ([]byte, error) {
	line, err := s.br.ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			// last line without newline
			return bytes.TrimRight(line, "\n"), nil
		}
		return nil, err
	}
	return bytes.TrimRight(line, "\n"), nil
}

func (s *fileScanner) close() error { return s.f.Close() }

type heapItem struct {
	keyStr string
	keyInt int64
	useInt bool
	val    []byte
	i      int // index of file scanner
}

type minHeap []heapItem

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	if h[i].useInt || h[j].useInt {
		// When sorting ids, both will have useInt=true
		return h[i].keyInt < h[j].keyInt
	}
	return h[i].keyStr < h[j].keyStr
}
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(heapItem)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func kWayMergeToKafka(ctx context.Context, files []string, writer *gokafka.Writer, sortKeyIndex int) error {
	scanners := make([]*fileScanner, len(files))
	for i, f := range files {
		sc, err := newFileScanner(f)
		if err != nil {
			return err
		}
		scanners[i] = sc
	}
	defer func() {
		for _, sc := range scanners {
			if sc != nil {
				_ = sc.close()
			}
		}
	}()

	h := &minHeap{}
	heap.Init(h)
	for i, sc := range scanners {
		if rec, err := sc.next(); err == nil {
			if sortKeyIndex == 0 {
				heap.Push(h, heapItem{keyInt: extractID(rec), useInt: true, val: rec, i: i})
			} else {
				heap.Push(h, heapItem{keyStr: extractKeyString(rec, sortKeyIndex), val: rec, i: i})
			}
		}
	}

	batch := make([]gokafka.Message, 0, 1000)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := writer.WriteMessages(ctx, batch...); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for h.Len() > 0 {
		item := heap.Pop(h).(heapItem)
		batch = append(batch, gokafka.Message{Value: append([]byte(nil), item.val...)})
		if len(batch) >= cap(batch) {
			if err := flush(); err != nil {
				return err
			}
		}
		if rec, err := scanners[item.i].next(); err == nil {
			if sortKeyIndex == 0 {
				heap.Push(h, heapItem{keyInt: extractID(rec), useInt: true, val: rec, i: item.i})
			} else {
				heap.Push(h, heapItem{keyStr: extractKeyString(rec, sortKeyIndex), val: rec, i: item.i})
			}
		}
	}
	return flush()
}

func extractKeyString(rec []byte, idx int) string {
	// Fast split without full CSV parsing (fields do not contain commas per spec)
	// id,name,address,continent
	// Return field at idx as string
	// We rely on small number of splits for speed
	switch idx {
	case 0: // id
		i := bytes.IndexByte(rec, ',')
		if i == -1 {
			return string(rec)
		}
		return string(rec[:i])
	case 1: // name
		first := bytes.IndexByte(rec, ',')
		if first == -1 {
			return string(rec)
		}
		rest := rec[first+1:]
		second := bytes.IndexByte(rest, ',')
		if second == -1 {
			return string(rest)
		}
		return string(rest[:second])
	case 3: // continent
		// find last comma without converting to string
		last := bytes.LastIndexByte(rec, ',')
		if last == -1 {
			return string(rec)
		}
		return string(rec[last+1:])
	}
	return string(rec)
}

// extractID parses the leading integer id (before first comma) as int64 without allocations.
func extractID(rec []byte) int64 {
	var n int64
	neg := false
	for i := 0; i < len(rec); i++ {
		c := rec[i]
		if c == ',' {
			break
		}
		if c == '-' && i == 0 {
			neg = true
			continue
		}
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else {
			// unexpected char, stop
			break
		}
	}
	if neg {
		n = -n
	}
	return n
}

func isTimeout(err error) bool {
	// kafka-go wraps context deadline exceeded; simple string check fallback
	return strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "timeout")
}
func isTemporary(err error) bool { return isTimeout(err) }
