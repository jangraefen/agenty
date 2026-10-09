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
//
// # Role in the architecture
//
// The package is the one definition of an event and its hash, shared by the
// side that writes the log and the side that checks it, so the two cannot
// drift apart. Package store appends events: it makes their details
// Canonical and computes each Hash with Sum while it holds the log's lock.
// The API server streams an export with WriteLine. `agenty audit verify`
// (package cli) checks an export with Verify, offline, against anchors read
// with ParseAnchor, and prints the last event's anchor to keep. Nothing here
// touches a database or the network, so an auditor can verify an export
// with the binary alone, far from the server that wrote it.
//
// # What it contains
//
//   - Event and Hash: an entry of the log and its SHA-256 hash.
//   - Canonical and Event.Sum: the bytes that are hashed.
//   - WriteLine: the export format, one JSON object per line.
//   - Anchor, ParseAnchor, Verify and Summary: checking an export.
//
// It upholds trust-model guarantee 9 (the audit log is tamper-evident) as
// far as a format can: a change to any event breaks every link after it,
// unless all of them are computed anew, which an anchor kept elsewhere
// then shows.
package auditlog

import (
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

// tag starts every hashed event, so the encoding can change later: a new
// encoding gets a new tag, and a hash of one can never be mistaken for a
// hash of the other.
const tag = "agenty-audit-v1"

// Hash is an event's SHA-256 hash, written as lowercase hex. It is a fixed
// array rather than a slice, so hashes compare with == and a zero Hash is
// the previous hash of the first event.
type Hash [sha256.Size]byte

// String writes the hash as lowercase hex, as exports and anchors show it.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// MarshalText writes the hash as hex.
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

// UnmarshalText reads a hash written as hex. Anything but exactly 32 bytes
// of hex is an error, so a truncated hash in an export or anchor is caught
// rather than padded with zeros.
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
//
// Its JSON form is one line of an export; Verify refuses unknown fields, so
// an export carries nothing the hash does not cover except Hash itself.
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
	Details json.RawMessage `json:"details"`
	// PrevHash is the hash of the event before, which links the chain, and
	// Hash this event's own, as Sum computes it.
	PrevHash Hash `json:"prev_hash"`
	Hash     Hash `json:"hash"`
}

// Canonical returns the canonical form of a JSON value: as encoding/json
// writes it, compact and with <, >, & and the line and paragraph separators
// escaped. It is its own canonical form, so a canonical value written to an
// export reads back the same.
//
// The hash covers details as bytes, so the bytes written to the database,
// the export and the verifier must agree. Re-encoding a value through
// encoding/json compacts it while keeping key order and every value as
// written, and an already canonical value comes out unchanged, which makes
// the form stable however often an event is read and written.
func Canonical(value []byte) ([]byte, error) {
	out, err := json.Marshal(json.RawMessage(value))
	if err != nil {
		return nil, fmt.Errorf("auditlog: %w", err)
	}
	return out, nil
}

// Sum returns the event's hash, from its fields and PrevHash; it ignores
// Hash. Details must be canonical.
//
// The encoding is unambiguous: every variable-length field is prefixed with
// its length, and numbers are fixed-width, so no two different events
// encode to the same bytes, as they could if fields were only joined. The
// time is hashed as microseconds since the epoch, the precision PostgreSQL
// keeps, so a stored event hashes the same once read back.
func (e Event) Sum() Hash {
	h := sha256.New()
	field := func(b []byte) {
		h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(b))))
		h.Write(b)
	}
	number := func(n int64) {
		//nolint:gosec // G115: what is hashed is the number's two's-complement bits, for any value.
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

// WriteLine writes the event as one line of an export. encoding/json
// escapes newlines inside strings, so an event is always exactly one line.
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
// agree with. Keeping the latest anchor outside Agenty is what turns the
// chain into evidence: a rewrite of the log up to it no longer verifies.
type Anchor struct {
	ID   int64
	Hash Hash
}

// ParseAnchor reads an anchor written as ID:HASH, as String writes it and
// `agenty audit verify` prints it.
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

// String writes the anchor as ID:HASH.
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
// each links to the one before it, that ids rise by one from at least 1, and
// that an export from the first event starts at a zero hash. Each anchor
// must be one of its events, or the one just before its first. It reads the
// export as a stream, so an event of any size verifies.
//
// An anchor may be the event just before the export's first, as an export
// that continues from a previous one, starting after its last event, does
// not repeat that event; it is then checked against the first event's
// PrevHash.
func Verify(r io.Reader, anchors ...Anchor) (Summary, error) {
	// Anchors not yet matched, by id. Each one found is removed; any left at
	// the end is not in the export, which is an error, not a pass.
	pending := map[int64]Hash{}
	for _, a := range anchors {
		if h, ok := pending[a.ID]; ok && h != a.Hash {
			return Summary{}, fmt.Errorf("auditlog: anchor %d: given twice, with different hashes", a.ID)
		}
		pending[a.ID] = a.Hash
	}
	// Decode event by event rather than reading lines, so no line length
	// limit applies; unknown fields are refused, as the hash would not cover
	// them.
	var s Summary
	var first Event
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	for {
		var e Event
		err := decoder.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Summary{}, fmt.Errorf("auditlog: entry %d: %w", s.Events+1, err)
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
	// An empty export proves nothing, so it does not verify.
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

// next checks e, the event after those s summarises, and adds it. The
// checks run in order from the cheapest; the hash is checked last, so a
// broken link is reported as such rather than as a wrong hash.
func (s *Summary) next(e Event) error {
	switch {
	case e.ID < 1:
		return fmt.Errorf("auditlog: event %d: ids start at 1", e.ID)
	case e.RecordedAt.Truncate(time.Microsecond) != e.RecordedAt:
		return fmt.Errorf("auditlog: event %d: its time is finer than the microseconds its hash covers", e.ID)
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
