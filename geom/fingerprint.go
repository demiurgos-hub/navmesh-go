package geom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
)

// Fingerprint serialization streams the large arrays. Encoding an entire scene
// into one growing JSON buffer otherwise dominates transient geometry memory.
type fingerprintWriter struct {
	h       hash.Hash
	encoder *json.Encoder
	err     error
}

func newFingerprintWriter() *fingerprintWriter {
	h := sha256.New()
	return &fingerprintWriter{h: h, encoder: json.NewEncoder(digestJSON{h})}
}

var fingerprintPunctuation = [...]byte{',', '[', ']', '{', '}'}

func (w *fingerprintWriter) raw(value string) {
	if w.err != nil {
		return
	}
	var data []byte
	switch value {
	case ",":
		data = fingerprintPunctuation[0:1]
	case "[":
		data = fingerprintPunctuation[1:2]
	case "]":
		data = fingerprintPunctuation[2:3]
	case "{":
		data = fingerprintPunctuation[3:4]
	case "}":
		data = fingerprintPunctuation[4:5]
	default:
		data = []byte(value)
	}
	_, w.err = w.h.Write(data)
}
func (w *fingerprintWriter) value(value any) {
	if w.err == nil {
		w.err = w.encoder.Encode(value)
	}
}
func (w *fingerprintWriter) result() (string, error) {
	if w.err != nil {
		return "", w.err
	}
	return hex.EncodeToString(w.h.Sum(nil)), nil
}

func fingerprintArray[T any](w *fingerprintWriter, values []T) {
	if values == nil {
		w.raw("null")
		return
	}
	w.raw("[")
	for i := range values {
		if i > 0 {
			w.raw(",")
		}
		w.value(&values[i])
	}
	w.raw("]")
}

func fingerprintScene(s Scene) (string, error) {
	w := newFingerprintWriter()
	w.raw(`{"Assets":`)
	if s.Assets == nil {
		w.raw("null")
	} else {
		w.raw("[")
		for i := range s.Assets {
			if i > 0 {
				w.raw(",")
			}
			a := &s.Assets[i]
			w.raw(`{"ID":`)
			w.value(a.ID)
			w.raw(`,"Vertices":`)
			fingerprintArray(w, a.Vertices)
			w.raw(`,"Triangles":`)
			fingerprintArray(w, a.Triangles)
			w.raw("}")
		}
		w.raw("]")
	}
	w.raw(`,"Instances":`)
	fingerprintArray(w, s.Instances)
	w.raw(`,"Volumes":`)
	fingerprintArray(w, s.Volumes)
	w.raw(`,"Links":`)
	fingerprintArray(w, s.Links)
	w.raw("}")
	return w.result()
}

func fingerprintResolved(r Resolved) (string, error) {
	w := newFingerprintWriter()
	w.raw(`{"Triangles":`)
	fingerprintArray(w, r.Triangles)
	w.raw(`,"Volumes":`)
	fingerprintArray(w, r.Volumes)
	w.raw(`,"Links":`)
	fingerprintArray(w, r.Links)
	w.raw("}")
	return w.result()
}

type digestJSON struct{ hash.Hash }

func (w digestJSON) Write(p []byte) (int, error) {
	n := len(p)
	// Compact JSON contains no literal linefeeds; Encoder appends one after
	// the value, while the established fingerprint hashes Marshal's bytes.
	if n != 0 && p[n-1] == '\n' {
		p = p[:n-1]
	}
	_, err := w.Hash.Write(p)
	return n, err
}
