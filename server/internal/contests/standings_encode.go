package contests

import (
	"encoding/json"
	"strconv"
)

// Standings encoded once per board version (PODIUM_STANDINGS_ENCODE_ONCE).
//
// A board's labels and rows are the same for every viewer of one version, and
// they are most of the bytes of a standings response. With the setting on,
// the cached board keeps them as JSON, built the first time a response
// needs them, and each response splices them between its own fields: the
// variant and version (shared), the frozen time, the final flag and the
// judging count (which differ by variant and viewer kind), the viewer's own
// id, and the server's clock. The cached bytes belong to one Standings, and
// a new board version is a new Standings, so a version change drops them
// with the board it replaces; there is no key to invalidate. They are only
// read after they are built, and every response copies them into a buffer of
// its own.

// encoded returns `"labels":[...],"rows":[...]` for the board, or nil when
// it cannot be encoded.
func (b *Standings) encoded() []byte {
	b.encOnce.Do(func() {
		rows := b.Rows
		if rows == nil {
			rows = []Row{} // the response shows an empty board as [], not null
		}
		labels, err := json.Marshal(b.Labels)
		if err != nil {
			return
		}
		rowsJSON, err := json.Marshal(rows)
		if err != nil {
			return
		}
		enc := make([]byte, 0, len(labels)+len(rowsJSON)+20)
		enc = append(enc, `"labels":`...)
		enc = append(enc, labels...)
		enc = append(enc, `,"rows":`...)
		enc = append(enc, rowsJSON...)
		b.enc = enc
	})
	return b.enc
}

// splice returns the response body for o, the same bytes json.Encoder gives
// for it, using the board's encoded labels and rows. It returns false when
// it cannot, and the caller encodes o as before. The field order is the
// order of standingsJSON.
func (o *standingsJSON) splice() ([]byte, bool) {
	if o.board == nil || (o.Variant != VariantFull && o.Variant != VariantFrozen) {
		return nil, false
	}
	enc := o.board.encoded()
	if enc == nil {
		return nil, false
	}
	b := make([]byte, 0, len(enc)+160)
	b = append(b, `{"variant":"`...)
	b = append(b, o.Variant...) // one of the two constants above, nothing to escape
	b = append(b, `","version":`...)
	b = strconv.AppendInt(b, o.Version, 10)
	b = append(b, `,"frozen_at":`...)
	if o.FrozenAt == nil {
		b = append(b, "null"...)
	} else {
		b = strconv.AppendInt(b, *o.FrozenAt, 10)
	}
	b = append(b, `,"final":`...)
	b = strconv.AppendBool(b, o.Final)
	b = append(b, `,"judging_remaining":`...)
	b = strconv.AppendInt(b, int64(o.JudgingRemaining), 10)
	b = append(b, ',')
	b = append(b, enc...)
	b = append(b, `,"me":`...)
	b = strconv.AppendInt(b, o.Me, 10)
	b = append(b, `,"server_now":`...)
	b = strconv.AppendInt(b, o.ServerNow, 10)
	return append(b, "}\n"...), true
}
