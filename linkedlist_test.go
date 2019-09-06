package timeoutqueue

import (
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestLinkedLists(t *testing.T) {
	tq := New(time.Second*5, 10)

	assert.EqualValues(t, empty, tq.head)
	assert.EqualValues(t, empty, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	action := func() {}

	cs := make([]Token, 3)

	cs[0] = tq.Add(action)
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 0, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	assert.True(t, cs[0].Cancel())
	assert.EqualValues(t, empty, tq.head)
	assert.EqualValues(t, empty, tq.tail)
	assert.EqualValues(t, 0, tq.free)

	cs[1] = tq.Add(action)
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 0, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	// calling previous cancel again does nothing
	assert.False(t, cs[0].Cancel())
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 0, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	cs[0] = tq.Add(action)
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 1, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	cs[2] = tq.Add(action)
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 2, tq.tail)
	assert.EqualValues(t, empty, tq.free)

	assert.True(t, cs[2].Cancel())
	assert.EqualValues(t, 0, tq.head)
	assert.EqualValues(t, 1, tq.tail)
	assert.EqualValues(t, 2, tq.free)

	assert.True(t, cs[1].Cancel())
	assert.EqualValues(t, 1, tq.head)
	assert.EqualValues(t, 1, tq.tail)
	assert.EqualValues(t, 0, tq.free)

	assert.True(t, cs[0].Cancel())
	assert.EqualValues(t, empty, tq.head)
	assert.EqualValues(t, empty, tq.tail)
	assert.EqualValues(t, 1, tq.free)

	// Just to get to 100% test coverage
	Token(token{}).private()
}
