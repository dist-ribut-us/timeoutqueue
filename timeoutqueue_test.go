package timeoutqueue_test

import (
	"github.com/dist-ribut-us/testutil"
	"github.com/dist-ribut-us/timeoutqueue"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestTimeoutQueue(t *testing.T) {
	tq := timeoutqueue.New(time.Millisecond, 10)

	ch := testutil.SignalChan()
	action := func() {
		ch <- testutil.Signal
	}
	tq.Add(action)
	assert.NoError(t, testutil.TimeoutChan(10, ch))

	cancel1 := tq.Add(func() {
		t.Error("This should be canceled")
	})
	cancel2 := tq.Add(action)
	assert.True(t, cancel1())
	assert.NoError(t, testutil.TimeoutChan(10, ch))
	assert.False(t, cancel2())
}
