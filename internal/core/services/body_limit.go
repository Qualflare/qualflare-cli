package services

import (
	"encoding/json"
	"fmt"
	"sort"

	"qualflare-cli/internal/core/domain"
)

// maxCollectBodyBytes is the largest /collect body the CLI will send.
//
// The server rejects anything over 10 MB with a bare 413 before parsing, which
// loses the entire launch. 9.5 MB (decimal) leaves 500 KB of headroom: it sits
// under the limit whether the server counts 10 MB as 10,000,000 or 10 MiB, and
// absorbs the bytes this measurement cannot see — HTTP framing, and any proxy
// in front of the server that counts the limit slightly differently.
const maxCollectBodyBytes = 9_500_000

// countingWriter counts bytes without keeping them, so measuring a ~10 MB body
// does not allocate a second copy of it.
type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}

// collectBodySize is the exact byte length of the /collect request body for
// launch.
//
// It must match what the HTTP adapter sends. SendReport passes the *Launch to
// resty's SetBody with no Content-Type override, no custom encoder and no
// request compression, so resty serialises it with its default JSON encoder:
// json.NewEncoder(w) with SetEscapeHTML(true), then Encode — the json.Marshal
// bytes plus one trailing newline, sent uncompressed. The same encoder here
// produces the same bytes. client_test.go's
// TestSendReport_BodyIsUncompressedJSONEncoding pins the adapter side, so a
// change there (gzip, a different encoder) fails a test rather than silently
// making this measurement wrong.
func collectBodySize(launch *domain.Launch) (int, error) {
	var w countingWriter
	enc := json.NewEncoder(&w)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(launch); err != nil {
		return 0, err
	}
	return w.n, nil
}

// inlineCandidate is one attachment still carrying base64 content in the body.
type inlineCandidate struct {
	suite, kase, att int
	name, caseName   string
	contentLen       int
	isImage          bool
	// saving is a lower bound on how much removing this attachment shrinks the
	// body: its own encoded JSON object. The real saving is at least this — plus
	// a separating comma, or the whole `"attachments":[...]` key when it was the
	// case's last one — so subtracting it never under-estimates the new size.
	saving int
}

// enforceBodyLimit keeps the /collect body under maxCollectBodyBytes.
//
// It runs after offloadInlineAttachments, so whatever is still inline either
// has a type the upload endpoint does not accept (JSON, logs, markdown) or is an
// image whose upload failed. Losing some of those beats losing the whole launch
// to a 413, so while the body is over the limit it drops inline attachments:
// every non-image before any image (an image the user can look at is the more
// valuable thing to keep), and within each group the largest first, so the
// fewest attachments are lost. Every drop is reported.
//
// A dropped attachment is removed outright rather than left as an empty
// placeholder: the server persists a row from Name alone, and an undownloadable
// row says less than the warning does.
//
// If the body is still over the limit with nothing inline left to drop, the
// results themselves are too large; that is an error before sending, not a 413
// after.
func (s *ReportService) enforceBodyLimit(launch *domain.Launch) error {
	size, err := collectBodySize(launch)
	if err != nil {
		return fmt.Errorf("failed to measure the request body: %w", err)
	}

	var dropped []inlineCandidate
	for size > maxCollectBodyBytes {
		candidates, err := inlineCandidates(launch)
		if err != nil {
			return fmt.Errorf("failed to measure the request body: %w", err)
		}
		if len(candidates) == 0 {
			break
		}

		// Drop until a conservative estimate fits. Because each saving is a lower
		// bound, an estimate at or under the limit means the real size is too;
		// the re-measure below confirms it exactly.
		estimate := size
		var batch []inlineCandidate
		for _, c := range candidates {
			if estimate <= maxCollectBodyBytes {
				break
			}
			batch = append(batch, c)
			estimate -= c.saving
		}
		removeAttachments(launch, batch)
		dropped = append(dropped, batch...)

		if size, err = collectBodySize(launch); err != nil {
			return fmt.Errorf("failed to measure the request body: %w", err)
		}
	}

	s.warnDroppedAttachments(dropped)

	if size > maxCollectBodyBytes {
		return fmt.Errorf("the results are %s, over /collect's 10 MB limit even without attachments; "+
			"split the upload, e.g. collect fewer files per run", humanBytes(size))
	}
	return nil
}

// inlineCandidates lists every attachment still carrying content and no
// storage key, in drop order: non-images before images, then largest content
// first. The sort is stable, so equal sizes keep report order.
func inlineCandidates(launch *domain.Launch) ([]inlineCandidate, error) {
	var out []inlineCandidate
	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			c := &launch.Suites[i].Cases[j]
			for k := range c.Attachments {
				a := &c.Attachments[k]
				if a.Content == "" || a.StorageKey != "" {
					continue
				}
				encoded, err := json.Marshal(a)
				if err != nil {
					return nil, err
				}
				_, isImage := offloadableExtensions[a.MimeType]
				out = append(out, inlineCandidate{
					suite: i, kase: j, att: k,
					name: a.Name, caseName: c.Name,
					contentLen: len(a.Content),
					isImage:    isImage,
					saving:     len(encoded),
				})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].isImage != out[b].isImage {
			return !out[a].isImage
		}
		return out[a].contentLen > out[b].contentLen
	})
	return out, nil
}

// removeAttachments deletes the given attachments. Each touched case gets a
// fresh slice rather than an in-place filter, because a parser may hand back
// cases whose attachment slices share a backing array with another file's.
func removeAttachments(launch *domain.Launch, drop []inlineCandidate) {
	type caseKey struct{ suite, kase int }
	byCase := make(map[caseKey]map[int]bool)
	for _, d := range drop {
		key := caseKey{d.suite, d.kase}
		if byCase[key] == nil {
			byCase[key] = make(map[int]bool)
		}
		byCase[key][d.att] = true
	}
	for key, idx := range byCase {
		c := &launch.Suites[key.suite].Cases[key.kase]
		kept := make([]domain.Attachment, 0, len(c.Attachments)-len(idx))
		for k, a := range c.Attachments {
			if !idx[k] {
				kept = append(kept, a)
			}
		}
		if len(kept) == 0 {
			kept = nil
		}
		c.Attachments = kept
	}
}

func (s *ReportService) warnDroppedAttachments(dropped []inlineCandidate) {
	if len(dropped) == 0 {
		return
	}
	total := 0
	for _, d := range dropped {
		total += d.contentLen
		fmt.Fprintf(s.warnWriter(),
			"warning: dropped attachment %q (%s) from %q to keep the upload under the server's 10 MB limit\n",
			d.name, humanBytes(d.contentLen), d.caseName)
	}
	fmt.Fprintf(s.warnWriter(),
		"warning: dropped %d inline attachment(s) totalling %s to keep the upload under the server's 10 MB limit\n",
		len(dropped), humanBytes(total))
}

// humanBytes formats n in decimal units, matching how the server's "10 MB"
// limit and maxCollectBodyBytes are stated.
func humanBytes(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1f KB", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d B", n)
	}
}
