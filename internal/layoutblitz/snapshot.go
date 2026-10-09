package layoutblitz

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// ObservationProperties is the fixed browser-consumer schema, not a workload
// selector list. Other CSSOM values use scalar readback from the same owner.
var ObservationProperties = []string{"display", "visibility", "cursor", "content-visibility", "opacity", "pointer-events", "position", "z-index", "overflow-x", "overflow-y", "direction", "font-size", "transform", "transform-origin", "isolation", "perspective", "content", "background-image"}

// PackedSnapshot has a versioned header followed by sorted canonical records
// and an interned UTF-8 string pool. Worlds binary-search IDs in place, without
// reconstructing a document object graph or copying a map per element.
func (d *Document) PackedSnapshot() ([]byte, error) {
	ids := make([]int64, 0, len(d.nodes))
	for id, node := range d.nodes {
		if node.Type == "element" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return d.packedStyles(ids, false)
}

// PackedNodeStyle materializes the required consumer columns for an
// undisplayed node in one native style-resolution pass. Geometry remains zero
// where no layout box exists. It never installs hidden styles as layout state.
func (d *Document) PackedNodeStyle(id int64) ([]byte, error) {
	if node, ok := d.nodes[id]; !ok || node.Type != "element" {
		return nil, fmt.Errorf("blitz: absent style node %d", id)
	}
	return d.packedStyles([]int64{id}, true)
}

func (d *Document) packedStyles(ids []int64, resolveUndisplayed bool) ([]byte, error) {
	stride := 80 + len(ObservationProperties)*8
	if len(ids) > (256*1024*1024-16)/stride {
		return nil, fmt.Errorf("blitz: snapshot exceeds memory bound")
	}
	output := make([]byte, 16+len(ids)*stride)
	binary.LittleEndian.PutUint32(output, 2)
	binary.LittleEndian.PutUint32(output[4:], uint32(len(ids)))
	binary.LittleEndian.PutUint32(output[8:], uint32(stride))
	binary.LittleEndian.PutUint32(output[12:], uint32(len(ObservationProperties)))
	pool := make(map[string]uint32)
	for i, id := range ids {
		at := 16 + i*stride
		binary.LittleEndian.PutUint64(output[at:], uint64(id))
		rect, err := d.Owner.Rect(uint64(id))
		if err != nil {
			return nil, err
		}
		for j, value := range []float64{rect.X, rect.Y, rect.Width, rect.Height, rect.ClientWidth, rect.ClientHeight, rect.ContentWidth, rect.ContentHeight} {
			binary.LittleEndian.PutUint64(output[at+8+j*8:], math.Float64bits(value))
		}
		binary.LittleEndian.PutUint32(output[at+72:], rect.Flags)
		ready, err := d.Owner.HasComputedStyle(uint64(id))
		if err != nil {
			return nil, err
		}
		if !ready && !resolveUndisplayed {
			for k := range ObservationProperties {
				binary.LittleEndian.PutUint32(output[at+80+k*8:], math.MaxUint32)
			}
			continue
		}
		err = d.Owner.styleBatch(uint64(id), ObservationProperties, func(j int, value []byte) error {
			// Lookup does not retain the borrowed buffer. Only a new pool entry
			// needs its own string; repeated values reuse their existing offsets.
			offset, exists := pool[string(value)]
			if !exists {
				if len(output)+len(value)+1 > 256*1024*1024 {
					return fmt.Errorf("blitz: string pool exceeds memory bound")
				}
				offset = uint32(len(output))
				pool[string(value)] = offset
				output = append(output, value...)
				output = append(output, 0)
			}
			binary.LittleEndian.PutUint32(output[at+80+j*8:], offset)
			binary.LittleEndian.PutUint32(output[at+84+j*8:], uint32(len(value)))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return output, nil
}
