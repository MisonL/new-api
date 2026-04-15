package common

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

const payloadAuditMaxPreviewBytes = 16 * 1024

type PayloadAuditRecord struct {
	Content     string
	ContentType string
	Bytes       int64
	Truncated   bool
	Omitted     bool
}

type payloadAuditResponseWriter struct {
	gin.ResponseWriter
	buf       bytes.Buffer
	total     int64
	truncated bool
}

func (w *payloadAuditResponseWriter) Write(data []byte) (int, error) {
	w.capture(data)
	return w.ResponseWriter.Write(data)
}

func (w *payloadAuditResponseWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *payloadAuditResponseWriter) capture(data []byte) {
	w.total += int64(len(data))
	if w.truncated || len(data) == 0 {
		return
	}
	remain := payloadAuditMaxPreviewBytes - w.buf.Len()
	if remain <= 0 {
		w.truncated = true
		return
	}
	if len(data) > remain {
		_, _ = w.buf.Write(data[:remain])
		w.truncated = true
		return
	}
	_, _ = w.buf.Write(data)
}

func (w *payloadAuditResponseWriter) Record(contentType string) *PayloadAuditRecord {
	if w == nil || (w.total == 0 && w.buf.Len() == 0) {
		return nil
	}
	return buildPayloadAuditRecord(w.buf.Bytes(), w.total, contentType, w.truncated)
}

func InitPayloadAudit(c *gin.Context, captureRequest bool, captureResponse bool) {
	if c == nil {
		return
	}
	if captureRequest {
		if record := captureRequestPayload(c); record != nil {
			SetContextKey(c, constant.ContextKeyLogRequestPayload, *record)
		}
	}
	if captureResponse {
		writer := &payloadAuditResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		SetContextKey(c, constant.ContextKeyLogResponsePayload, writer)
	}
}

func AppendPayloadAuditFields(c *gin.Context, other map[string]interface{}) map[string]interface{} {
	if c == nil {
		return other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	if requestRecord, ok := GetContextKeyType[PayloadAuditRecord](c, constant.ContextKeyLogRequestPayload); ok {
		appendPayloadAuditRecord(other, "request", &requestRecord)
	}
	if responseWriter, ok := GetContextKeyType[*payloadAuditResponseWriter](c, constant.ContextKeyLogResponsePayload); ok {
		responseRecord := responseWriter.Record(c.Writer.Header().Get("Content-Type"))
		appendPayloadAuditRecord(other, "response", responseRecord)
	}
	return other
}

func appendPayloadAuditRecord(other map[string]interface{}, prefix string, record *PayloadAuditRecord) {
	if other == nil || record == nil || record.Content == "" {
		return
	}
	other[prefix+"_content"] = record.Content
	if record.ContentType != "" {
		other[prefix+"_content_type"] = record.ContentType
	}
	if record.Bytes > 0 {
		other[prefix+"_content_bytes"] = record.Bytes
	}
	if record.Truncated {
		other[prefix+"_content_truncated"] = true
	}
	if record.Omitted {
		other[prefix+"_content_omitted"] = true
	}
}

func captureRequestPayload(c *gin.Context) *PayloadAuditRecord {
	if c == nil || c.Request == nil || c.Request.Method == http.MethodGet {
		return nil
	}
	storage, err := GetBodyStorage(c)
	if err != nil {
		return nil
	}
	return buildPayloadAuditRecordFromStorage(storage, c.ContentType())
}

func buildPayloadAuditRecordFromStorage(storage BodyStorage, contentType string) *PayloadAuditRecord {
	if storage == nil || storage.Size() == 0 {
		return nil
	}

	currentPos, err := storage.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil
	}
	if _, err = storage.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	defer func() {
		_, _ = storage.Seek(currentPos, io.SeekStart)
	}()

	previewSize := int(storage.Size())
	if previewSize > payloadAuditMaxPreviewBytes {
		previewSize = payloadAuditMaxPreviewBytes
	}
	if previewSize <= 0 {
		return nil
	}

	preview := make([]byte, previewSize)
	n, err := io.ReadFull(storage, preview)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil
	}
	preview = preview[:n]
	if len(preview) == 0 {
		return nil
	}

	return buildPayloadAuditRecord(
		preview,
		storage.Size(),
		contentType,
		storage.Size() > payloadAuditMaxPreviewBytes,
	)
}

func buildPayloadAuditRecord(data []byte, totalBytes int64, contentType string, truncated bool) *PayloadAuditRecord {
	if len(data) == 0 && totalBytes == 0 {
		return nil
	}
	mediaType := strings.TrimSpace(contentType)
	if parsedType, _, err := mime.ParseMediaType(contentType); err == nil {
		mediaType = parsedType
	}
	record := &PayloadAuditRecord{
		ContentType: mediaType,
		Bytes:       totalBytes,
		Truncated:   truncated || totalBytes > payloadAuditMaxPreviewBytes,
	}
	if shouldOmitPayloadBody(mediaType, data) {
		record.Omitted = true
		record.Content = buildOmittedPayloadSummary(mediaType, totalBytes)
		return record
	}
	preview := data
	if len(preview) > payloadAuditMaxPreviewBytes {
		preview = preview[:payloadAuditMaxPreviewBytes]
		record.Truncated = true
	}
	record.Content = string(preview)
	if record.Truncated {
		record.Content += fmt.Sprintf("\n\n[truncated, original size: %d bytes]", totalBytes)
	}
	return record
}

func buildOmittedPayloadSummary(contentType string, totalBytes int64) string {
	if contentType == "" {
		contentType = "unknown"
	}
	if totalBytes > 0 {
		return fmt.Sprintf("[%s body omitted, %d bytes]", contentType, totalBytes)
	}
	return fmt.Sprintf("[%s body omitted]", contentType)
}

func shouldOmitPayloadBody(contentType string, data []byte) bool {
	switch {
	case strings.HasPrefix(contentType, "multipart/form-data"),
		contentType == "application/octet-stream",
		strings.HasPrefix(contentType, "image/"),
		strings.HasPrefix(contentType, "audio/"),
		strings.HasPrefix(contentType, "video/"):
		return true
	}
	if isTextualContentType(contentType) {
		return false
	}
	return !looksTextual(data)
}

func isTextualContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	return strings.HasPrefix(contentType, "text/") ||
		contentType == "application/json" ||
		strings.HasSuffix(contentType, "+json") ||
		contentType == "application/xml" ||
		strings.HasSuffix(contentType, "+xml") ||
		contentType == "application/x-www-form-urlencoded" ||
		contentType == "text/event-stream"
}

func looksTextual(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	reader := bytes.NewReader(data)
	sample, err := io.ReadAll(io.LimitReader(reader, 2048))
	if err != nil || len(sample) == 0 {
		return false
	}
	printable := 0
	for _, r := range string(sample) {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			printable++
		}
	}
	return printable*100/len([]rune(string(sample))) >= 90
}
