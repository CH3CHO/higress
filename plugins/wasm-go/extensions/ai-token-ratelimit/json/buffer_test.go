package json

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScrollableBuffer(t *testing.T) {
	buffer := NewScrollableBuffer().(*scrollableBuffer)

	// Test initial state
	assert.Equal(t, 0, buffer.GetPos())
	assert.Equal(t, 0, buffer.GetForwardLimit())
	assert.True(t, buffer.IsEOB())
	assert.True(t, buffer.HasMoreDataComing())

	// Test Append and data integrity
	data1 := []byte("hello")
	err := buffer.Append(data1, false)
	assert.NoError(t, err)
	assert.Equal(t, 0, buffer.GetBackwardLimit())
	assert.Equal(t, 5, buffer.GetForwardLimit())
	assert.False(t, buffer.IsEOB())
	assert.True(t, buffer.HasMoreDataComing())
	assert.Equal(t, data1, buffer.data)
	assert.Equal(t, byte('h'), buffer.PeekByte())
	assert.Equal(t, data1[:3], buffer.PeekBytes(3))

	// Test ReadByte, ReadBytes, and position updates
	assert.Equal(t, byte('h'), buffer.ReadByte())
	assert.Equal(t, 1, buffer.GetPos())
	err = buffer.Rewind(1)
	assert.NoError(t, err)
	assert.Equal(t, 0, buffer.GetPos())
	assert.Equal(t, data1[0:3], buffer.ReadBytes(3))
	assert.Equal(t, 3, buffer.GetPos())
	err = buffer.Advance(1)
	assert.NoError(t, err)
	assert.Equal(t, 4, buffer.GetPos())
	assert.False(t, buffer.IsEOB())
	assert.True(t, buffer.HasMoreDataComing())

	// Test Append more data and set to EOF
	data2 := []byte(" world")
	err = buffer.Append(data2, true)
	assert.NoError(t, err)
	assert.Equal(t, 4, buffer.GetBackwardLimit())
	assert.Equal(t, 4, buffer.GetPos())
	assert.Equal(t, 11, buffer.GetForwardLimit())
	assert.Equal(t, append(data1[4:], data2...), buffer.data)
	assert.False(t, buffer.IsEOB())
	assert.False(t, buffer.HasMoreDataComing())

	// Overshooting operations
	assert.Equal(t, 4, buffer.GetPos())
	err = buffer.Advance(10)
	assert.Error(t, err)
	assert.Equal(t, 4, buffer.GetPos())
	err = buffer.Rewind(10)
	assert.Error(t, err)
	assert.Equal(t, 4, buffer.GetPos())
	assert.Equal(t, []byte("o world"), buffer.PeekBytes(10))
	assert.Equal(t, []byte("o world"), buffer.ReadBytes(10))
	assert.Equal(t, 11, buffer.GetPos())

	// Test EOF behavior
	assert.False(t, buffer.HasMoreDataComing())
	err = buffer.Append([]byte("!"), true)
	assert.Error(t, err)
}
