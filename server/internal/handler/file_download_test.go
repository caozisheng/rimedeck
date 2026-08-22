package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWriteAttachmentDownloadStreamsFileBytes(t *testing.T) {
	body := []byte("\x89PNG\r\n\x1a\nimage-bytes")
	store := &mockStorage{}
	url, err := store.Upload(context.Background(), "workspaces/ws/image.png", body, "image/png", "screen shot.png")
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{Storage: store}
	att := db.Attachment{
		ID:          pgtype.UUID{Valid: true},
		Filename:    "screen shot.png",
		Url:         url,
		ContentType: "image/png",
		SizeBytes:   int64(len(body)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/attachments/id/download", nil)
	rec := httptest.NewRecorder()

	h.writeAttachmentDownload(rec, req, att)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Body.Bytes(); string(got) != string(body) {
		t.Fatalf("body = %q, want raw bytes %q", got, body)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "19" {
		t.Fatalf("Content-Length = %q, want 19", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="screen shot.png"` {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestAttachmentToResponseUsesPublicLocalUploadURL(t *testing.T) {
	h := &Handler{Storage: &mockStorageNoCdn{}}
	att := db.Attachment{
		ID:  pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Url: "/uploads/workspaces/ws/image.png", Filename: "image.png",
		ContentType: "image/png", SizeBytes: 1,
	}
	resp := h.attachmentToResponse(att)
	if resp.DownloadURL != att.Url || resp.MarkdownURL != att.Url {
		t.Fatalf("download_url=%q markdown_url=%q want local upload URL %q", resp.DownloadURL, resp.MarkdownURL, att.Url)
	}
}
