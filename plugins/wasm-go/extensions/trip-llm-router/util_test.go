package main

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUint32ToBytesAndBack(t *testing.T) {
	var i uint32
	for i = 0; i < math.MaxUint32; {
		t.Run("value", func(t *testing.T) {
			bs := uint32ToBytes(i)
			assert.Len(t, bs, 4, "expected 4 bytes")
			got := bytesToUint32(bs)
			assert.Equal(t, i, got, "round-trip mismatch")
		})

		if i < 100000 {
			i++
		} else {
			i = i*2 + 1
		}
	}
}

func TestUint32ToBytesEndian(t *testing.T) {
	bs := uint32ToBytes(0x01020304)
	want := []byte{0x04, 0x03, 0x02, 0x01}
	assert.True(t, bytes.Equal(bs, want), "little-endian mismatch: want %v, got %v", want, bs)
}

func TestBytesToUint32InvalidLength(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00},
		{0x00, 0x01},
		{0x00, 0x01, 0x02},
		{0x00, 0x01, 0x02, 0x03, 0x04},
	}

	for _, c := range cases {
		t.Run("len", func(t *testing.T) {
			got := bytesToUint32(c)
			assert.Equal(t, uint32(0), got, "expected 0 for invalid length")
		})
	}
}
