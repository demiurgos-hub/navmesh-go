package geom

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"math"
)

// The resolved fingerprint is SHA-256 over this tag, one record per object
// with valid triangles (0x01, uint64 ID length, ID, uint64 triangle count,
// object digest), 0x00, then canonical JSON {"Volumes":…,"Links":…}. An object
// digest is SHA-256 over 81-byte triangle records: nine binary64 vertex
// coordinates, area, then uint64 source triangle index, all little-endian.
// Fixed binary records keep hashing far cheaper than reflective encoding.
const resolvedFingerprintTag = "navmesh-go resolved fingerprint v2\x00"

const resolvedTriangleRecord = 81

// resolvedDigest computes the resolved fingerprint from triangles in Resolve's
// order, where each object's triangles are contiguous. Allocation is bounded by
// the object count, not the triangle count.
type resolvedDigest struct {
	writer  *fingerprintWriter
	object  hash.Hash
	id      string
	count   uint64
	open    bool
	records []byte
	scratch []byte
	// closed receives each completed object's digest.
	closed func(id string, signature [32]byte)
}

func newResolvedDigest(closed func(string, [32]byte)) *resolvedDigest {
	d := &resolvedDigest{writer: newFingerprintWriter(), object: sha256.New(), records: make([]byte, 0, 64*resolvedTriangleRecord), closed: closed}
	d.writer.raw(resolvedFingerprintTag)
	return d
}

func (d *resolvedDigest) triangle(triangle *ResolvedTriangle) {
	if !d.open || triangle.ObjectID != d.id {
		d.closeObject()
		d.id, d.count, d.open = triangle.ObjectID, 0, true
	}
	d.count++
	if len(d.records)+resolvedTriangleRecord > cap(d.records) {
		d.flush()
	}
	for _, p := range triangle.Vertices {
		d.records = binary.LittleEndian.AppendUint64(d.records, math.Float64bits(p.X))
		d.records = binary.LittleEndian.AppendUint64(d.records, math.Float64bits(p.Y))
		d.records = binary.LittleEndian.AppendUint64(d.records, math.Float64bits(p.Z))
	}
	d.records = append(d.records, triangle.Area)
	d.records = binary.LittleEndian.AppendUint64(d.records, uint64(triangle.SourceTriangle))
}

func (d *resolvedDigest) flush() {
	d.object.Write(d.records)
	d.records = d.records[:0]
}

func (d *resolvedDigest) closeObject() {
	if !d.open {
		return
	}
	d.flush()
	var signature [32]byte
	d.object.Sum(signature[:0])
	d.object.Reset()
	d.scratch = append(d.scratch[:0], 1)
	d.scratch = binary.LittleEndian.AppendUint64(d.scratch, uint64(len(d.id)))
	d.scratch = append(d.scratch, d.id...)
	d.scratch = binary.LittleEndian.AppendUint64(d.scratch, d.count)
	d.scratch = append(d.scratch, signature[:]...)
	d.writer.bytes(d.scratch)
	if d.closed != nil {
		d.closed(d.id, signature)
	}
	d.open = false
}

func (d *resolvedDigest) finish(volumes []Volume, links []Link) (string, error) {
	d.closeObject()
	d.writer.bytes([]byte{0})
	d.writer.raw(`{"Volumes":`)
	fingerprintArray(d.writer, volumes)
	d.writer.raw(`,"Links":`)
	fingerprintArray(d.writer, links)
	d.writer.raw("}")
	return d.writer.result()
}

func fingerprintResolved(r Resolved) (string, error) {
	d := newResolvedDigest(nil)
	for i := range r.Triangles {
		d.triangle(&r.Triangles[i])
	}
	return d.finish(r.Volumes, r.Links)
}
