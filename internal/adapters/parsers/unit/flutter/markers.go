package flutter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// The qualflare_flutter package prints one marker line per API call from
// inside the running test, which the JSON reporter records as that test's
// print output:
//
//	##qualflare[v1] {"k":"label","name":"feature","value":"checkout"}
//
// The parser turns them into case metadata and keeps them out of the output.
const (
	markerPrefix = "##qualflare[v1] "
	// markerFamily starts every marker line, whatever its protocol version.
	markerFamily = "##qualflare["

	// Attachment caps in decoded bytes (at the cap is allowed), the same ones
	// the package enforces; the per-test cap covers all attempts together.
	maxAttachmentBytes     = 5 * 1024 * 1024
	maxTestAttachmentBytes = 20 * 1024 * 1024
	// maxMarkerTextRunes bounds a name, value, URL, warning or step error.
	maxMarkerTextRunes = 8192

	stepUnfinishedMessage = "step did not finish"
	defaultAttachmentMIME = "application/octet-stream"
)

// offloadableMIME is the set the upload offloads out of the report body
// (offloadableExtensions in internal/core/services/report_service.go). Any
// other attachment travels base64 inside the body and counts against the
// run's inline budget.
var offloadableMIME = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
}

// inlineBudget is the run-wide total of attachment bytes that stay inline in
// the report body, shared by every case of one parse.
type inlineBudget struct{ used int }

// admit reports why an attachment of size bytes cannot stay inline, or ""
// when it can, in which case it is counted. Offloadable images always pass.
func (b *inlineBudget) admit(mime string, size int) string {
	switch {
	case offloadableMIME[mime]:
		return ""
	case size > base.MaxInlineAttachmentBytes:
		return fmt.Sprintf("over the %d-byte limit for an attachment sent inline (only PNG, JPEG and GIF are uploaded separately)", base.MaxInlineAttachmentBytes)
	case b.used+size > base.MaxInlineTotalBytes:
		return fmt.Sprintf("would take this run's inline attachments past %d bytes", base.MaxInlineTotalBytes)
	}
	b.used += size
	return ""
}

// markerSet is what one attempt's marker lines amount to.
type markerSet struct {
	labels      []domain.Label
	links       []domain.Link
	tags        []string
	priority    domain.Severity
	steps       []domain.Step
	attachments []domain.Attachment
	warnings    []string
}

// isMarkerLine reports whether a print is a marker line of any version.
func isMarkerLine(msg string) bool {
	return strings.HasPrefix(msg, markerFamily)
}

// rawMarker is the union of every marker kind's fields. id is an int for
// steps and a string for attachments, so it is decoded per kind.
type rawMarker struct {
	K      string          `json:"k"`
	ID     json.RawMessage `json:"id"`
	Parent *int            `json:"parent"`
	Name   string          `json:"name"`
	Value  string          `json:"value"`
	Type   string          `json:"type"`
	URL    string          `json:"url"`
	Tags   []string        `json:"tags"`
	T      *int64          `json:"t"`
	Status string          `json:"status"`
	Error  string          `json:"error"`
	N      *int            `json:"n"`
	I      *int            `json:"i"`
	Data   string          `json:"data"`
	Msg    string          `json:"msg"`
}

// openStep is a started step: its index in the steps slice and start time.
type openStep struct {
	index int
	start *int64
}

// pendingAttachment collects one attachment's chunks.
type pendingAttachment struct {
	name, mime string
	n          int
	chunks     map[int]string
}

// markerReader accumulates one attempt's markers.
type markerReader struct {
	set         markerSet
	malformed   int
	badVersion  int
	unknownKind []string
	seenWarn    map[string]bool
	steps       map[int]openStep
	ended       map[int]bool
	attOrder    []string
	atts        map[string]*pendingAttachment
}

// extractMarkers reads the marker lines out of one attempt's prints. rest is
// prints without them, and prints itself when there are none. Every problem
// (malformed JSON, an unknown kind, another protocol version, a dropped
// attachment) becomes one warning.
func extractMarkers(prints []string) (set markerSet, rest []string) {
	r := markerReader{
		seenWarn: map[string]bool{},
		steps:    map[int]openStep{},
		ended:    map[int]bool{},
		atts:     map[string]*pendingAttachment{},
	}
	sawMarker := false
	for i, p := range prints {
		if !isMarkerLine(p) {
			if sawMarker {
				rest = append(rest, p)
			}
			continue
		}
		if !sawMarker {
			sawMarker = true
			rest = append(make([]string, 0, len(prints)-1), prints[:i]...)
		}
		r.read(p)
	}
	if !sawMarker {
		return markerSet{}, prints
	}
	r.finish()
	return r.set, rest
}

func (r *markerReader) warn(msg string) {
	if r.seenWarn[msg] {
		return
	}
	r.seenWarn[msg] = true
	r.set.warnings = append(r.set.warnings, msg)
}

func (r *markerReader) read(line string) {
	body, ok := strings.CutPrefix(line, markerPrefix)
	if !ok {
		if strings.HasPrefix(line, strings.TrimSpace(markerPrefix)) {
			r.malformed++ // v1 without the separating space
		} else {
			r.badVersion++
		}
		return
	}
	var m rawMarker
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		r.malformed++
		return
	}
	switch m.K {
	case "label":
		r.label(m)
	case "link":
		r.link(m)
	case "tag":
		r.tag(m)
	case "priority":
		r.priority(m)
	case "step+":
		r.stepStart(m)
	case "step-":
		r.stepEnd(m)
	case "att":
		r.chunk(m)
	case "warn":
		if m.Msg == "" {
			r.malformed++
			return
		}
		r.warn(capRunes(m.Msg, maxMarkerTextRunes))
	case "":
		r.malformed++
	default:
		kind := capRunes(m.K, 64)
		for _, k := range r.unknownKind {
			if k == kind {
				return
			}
		}
		r.unknownKind = append(r.unknownKind, kind)
	}
}

func (r *markerReader) label(m rawMarker) {
	if m.Name == "" || m.Value == "" {
		r.malformed++
		return
	}
	r.set.labels = appendLabel(r.set.labels, domain.Label{
		Name:  capRunes(m.Name, maxMarkerTextRunes),
		Value: capRunes(m.Value, maxMarkerTextRunes),
	})
}

func (r *markerReader) link(m rawMarker) {
	if m.URL == "" {
		r.malformed++
		return
	}
	url := capRunes(m.URL, maxMarkerTextRunes)
	switch m.Type {
	case "issue", "tms", "custom":
	default:
		r.warn(fmt.Sprintf("link type %q is not one of issue, tms, custom; link %s ignored", capRunes(m.Type, 64), url))
		return
	}
	r.set.links = appendLink(r.set.links, domain.Link{Type: m.Type, Name: capRunes(m.Name, maxMarkerTextRunes), URL: url})
}

func (r *markerReader) tag(m rawMarker) {
	if len(m.Tags) == 0 {
		r.malformed++
		return
	}
	for _, t := range m.Tags {
		if t != "" {
			r.set.tags = appendTag(r.set.tags, capRunes(t, maxMarkerTextRunes))
		}
	}
}

func (r *markerReader) priority(m rawMarker) {
	switch v := domain.Severity(m.Value); v {
	case domain.SeverityLow, domain.SeverityMedium, domain.SeverityHigh, domain.SeverityCritical:
		r.set.priority = v
	default:
		r.warn(fmt.Sprintf("priority %q is not one of low, medium, high, critical; ignored", capRunes(m.Value, 64)))
	}
}

// stepStart opens a step. It counts as an error that did not finish until its
// step- arrives.
func (r *markerReader) stepStart(m rawMarker) {
	var id int
	if json.Unmarshal(m.ID, &id) != nil || m.Name == "" {
		r.malformed++
		return
	}
	if _, dup := r.steps[id]; dup {
		r.malformed++
		return
	}
	s := domain.Step{Name: capRunes(m.Name, maxMarkerTextRunes), Status: domain.StatusError, Error: stepUnfinishedMessage}
	if m.Parent != nil {
		if parent, ok := r.steps[*m.Parent]; ok {
			s.ParentIndex = domain.IntPtr(parent.index)
		}
	}
	r.steps[id] = openStep{index: len(r.set.steps), start: m.T}
	r.set.steps = append(r.set.steps, s)
}

func (r *markerReader) stepEnd(m rawMarker) {
	var id int
	if json.Unmarshal(m.ID, &id) != nil {
		r.malformed++
		return
	}
	open, ok := r.steps[id]
	if !ok || r.ended[id] {
		r.malformed++
		return
	}
	r.ended[id] = true
	s := &r.set.steps[open.index]
	switch m.Status {
	case "passed":
		s.Status, s.Error = domain.StatusPassed, ""
	case "failed":
		s.Status, s.Error = domain.StatusFailed, capRunes(m.Error, maxMarkerTextRunes)
	default:
		s.Status, s.Error = domain.StatusError, capRunes(m.Error, maxMarkerTextRunes)
	}
	if open.start != nil && m.T != nil && *m.T > *open.start {
		s.Duration = time.Duration(*m.T-*open.start) * time.Millisecond
	}
}

func (r *markerReader) chunk(m rawMarker) {
	var id string
	if json.Unmarshal(m.ID, &id) != nil || id == "" || m.N == nil || m.I == nil || *m.N < 1 || *m.I < 0 || *m.I >= *m.N {
		r.malformed++
		return
	}
	a := r.atts[id]
	if a == nil {
		name := capRunes(m.Name, maxMarkerTextRunes)
		if name == "" {
			name = "attachment"
		}
		mime := m.Type
		if mime == "" {
			mime = defaultAttachmentMIME
		}
		a = &pendingAttachment{name: name, mime: mime, n: *m.N, chunks: map[int]string{}}
		r.atts[id] = a
		r.attOrder = append(r.attOrder, id)
	}
	if _, dup := a.chunks[*m.I]; dup || *m.N != a.n {
		r.malformed++
		return
	}
	a.chunks[*m.I] = m.Data
}

// finish assembles the attachments, in order of their first chunk, and turns
// the counted problems into one warning each.
func (r *markerReader) finish() {
	total := 0
	for _, id := range r.attOrder {
		a := r.atts[id]
		if len(a.chunks) < a.n {
			r.warn(fmt.Sprintf("attachment %q is missing chunks (%d of %d arrived); dropped", a.name, len(a.chunks), a.n))
			continue
		}
		// The package splits one base64 string, so the chunks join back into it.
		var b strings.Builder
		for i := range a.n {
			b.WriteString(a.chunks[i])
		}
		content := b.String()
		raw, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			r.warn(fmt.Sprintf("attachment %q is not valid base64; dropped", a.name))
			continue
		}
		if len(raw) > maxAttachmentBytes {
			r.warn(fmt.Sprintf("attachment %q is %d bytes, over the %d-byte cap; dropped", a.name, len(raw), maxAttachmentBytes))
			continue
		}
		if total+len(raw) > maxTestAttachmentBytes {
			r.warn(testCapWarning(a.name))
			continue
		}
		total += len(raw)
		r.set.attachments = append(r.set.attachments, domain.Attachment{Name: a.name, MimeType: a.mime, Content: content})
	}
	if r.malformed > 0 {
		r.warn(fmt.Sprintf("skipped %d malformed marker %s", r.malformed, plural(r.malformed, "line", "lines")))
	}
	for _, k := range r.unknownKind {
		r.warn(fmt.Sprintf("skipped markers of unknown kind %q", k))
	}
	if r.badVersion > 0 {
		r.warn(fmt.Sprintf("skipped %d marker %s of an unsupported protocol version (this qf reads v1)",
			r.badVersion, plural(r.badVersion, "line", "lines")))
	}
}

func testCapWarning(name string) string {
	return fmt.Sprintf("attachment %q would take this test past %d bytes of attachments; dropped", name, maxTestAttachmentBytes)
}

// applyMarkers merges every attempt's markers into the case, in attempt order,
// and returns the warnings for its output. Labels, links and tags are
// deduplicated across attempts and the last priority wins; steps come from the
// final attempt; attachments come from every attempt, an earlier attempt's
// renamed `attempt <n>: <name>`, within the per-test cap and the run's inline
// budget.
func applyMarkers(c *domain.Case, sets []markerSet, budget *inlineBudget) []string {
	var warnings []string
	seen := map[string]bool{}
	total := 0
	for i, s := range sets {
		for _, l := range s.labels {
			c.Labels = appendLabel(c.Labels, l)
		}
		for _, l := range s.links {
			c.Links = appendLink(c.Links, l)
		}
		for _, t := range s.tags {
			c.Tags = appendTag(c.Tags, t)
		}
		if s.priority != "" {
			c.Priority = s.priority
		}
		final := i == len(sets)-1
		if final {
			c.Steps = s.steps
		}
		pending := s.warnings
		for _, a := range s.attachments {
			if !final {
				a.Name = "attempt " + strconv.Itoa(i+1) + ": " + a.Name
			}
			size := decodedSize(a.Content)
			if total+size > maxTestAttachmentBytes {
				pending = append(pending, testCapWarning(a.Name))
				continue
			}
			if why := budget.admit(a.MimeType, size); why != "" {
				pending = append(pending, fmt.Sprintf("attachment %q (%d bytes, %s) dropped: %s", a.Name, size, a.MimeType, why))
				continue
			}
			total += size
			c.Attachments = append(c.Attachments, a)
		}
		for _, w := range pending {
			if !seen[w] {
				seen[w] = true
				warnings = append(warnings, w)
			}
		}
	}
	return warnings
}

// decodedSize is the byte length of valid, padded standard base64.
func decodedSize(b64 string) int {
	return len(b64)/4*3 - (len(b64) - len(strings.TrimRight(b64, "=")))
}

func appendLabel(labels []domain.Label, l domain.Label) []domain.Label {
	for _, have := range labels {
		if have.Name == l.Name && have.Value == l.Value {
			return labels
		}
	}
	return append(labels, l)
}

func appendLink(links []domain.Link, l domain.Link) []domain.Link {
	for _, have := range links {
		if have.Type == l.Type && have.URL == l.URL {
			return links
		}
	}
	return append(links, l)
}

func appendTag(tags []string, t string) []string {
	for _, have := range tags {
		if have == t {
			return tags
		}
	}
	return append(tags, t)
}

// capRunes bounds s to maxRunes runes without splitting one.
func capRunes(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			return s[:i]
		}
		n++
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
