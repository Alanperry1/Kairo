package research

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const MaxDatasetBytes int64 = 5_000_000_000
const InlineDatasetBytes int64 = 20 << 20
const InlineDatasetRows = 100000

var errDatasetTooLarge = errors.New("CSV must be at most 5 GB")

type byteCounter struct{ n int64 }

func (c *byteCounter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// Bound physical lines and multiline records before encoding/csv can allocate
// arbitrarily large strings. At most one physical line is delivered per read.
type csvRecordReader struct {
	reader  *bufio.Reader
	pending []byte
	used    int
}

func newCSVReader(reader io.Reader) (*csv.Reader, *csvRecordReader) {
	guard := &csvRecordReader{reader: bufio.NewReaderSize(reader, 1<<20)}
	csvReader := csv.NewReader(guard)
	return csvReader, guard
}
func (r *csvRecordReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		line, err := r.reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			return 0, errors.New("CSV lines must be at most 1 MiB")
		}
		if len(line) == 0 {
			return 0, err
		}
		r.used += len(line)
		if r.used > 2<<20 {
			return 0, errors.New("CSV records must be at most 2 MiB")
		}
		r.pending = line
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
func validateCSV(reader io.Reader) ([]string, [][]string, int, error) {
	csvReader, guard := newCSVReader(reader)
	header, err := csvReader.Read()
	if err != nil || len(header) < 2 || len(header) > 100 {
		return nil, nil, 0, errors.New("CSV requires a header with 2–100 columns")
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	seen := map[string]bool{}
	for _, name := range header {
		if name == "" || strings.TrimSpace(name) != name || len(name) > 100 || seen[name] {
			return nil, nil, 0, errors.New("column names must be nonempty, unique, trimmed and at most 100 characters")
		}
		seen[name] = true
	}
	rows := 0
	preview := [][]string{}
	for {
		guard.used = 0
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("invalid CSV row: %w", err)
		}
		rows++
		for _, value := range row {
			if len(value) > 10000 {
				return nil, nil, 0, errors.New("CSV cells must be at most 10000 characters")
			}
		}
		if len(preview) < 5 {
			preview = append(preview, append([]string{}, row...))
		}
	}
	if rows == 0 {
		return nil, nil, 0, errors.New("CSV must contain data rows")
	}
	return header, preview, rows, nil
}
func (s *Server) ingestDataset(ctx context.Context, name string, reader io.Reader, source string) (Dataset, error) {
	return s.ingestDatasetLimit(ctx, name, reader, source, MaxDatasetBytes)
}
func (s *Server) ingestDatasetLimit(ctx context.Context, name string, reader io.Reader, source string, limit int64) (Dataset, error) {
	var d Dataset
	if len(name) > 180 || strings.ToLower(filepath.Ext(name)) != ".csv" {
		return d, errors.New("file must have a .csv extension (name max 180 characters)")
	}
	objects, ok := s.objectStorage().(StreamingObjectStorage)
	if !ok {
		return d, errors.New("streaming dataset storage is unavailable")
	}
	d.ID = newID("ds")
	key := "datasets/" + d.ID + ".csv"
	pipeReader, pipeWriter := io.Pipe()
	uploaded := make(chan error, 1)
	go func() {
		err := objects.PutStream(ctx, key, "text/csv; charset=utf-8", pipeReader)
		_ = pipeReader.CloseWithError(err)
		uploaded <- err
	}()
	hash := sha256.New()
	count := &byteCounter{}
	bounded := io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limit+1)
	header, preview, rows, validationErr := validateCSV(io.TeeReader(bounded, io.MultiWriter(hash, count, pipeWriter)))
	if count.n > limit {
		validationErr = errDatasetTooLarge
	}
	_ = pipeWriter.CloseWithError(validationErr)
	uploadErr := <-uploaded
	cleanup := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = s.objectStorage().Delete(cleanup, key)
	}
	if validationErr != nil {
		cleanup()
		return Dataset{}, validationErr
	}
	if uploadErr != nil {
		cleanup()
		return Dataset{}, uploadErr
	}
	d.Name = name
	d.Rows = rows
	d.Columns = header
	d.Preview = preview
	d.Size = count.n
	d.SHA256 = hex.EncodeToString(hash.Sum(nil))
	d.CreatedAt = time.Now().UTC()
	d.SourceURL = source
	if err := s.Store.SaveDataset(d); err != nil {
		cleanup()
		return Dataset{}, err
	}
	return d, nil
}
func longDatasetRequest(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(6 * time.Hour))
	_ = controller.SetWriteDeadline(time.Now().Add(6 * time.Hour))
}
func (s *Server) uploadDataset(w http.ResponseWriter, r *http.Request) {
	longDatasetRequest(w, r)
	r.Body = http.MaxBytesReader(w, r.Body, MaxDatasetBytes+(2<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		apiError(w, 400, "invalid_upload", "Use multipart/form-data with one CSV field named file")
		return
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		apiError(w, 400, "invalid_upload", "Exactly one CSV file field named file is required")
		return
	}
	name := filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
	d, err := s.ingestDataset(r.Context(), name, part, "")
	part.Close()
	if err != nil {
		status := 422
		if errors.Is(err, errDatasetTooLarge) {
			status = 413
		}
		apiError(w, status, "invalid_dataset", err.Error())
		return
	}
	if _, err = reader.NextPart(); err != io.EOF {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = s.objectStorage().Delete(cleanup, "datasets/"+d.ID+".csv")
		_ = s.Store.DeleteDataset(d.ID)
		apiError(w, 400, "invalid_upload", "Exactly one file field is required")
		return
	}
	respond(w, 201, d)
}
