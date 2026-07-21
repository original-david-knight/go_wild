package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// captureLog redirects the default log output for the duration of the callback
// and returns whatever was written.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	fn()
	return buf.String()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestReadWebpageToolPrefersOldRedditForPublicRedditPages(t *testing.T) {
	var requested []string
	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			if req.URL.Host != oldRedditHost {
				t.Fatalf("expected old.reddit.com request first, got %s", req.URL.Host)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>Old Reddit Thread</title></head><body><p>Fetched via old reddit.</p></body></html>`)),
				Request:    req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://www.reddit.com/r/golang/comments/example/thread/",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}
	if len(requested) != 1 {
		t.Fatalf("expected 1 request, got %d (%v)", len(requested), requested)
	}
	content := result.Content.(map[string]any)["content"].(string)
	if !strings.Contains(content, "Fetched via old reddit.") {
		t.Fatalf("expected old reddit content, got %q", content)
	}
}

func TestReadWebpageToolFallsBackToOriginalRedditURLWhenOldRedditFails(t *testing.T) {
	var requested []string
	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			switch req.URL.Host {
			case oldRedditHost:
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Status:     "403 Forbidden",
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader("forbidden")),
					Request:    req,
				}, nil
			case "www.reddit.com":
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>Reddit Thread</title></head><body><p>Fetched via original reddit host.</p></body></html>`)),
					Request:    req,
				}, nil
			default:
				t.Fatalf("unexpected host %s", req.URL.Host)
				return nil, nil
			}
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://www.reddit.com/r/golang/comments/example/thread/",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}
	if len(requested) != 2 {
		t.Fatalf("expected 2 requests, got %d (%v)", len(requested), requested)
	}
	if !strings.Contains(requested[0], "old.reddit.com") || !strings.Contains(requested[1], "www.reddit.com") {
		t.Fatalf("unexpected request order: %v", requested)
	}
	content := result.Content.(map[string]any)["content"].(string)
	if !strings.Contains(content, "Fetched via original reddit host.") {
		t.Fatalf("expected fallback content, got %q", content)
	}
}

// buildZeroPagePDF builds a PDF whose Pages tree has zero Kids.
func buildZeroPagePDF() []byte {
	var buf bytes.Buffer
	offsets := make([]int, 2)

	buf.WriteString("%PDF-1.4\n")
	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")

	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", len(offsets)+1))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))
	return buf.Bytes()
}

// buildMixedPagePDF builds a 2-page PDF: page 1 valid, page 2 with a dangling
// Kid reference that the library resolves to a null page value.
func buildMixedPagePDF() []byte {
	stream := "BT /F1 12 Tf 100 700 Td (Good Page) Tj ET"
	streamLen := len(stream)

	var buf bytes.Buffer
	offsets := make([]int, 5)
	buf.WriteString("%PDF-1.4\n")

	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R 99 0 R] /Count 2 >>\nendobj\n")
	offsets[2] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	offsets[3] = buf.Len()
	buf.WriteString(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", streamLen, stream))
	offsets[4] = buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", len(offsets)+1))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))
	return buf.Bytes()
}

// buildPDFWithBadContentsPage builds a 2-page PDF where page 2's Contents
// reference points at a non-stream object, causing GetPlainText to error.
func buildPDFWithBadContentsPage() []byte {
	stream := "BT /F1 12 Tf 100 700 Td (Good Page) Tj ET"
	streamLen := len(stream)

	var buf bytes.Buffer
	offsets := make([]int, 7)
	buf.WriteString("%PDF-1.4\n")

	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>\nendobj\n")
	offsets[2] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	offsets[3] = buf.Len()
	buf.WriteString(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", streamLen, stream))
	offsets[4] = buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
	offsets[5] = buf.Len()
	buf.WriteString("6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	offsets[6] = buf.Len()
	buf.WriteString("7 0 obj\n<< /NotAStream true >>\nendobj\n")

	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", len(offsets)+1))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))
	return buf.Bytes()
}

// buildAllBadContentsPDF builds a 1-page PDF whose only page has a bad
// Contents reference, so every page errors.
func buildAllBadContentsPDF() []byte {
	var buf bytes.Buffer
	offsets := make([]int, 5)
	buf.WriteString("%PDF-1.4\n")

	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	offsets[2] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	offsets[3] = buf.Len()
	buf.WriteString("4 0 obj\n<< /NotAStream true >>\nendobj\n")
	offsets[4] = buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", len(offsets)+1))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))
	return buf.Bytes()
}

// buildMinimalPDF creates a minimal valid PDF containing the given text.
func buildMinimalPDF(text string) []byte {
	// Minimal PDF 1.4 with a single page containing the text.
	stream := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", text)
	streamLen := len(stream)

	var buf bytes.Buffer
	offsets := make([]int, 5) // objects 1-5

	buf.WriteString("%PDF-1.4\n")

	// Object 1: Catalog
	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	// Object 2: Pages
	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	// Object 3: Page
	offsets[2] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")

	// Object 4: Content stream
	offsets[3] = buf.Len()
	buf.WriteString(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", streamLen, stream))

	// Object 5: Font
	offsets[4] = buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	// Cross-reference table
	xrefOffset := buf.Len()
	buf.WriteString("xref\n")
	buf.WriteString(fmt.Sprintf("0 %d\n", len(offsets)+1))
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}

	// Trailer
	buf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\n", len(offsets)+1))
	buf.WriteString(fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset))

	return buf.Bytes()
}

// TestBuildMinimalPDFProducesValidPDF is a regression test ensuring
// buildMinimalPDF continues to emit a structurally valid PDF parseable by
// the same library that extractPDFText uses. If buildMinimalPDF is modified
// (e.g. objects renumbered, offsets miscomputed, trailer changed), this test
// fails before any of the higher-level HTTP tests run, pinpointing the cause.
func TestBuildMinimalPDFProducesValidPDF(t *testing.T) {
	const marker = "Regression marker text"
	data := buildMinimalPDF(marker)

	if !bytes.HasPrefix(data, []byte("%PDF-1.")) {
		t.Fatalf("missing PDF header, got prefix %q", string(data[:min(8, len(data))]))
	}
	// EOF must be the file suffix (after trailing whitespace), matching the
	// PDF spec and how ledongthuc/pdf locates it.
	if !bytes.HasSuffix(bytes.TrimRight(data, "\r\n\t "), []byte("%%EOF")) {
		t.Error("PDF does not end with EOF trailer")
	}

	// Verify the startxref pointer actually points at an "xref" keyword in
	// the file. This catches the regression where the marker is still
	// present but the offset was miscomputed.
	startMarker := []byte("\nstartxref\n")
	idx := bytes.Index(data, startMarker)
	if idx < 0 {
		t.Fatal("missing startxref marker")
	}
	rest := data[idx+len(startMarker):]
	eol := bytes.IndexByte(rest, '\n')
	if eol < 0 {
		t.Fatal("startxref has no terminating newline")
	}
	offset, err := strconv.Atoi(string(bytes.TrimSpace(rest[:eol])))
	if err != nil {
		t.Fatalf("startxref offset is not an integer: %v", err)
	}
	if offset < 0 || offset >= len(data) {
		t.Fatalf("startxref offset %d out of range [0, %d)", offset, len(data))
	}
	if !bytes.HasPrefix(data[offset:], []byte("xref\n")) {
		t.Errorf("startxref offset %d does not point at an xref table, got %q", offset, string(data[offset:min(offset+16, len(data))]))
	}

	// Round-trip through the production extractor to confirm the PDF is
	// semantically valid and the embedded text survives parsing.
	text, err := extractPDFText(data)
	if err != nil {
		t.Fatalf("extractPDFText failed on buildMinimalPDF output: %v", err)
	}
	if !strings.Contains(text, marker) {
		t.Errorf("extracted text missing marker %q, got %q", marker, text)
	}
}

// TestExtractPDFTextNeverPanics guards the contract that extractPDFText
// always returns — never panics — regardless of input. The underlying
// ledongthuc/pdf library uses panic() as its internal parse-error channel in
// NewReader/NumPage/Page/Value, and only a subset of its public entry points
// (notably Page.GetPlainText) recovers. Open upstream issues report real
// crashes from valid-looking PDFs (e.g. ledongthuc/pdf#57 "Crash when image
// is in there", #30 on mixed CJK). Since we feed this extractor PDFs fetched
// from arbitrary URLs, any unrecovered panic would crash the agent process.
func TestExtractPDFTextNeverPanics(t *testing.T) {
	cases := map[string][]byte{
		"empty":                  nil,
		"random garbage":         bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 64),
		"truncated after header": append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{0x00}, 128)...),
		"bogus startxref":        []byte("%PDF-1.4\n%garbage\nstartxref\n999999\n%%EOF\n"),
		"header only":            []byte("%PDF-1.4\n%%EOF\n"),
		"exceeds size cap":       bytes.Repeat([]byte{'A'}, maxPDFBytes+1),
		// An XRefStm with /Filter /FlateDecode over non-flate bytes makes the
		// library panic through zlib during NewReader. This is the concrete
		// trigger that exercises the recover() branch.
		"xref stream with invalid flate": buildXRefStreamPDFWithBadFlate(),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			// The assertion is that this does not panic. If the library
			// panics, the test binary crashes and the failure is obvious.
			// The returned error is expected but not the focus.
			_, err := extractPDFText(data)
			if err == nil {
				t.Fatal("expected error for malformed PDF, got nil")
			}
		})
	}
}

// TestExtractPDFPageTextRecoversFromPanic exercises the per-page recover
// branch of extractPDFPageText directly. Passing a nil *pdf.Reader triggers
// a nil-pointer deref inside the library on the Page() call — standing in
// for any unexpected panic the library could raise mid-loop. The per-page
// recover must convert that into a returned error so the surrounding loop
// in extractPDFText can count it as an errored page and preserve any text
// already extracted from earlier pages.
func TestExtractPDFPageTextRecoversFromPanic(t *testing.T) {
	// Swallow the debug.Stack log so the test output stays clean.
	captureLog(t, func() {
		text, err := extractPDFPageText(nil, 1)
		if err == nil {
			t.Fatal("expected error from nil reader panic, got nil")
		}
		if text != "" {
			t.Errorf("expected empty text on panic, got %q", text)
		}
		if !strings.Contains(err.Error(), "failed to parse PDF page 1") {
			t.Errorf("expected error identifying the page number, got %q", err.Error())
		}
	})
}

// TestExtractPDFTextRecoverConvertsLibraryPanicToError specifically exercises
// the defer-recover branch of extractPDFText by feeding it a PDF whose xref
// is a FlateDecode stream containing non-flate bytes. The library eagerly
// decompresses xref streams during NewReader and panics with "zlib: invalid
// header" when the stream isn't valid flate. Without the recover, that panic
// would propagate out of extractPDFText and crash the calling goroutine.
func TestExtractPDFTextRecoverConvertsLibraryPanicToError(t *testing.T) {
	data := buildXRefStreamPDFWithBadFlate()

	text, err := extractPDFText(data)
	if err == nil {
		t.Fatal("expected error from malformed xref stream, got nil")
	}
	if text != "" {
		t.Errorf("expected empty text on error, got %q", text)
	}
	// Confirm the error came through the recover path rather than a normal
	// library return — that path wraps with "failed to parse PDF".
	if !strings.Contains(err.Error(), "failed to parse PDF") {
		t.Errorf("expected recover-wrapped error containing %q, got %q", "failed to parse PDF", err.Error())
	}
}

// buildXRefStreamPDFWithBadFlate constructs a minimal PDF whose cross-reference
// is stored as a FlateDecode-filtered stream, but the stream bytes are not
// valid flate data. The ledongthuc/pdf library decompresses xref streams
// eagerly in NewReader, so this reliably triggers a "zlib: invalid header"
// panic inside the library — the kind of crash the recover() in
// extractPDFText exists to contain.
func buildXRefStreamPDFWithBadFlate() []byte {
	garbage := []byte{0xFF, 0x00, 0xAA, 0x55}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n%\xe2\xe3\xcf\xd3\n")
	objOff := buf.Len()
	fmt.Fprintf(&buf,
		"1 0 obj\n<< /Type /XRef /Filter /FlateDecode /W [1 2 1] /Size 1 /Root 2 0 R /Length %d >>\nstream\n",
		len(garbage))
	buf.Write(garbage)
	buf.WriteString("\nendstream\nendobj\n")
	buf.WriteString("2 0 obj\n<< /Type /Catalog /Pages 3 0 R >>\nendobj\n")
	buf.WriteString("3 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", objOff)
	return buf.Bytes()
}

func TestReadWebpageToolHandlesPDFContentType(t *testing.T) {
	pdfBytes := buildMinimalPDF("Hello from PDF")

	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/pdf"}},
				Body:       io.NopCloser(bytes.NewReader(pdfBytes)),
				Request:    req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://example.com/report.pdf",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}

	payload := result.Content.(map[string]any)
	content := payload["content"].(string)
	format := payload["format"].(string)

	if format != "text" {
		t.Errorf("expected format 'text', got %q", format)
	}
	if !strings.Contains(content, "Hello from PDF") {
		t.Errorf("expected PDF text in content, got %q", content)
	}
}

func TestReadWebpageToolDetectsPDFByURLExtension(t *testing.T) {
	pdfBytes := buildMinimalPDF("Extension detected PDF")

	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
				Body:       io.NopCloser(bytes.NewReader(pdfBytes)),
				Request:    req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://example.com/docs/whitepaper.pdf",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}

	payload := result.Content.(map[string]any)
	if payload["format"].(string) != "text" {
		t.Errorf("expected format 'text', got %q", payload["format"])
	}
	if !strings.Contains(payload["content"].(string), "Extension detected PDF") {
		t.Errorf("expected PDF text in content, got %q", payload["content"])
	}
}

func TestReadWebpageToolPDFURLExtensionDetection(t *testing.T) {
	cases := []struct {
		name      string
		url       string
		wantPDF   bool
		pdfMarker string
	}{
		{"query string", "https://example.com/docs/whitepaper.pdf?token=abc&v=2", true, "QS PDF"},
		{"fragment", "https://example.com/docs/whitepaper.pdf#page=2", true, "Frag PDF"},
		{"uppercase extension", "https://example.com/docs/WHITEPAPER.PDF", true, "Upper PDF"},
		{"extension mid-path does not match", "https://example.com/foo.pdf/extra", false, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pdfBytes := buildMinimalPDF(tc.pdfMarker)

			w := NewWebReaderTools(nil)
			w.httpClient = &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
						Body:       io.NopCloser(bytes.NewReader(pdfBytes)),
						Request:    req,
					}, nil
				}),
			}

			result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{URL: tc.url})
			if err != nil {
				t.Fatalf("ReadWebpageTool returned error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if tc.wantPDF {
				if !result.Success {
					t.Fatalf("expected success (PDF detected), got %#v", result)
				}
				payload := result.Content.(map[string]any)
				if payload["format"].(string) != "text" {
					t.Errorf("expected format 'text', got %q", payload["format"])
				}
				if !strings.Contains(payload["content"].(string), tc.pdfMarker) {
					t.Errorf("expected PDF text %q in content, got %q", tc.pdfMarker, payload["content"])
				}
			} else {
				if result.Success {
					t.Fatalf("expected failure (no PDF detection), got success: %#v", result)
				}
			}
		})
	}
}

// TestReadWebpageToolPDFSkipsTitleExtraction is a regression guard for the
// !isPDF check that gates extractTitle in ReadWebpageTool. It embeds a literal
// "<title>...</title>" substring inside the PDF content stream so that if the
// guard regresses, extractTitle's regex would happily pull the marker back
// out. Test fails if the returned title is non-empty for a PDF response.
func TestReadWebpageToolPDFSkipsTitleExtraction(t *testing.T) {
	const marker = "<title>SHOULD_NOT_BE_TITLE</title>"
	pdfBytes := buildMinimalPDF(marker)
	if !bytes.Contains(pdfBytes, []byte(marker)) {
		t.Fatalf("test premise broken: PDF bytes do not contain the <title> substring")
	}

	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/pdf"}},
				Body:       io.NopCloser(bytes.NewReader(pdfBytes)),
				Request:    req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://example.com/report.pdf",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success result, got %#v", result)
	}

	payload := result.Content.(map[string]any)
	if title, _ := payload["title"].(string); title != "" {
		t.Errorf("expected empty title for PDF response, got %q — extractTitle guard may have regressed", title)
	}
}

func TestExtractPDFTextReturnsErrorForEmptyPDF(t *testing.T) {
	_, err := extractPDFText([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}

func TestExtractPDFTextRejectsOversizedInput(t *testing.T) {
	oversized := make([]byte, maxPDFBytes+1)
	_, err := extractPDFText(oversized)
	if err == nil {
		t.Fatal("expected error for oversized PDF data")
	}
	if !strings.Contains(err.Error(), "PDF too large") {
		t.Errorf("expected size-limit error, got %v", err)
	}
}

func TestFetchWebpageOnceRejectsOversizedPDFWithoutContentLength(t *testing.T) {
	oversized := make([]byte, maxPDFBytes+1024)
	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/pdf"}},
				ContentLength: -1,
				Body:          io.NopCloser(bytes.NewReader(oversized)),
				Request:       req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://example.com/stream.pdf",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failure result, got %#v", result)
	}
	if !strings.Contains(result.Error, "PDF too large") {
		t.Errorf("expected PDF size-limit error, got %q", result.Error)
	}
}

func TestFetchWebpageOnceRejectsOversizedPDFByContentLength(t *testing.T) {
	w := NewWebReaderTools(nil)
	w.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        http.Header{"Content-Type": []string{"application/pdf"}},
				ContentLength: int64(maxPDFBytes) + 1,
				Body:          io.NopCloser(bytes.NewReader([]byte("ignored"))),
				Request:       req,
			}, nil
		}),
	}

	result, err := w.ReadWebpageTool(context.Background(), ReadWebpageInput{
		URL: "https://example.com/huge.pdf",
	})
	if err != nil {
		t.Fatalf("ReadWebpageTool returned error: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failure result, got %#v", result)
	}
	if !strings.Contains(result.Error, "PDF too large") {
		t.Errorf("expected PDF size-limit error, got %q", result.Error)
	}
}

func TestExtractPDFTextReturnsErrorForZeroPagePDF(t *testing.T) {
	_, err := extractPDFText(buildZeroPagePDF())
	if err == nil {
		t.Fatal("expected error for zero-page PDF")
	}
	if !strings.Contains(err.Error(), "no extractable text") {
		t.Errorf("expected no-extractable-text error, got %v", err)
	}
}

func TestExtractPDFTextLogsAndReturnsPartialWhenSomePagesAreNull(t *testing.T) {
	var (
		text string
		err  error
	)
	logOutput := captureLog(t, func() {
		text, err = extractPDFText(buildMixedPagePDF())
	})
	if err != nil {
		t.Fatalf("extractPDFText returned error: %v", err)
	}
	if !strings.Contains(text, "Good Page") {
		t.Errorf("expected good page content, got %q", text)
	}
	if !strings.Contains(logOutput, "partial extraction") {
		t.Errorf("expected partial extraction log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "1/2 null pages") {
		t.Errorf("expected null-page count in log, got %q", logOutput)
	}
}

func TestExtractPDFTextLogsAndReturnsPartialWhenSomePagesError(t *testing.T) {
	var (
		text string
		err  error
	)
	logOutput := captureLog(t, func() {
		text, err = extractPDFText(buildPDFWithBadContentsPage())
	})
	if err != nil {
		t.Fatalf("extractPDFText returned error: %v", err)
	}
	if !strings.Contains(text, "Good Page") {
		t.Errorf("expected good page content, got %q", text)
	}
	if !strings.Contains(logOutput, "partial extraction") {
		t.Errorf("expected partial extraction log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "1 errored") {
		t.Errorf("expected errored-page count in log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "first error:") {
		t.Errorf("expected first-error detail in log, got %q", logOutput)
	}
}

func TestExtractPDFTextReportsAllPagesErroredInReturnedError(t *testing.T) {
	_, err := extractPDFText(buildAllBadContentsPDF())
	if err == nil {
		t.Fatal("expected error when every page fails")
	}
	msg := err.Error()
	if !strings.Contains(msg, "failed on all 1 pages") {
		t.Errorf("expected all-pages-failed error, got %v", err)
	}
	if !strings.Contains(msg, "1 errored") {
		t.Errorf("expected errored-page count in error, got %v", err)
	}
	if !strings.Contains(msg, "first error:") {
		t.Errorf("expected first-error detail, got %v", err)
	}
}
