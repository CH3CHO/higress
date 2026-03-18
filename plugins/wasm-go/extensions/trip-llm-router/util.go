package main

import (
	"encoding/binary"
)

func uint32ToBytes(i uint32) []byte {
	bs := make([]byte, 4)
	binary.LittleEndian.PutUint32(bs, i)
	return bs
}

func bytesToUint32(bs []byte) uint32 {
	if len(bs) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(bs)
}
