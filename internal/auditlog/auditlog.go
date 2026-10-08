// Package auditlog is the audit log's evidence format: events linked into a
// hash chain, written as JSON lines, and verified from such an export
// without the server that wrote it.
//
// A chain shows that an export is consistent: no event in it was changed,
// removed or moved since its hash was computed. It cannot show by itself
// that the log was not rewritten as a whole, its hashes computed anew, as
// the format is public and nothing in it is secret. An anchor does: the hash
// of an event kept outside Agenty, from an earlier export, which a later one
// must still contain.
package auditlog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// tag starts every hashed event, so the encoding can change later.
const tag = "agenty-audit-v1"

// Hash is an event's SHA-256 hash, written as lowercase hex.
type Hash [sha256.Size]byte

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// MarshalText writes the hash as hex.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

// UnmarshalText reads a hash written as hex.
func (h *Hash) UnmarshalText(text []byte) error {
	b, err := hex.DecodeString(string(text))
	if err != nil || len(b) != len(h) {
		return fmt.Errorf("hash %q: not %d bytes of hex", text, len(h))
	}
	copy(h[:], b)
	return nil
}

// Event is one entry of the audit log. ID is its position in the log, from
// 1 without gaps; the first event's PrevHash is zero.
type Event struct {
	ID int64 `json:"id"`
	// RecordedAt is when the event was recorded, to the microsecond.
	RecordedAt time.Time `json:"recorded_at"`
	// Actor is the user who acted; empty for the server itself.
	Actor  string `json:"actor"`
	Action string `json:"action"`
	// Workspace is empty for an event of the whole organisation.
	Workspace string `json:"workspace"`
	// RunID names the run the event is about, if any, and Target what else
	// it acted on, such as a harness.
	RunID  string `json:"run_id"`
	Target string `json:"target"`
	// Details is a JSON object, in its canonical form.
	Details  json.RawMessage `json:"details"`
	PrevHash Hash            `json:"prev_hash"`
	Hash     Hash            `json:"hash"`
}

// Canonical returns the canonical form of a JSON value: as encoding/json
// writes it, compact and with <, >, & and the line and paragraph separators
// escaped. It is its own canonical form, so a canonical value written to an
// export reads back the same.
func Canonical(value []byte) ([]byte, error) {
	out, err := json.Marshal(json.RawMessage(value))
	if err != nil {
		return nil, fmt.Errorf("auditlog: %w", err)
	}
	return out, nil
}

// Sum returns the event's hash, from its fields and PrevHash; it ignores
// Hash. Details must be canonical.
func (e Event) Sum() Hash {
	h := sha256.New()
	field := func(b []byte) {
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(b))))
		h.Write(b)
	}
	number := func(n int64) {
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(n)))
	}
	field([]byte(tag))
	h.Write(e.PrevHash[:])
	number(e.ID)
	number(e.RecordedAt.UnixMicro())
	for _, s := range []string{e.Actor, e.Action, e.Workspace, e.RunID, e.Target} {
		field([]byte(s))
	}
	field(e.Details)
	var out Hash
	h.Sum(out[:0])
	return out
}

// WriteLine writes the event as one line of an export.
func WriteLine(w io.Writer, e Event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("auditlog: event %d: %w", e.ID, err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("auditlog: %w", err)
	}
	return nil
}

// Anchor is an event's hash kept apart from the log, which an export must
// agree with.
type Anchor struct {
	ID   int64
	Hash Hash
}

// ParseAnchor reads an anchor written as ID:HASH.
func ParseAnchor(s string) (Anchor, error) {
	id, hash, ok := strings.Cut(s, ":")
	n, err := strconv.ParseInt(id, 10, 64)
	if !ok || err != nil || n < 1 {
		return Anchor{}, fmt.Errorf("anchor %q: not ID:HASH", s)
	}
	a := Anchor{ID: n}
	if err := a.Hash.UnmarshalText([]byte(hash)); err != nil {
		return Anchor{}, fmt.Errorf("anchor %q: %w", s, err)
	}
	return a, nil
}

func (a Anchor) String() string {
	return strconv.FormatInt(a.ID, 10) + ":" + a.Hash.String()
}

// Summary describes a verified export: how many events it holds, the ids of
// its first and last, and the last one's hash, the anchor to keep.
type Summary struct {
	Events      int
	First, Last int64
	LastHash    Hash
}

// Verify reads an export and checks that every event's hash is its own, that
// each links to the one before it, that ids rise by one, and that an export
// from the first event starts at a zero hash. Each anchor must be one of its
// events, or the one just before its first. It reads the export as a stream.
func Verify(r io.Reader, anchors ...Anchor) (Summary, error) {
	var s Summary
	var first Event
	pending := map[int64]Hash{}
	for _, a := range anchors {
		pending[a.ID] = a.Hash
	}
	lines := bufio.NewScanner(r)
	// An event holds a tool's arguments and result, which may be large.
	lines.Buffer(nil, 64<<20)
	for n := 1; lines.Scan(); n++ {
		var e Event
		decoder := json.NewDecoder(bytes.NewReader(lines.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&e); err != nil {
			return Summary{}, fmt.Errorf("auditlog: line %d: %w", n, err)
		}
		if err := s.next(e); err != nil {
			return Summary{}, err
		}
		if s.Events == 1 {
			first = e
		}
		if want, ok := pending[e.ID]; ok {
			if want != e.Hash {
				return Summary{}, fmt.Errorf("auditlog: anchor %d: the export's event %d has another hash", e.ID, e.ID)
			}
			delete(pending, e.ID)
		}
	}
	if err := lines.Err(); err != nil {
		return Summary{}, fmt.Errorf("auditlog: %w", err)
	}
	if s.Events == 0 {
		return Summary{}, errors.New("auditlog: the export holds no events")
	}
	if want, ok := pending[first.ID-1]; ok {
		if want != first.PrevHash {
			return Summary{}, fmt.Errorf("auditlog: anchor %d: event %d does not follow it", first.ID-1, first.ID)
		}
		delete(pending, first.ID-1)
	}
	for id := range pending {
		return Summary{}, fmt.Errorf("auditlog: anchor %d: not in the export, which holds events %d to %d", id, s.First, s.Last)
	}
	return s, nil
}

// next checks e, the event after those s summarises, and adds it.
func (s *Summary) next(e Event) error {
	switch {
	case s.Events == 0 && e.ID == 1 && e.PrevHash != (Hash{}):
		return fmt.Errorf("auditlog: event 1: the first event follows no other")
	case s.Events > 0 && e.ID != s.Last+1:
		return fmt.Errorf("auditlog: event %d: follows event %d", e.ID, s.Last)
	case s.Events > 0 && e.PrevHash != s.LastHash:
		return fmt.Errorf("auditlog: event %d: does not link to event %d", e.ID, s.Last)
	case e.Sum() != e.Hash:
		return fmt.Errorf("auditlog: event %d: its hash is not its own", e.ID)
	}
	if s.Events == 0 {
		s.First = e.ID
	}
	s.Events++
	s.Last = e.ID
	s.LastHash = e.Hash
	return nil
}
