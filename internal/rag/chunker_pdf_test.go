package rag

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExtractPdfTextHandlesContentStreamsSplitAcrossStreams(t *testing.T) {
	path := writeSplitContentPDF(t)
	done := make(chan struct {
		text string
		err  error
	}, 1)
	go func() {
		text, err := extractPdfText(path)
		done <- struct {
			text string
			err  error
		}{text: text, err: err}
	}()

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("extractPdfText: %v", result.err)
		}
		if !strings.Contains(result.text, "split across") {
			t.Fatalf("text = %q, want split content", result.text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("extractPdfText did not return for content streams split across streams")
	}
}

func writeSplitContentPDF(t *testing.T) string {
	t.Helper()
	content1 := "BT\n/F1 12 Tf\n[(split"
	content2 := " across)] TJ\nET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents [5 0 R 6 0 R] >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content1), content1),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content2), content2),
	}

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)

	path := t.TempDir() + string(os.PathSeparator) + "split-content.pdf"
	if err := os.WriteFile(path, pdf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
