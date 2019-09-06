package timeoutqueue_test

import (
	"github.com/dist-ribut-us/testutil/timeout"
	"github.com/dist-ribut-us/timeoutqueue"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

// Note: While it would be better to avoid time.Sleep in tests, when the
// TimeoutQueue was updated to call the TimeoutActions in Go routines, it was
// difficult to guarentee the execution order. Adding small delays doesn't
// slow down the test much, but keeps the execution order predictible.

func TestTimeoutQueue(t *testing.T) {
	tq := timeoutqueue.New(time.Millisecond*5, 10)

	ch := make(chan int)
	tq.Add(getAction(ch, 1))
	assert.NoError(t, timeout.After(20, ch))

	token1 := tq.Add(func() {
		t.Error("This should be canceled")
	})
	token2 := tq.Add(getAction(ch, 2))
	assert.True(t, token1.Cancel())
	assert.NoError(t, timeout.After(20, ch))
	assert.False(t, token2.Cancel())
}

func TestReset(t *testing.T) {
	tq := timeoutqueue.New(time.Millisecond*10, 2)
	ch := make(chan int)

	tokens := []timeoutqueue.Token{
		tq.Add(getAction(ch, 1)),
		tq.Add(getAction(ch, 2)),
		tq.Add(getAction(ch, 3)),
	}

	time.Sleep(time.Millisecond)
	assert.True(t, tokens[1].Reset())
	assert.NoError(t, timeout.After(20, func() {
		assert.Equal(t, 1, <-ch)
		assert.Equal(t, 3, <-ch)
		assert.Equal(t, 2, <-ch)
	}))

	tokens = []timeoutqueue.Token{
		tq.Add(getAction(ch, 1)),
		tq.Add(getAction(ch, 2)),
		tq.Add(getAction(ch, 3)),
	}

	time.Sleep(time.Millisecond)
	assert.True(t, tokens[1].Reset())
	assert.True(t, tokens[0].Reset())
	assert.True(t, tokens[1].Cancel())
	tq.Add(getAction(ch, 4))
	assert.NoError(t, timeout.After(20, func() {
		assert.Equal(t, 3, <-ch)
		assert.Equal(t, 1, <-ch)
		assert.Equal(t, 4, <-ch)
	}))
}

func getAction(ch chan<- int, i int) func() {
	time.Sleep(time.Millisecond * 2)
	return func() {
		ch <- i
	}
}
