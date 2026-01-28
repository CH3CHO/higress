package json

import (
	"errors"
	"fmt"
)

type ScrollableBuffer interface {
	GetPos() int
	SetPos(pos int) error
	Advance(n int) error
	Rewind(n int) error
	GetBackwardLimit() int
	GetForwardLimit() int

	ByteAt(pos int) byte
	PeekByte() byte
	PeekBytes(n int) []byte
	ReadByte() byte
	ReadBytes(n int) []byte
	Append(data []byte, eof bool) error

	// HasBytesAvailable returns if n bytes are available from current position in the buffer
	HasBytesAvailable(n int) bool
	// HasMoreDataComing returns whether more data may be appended in the future
	HasMoreDataComing() bool
}

func NewScrollableBuffer() ScrollableBuffer {
	return &scrollableBuffer{
		data:        make([]byte, 0),
		headPos:     0,
		relativePos: 0,
		limit:       0,
		eof:         false,
	}
}

type scrollableBuffer struct {
	data        []byte
	headPos     int // absolute head position
	relativePos int // position relative to head
	limit       int // absolute limit
	eof         bool
}

func (b *scrollableBuffer) GetPos() int {
	return b.headPos + b.relativePos
}

func (b *scrollableBuffer) SetPos(pos int) error {
	if pos < b.headPos || pos > b.limit {
		return fmt.Errorf("position out of bounds: [%d, %d]", b.headPos, b.limit)
	}
	b.relativePos = pos - b.headPos
	return nil
}

func (b *scrollableBuffer) Advance(n int) error {
	if n < 0 {
		return errors.New("n must be non-negative")
	}
	if n == 0 {
		return nil
	}
	pos := b.GetPos()
	newPos := pos + n
	if newPos > b.limit {
		return fmt.Errorf("step out of bounds: [0, %d]", b.limit-pos)
	}
	return b.SetPos(newPos)
}

func (b *scrollableBuffer) Rewind(n int) error {
	if n < 0 {
		return errors.New("n must be non-negative")
	}
	if n == 0 {
		return nil
	}
	pos := b.GetPos()
	newPos := pos - n
	if newPos < b.headPos {
		return fmt.Errorf("step out of bounds: [0, %d]", pos-b.headPos)
	}
	return b.SetPos(newPos)
}

func (b *scrollableBuffer) GetBackwardLimit() int {
	return b.headPos
}

func (b *scrollableBuffer) GetForwardLimit() int {
	return b.limit
}

func (b *scrollableBuffer) ByteAt(pos int) byte {
	if pos < b.headPos || pos >= b.limit {
		return 0
	}
	relativePos := pos - b.headPos
	return b.data[relativePos]
}

func (b *scrollableBuffer) PeekByte() byte {
	if b.GetPos() >= b.limit {
		return 0
	}
	return b.data[b.relativePos]
}

func (b *scrollableBuffer) PeekBytes(n int) []byte {
	if n <= 0 {
		return make([]byte, 0)
	}
	end := b.GetPos() + n
	if end >= b.limit {
		end = b.limit
	}
	return b.data[b.relativePos : end-b.headPos]
}

func (b *scrollableBuffer) ReadByte() byte {
	if b.GetPos() >= b.limit {
		return 0
	}
	byteValue := b.data[b.relativePos]
	b.relativePos++
	return byteValue
}

func (b *scrollableBuffer) ReadBytes(n int) []byte {
	if n <= 0 {
		return make([]byte, 0)
	}
	bytesValue := b.PeekBytes(n)
	_ = b.Advance(len(bytesValue))
	return bytesValue
}

func (b *scrollableBuffer) Append(data []byte, eof bool) error {
	if b.eof {
		return errors.New("cannot append data after EOF")
	}
	pos := b.GetPos()
	b.data = append(b.data[b.relativePos:], data...)
	b.headPos = pos
	b.relativePos = 0
	b.limit = b.headPos + len(b.data)
	b.eof = eof
	return nil
}

func (b *scrollableBuffer) HasBytesAvailable(n int) bool {
	return b.GetPos()+n <= b.limit
}

func (b *scrollableBuffer) HasMoreDataComing() bool {
	return !b.eof
}

func (b *scrollableBuffer) IsEOB() bool {
	return b.GetPos() >= b.limit
}
