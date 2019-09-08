// Package timeoutqueue provides a queue for performing a timeout action after a
// constant period of time. It generates almost no garbage (only when it has to
// grow it's internal slice). It is threadsafe. It runs a Go routine only when
// there are timeout actions in the queue.
package timeoutqueue

import (
	"sync"
	"time"
)

// TimeoutAction is what is called when a timeout occures. It is not called in
// it's own Go routine so depending on the complexity of the TimeoutAction, it
// may need to call a Go routine to quickly return control to the queue.
type TimeoutAction func()

const empty = ^uint32(0)

type node struct {
	next, prev uint32
	timeout    time.Time
	// actionID is incremented each time the node is reused to prevent a previous
	// cancel from working on a later action
	actionID uint32
	action   TimeoutAction
}

// TimeoutQueue manages a queue of TimeoutActions that may be canceled before
// they timeout. The timeout duration is constant within a queue.
type TimeoutQueue struct {
	timeout time.Duration
	running uint16
	// nodes in use form a doubly linked list
	head uint32
	tail uint32
	// free nodes form a singly linked list
	free  uint32
	nodes []node
	mux   sync.Mutex
}

// New returns a TimeoutQueue. This is the point at which timeout is set and
// cannot be changed. The capacity determines the capacity of the internal
// slice. The queue will grow in size as need, but will not shrink. Providing
// enough initial capacity will reduce the copy cost of growing the internal
// slice.
func New(timeout time.Duration, capacity int) *TimeoutQueue {
	return &TimeoutQueue{
		timeout: timeout,
		head:    empty,
		tail:    empty,
		free:    empty,
		nodes:   make([]node, 0, capacity),
	}
}

func (tq *TimeoutQueue) run(id uint16) {
	if id == 1 {
		time.Sleep(tq.timeout)
	}
	for {
		tq.mux.Lock()
		if id != tq.running {
			// another thread has taken over
			return
		}
		if tq.head == empty {
			tq.running = 0
			tq.mux.Unlock()
			return
		}
		n := tq.nodes[tq.head]
		if d := n.timeout.Sub(time.Now()); d > 0 {
			tq.mux.Unlock()
			time.Sleep(d)
			continue
		}
		tq.freeNode(tq.head)
		tq.mux.Unlock()
		go n.action()
	}
}

/* IMPORTANT NOTE */
// add, remove and freeNode actually requires a mux lock - but all callers already
// have a mux lock, so rather than unlocking and reaquiring, we just call and
// unlock when done.
func (tq *TimeoutQueue) add(nodeIdx uint32) {
	if tq.head == empty {
		tq.head = nodeIdx
	} else {
		tq.nodes[tq.tail].next = nodeIdx
	}
	tq.tail = nodeIdx
}

func (tq *TimeoutQueue) remove(nodeIdx uint32) {
	n := tq.nodes[nodeIdx]
	if n.prev == empty {
		tq.head = n.next
	} else {
		tq.nodes[n.prev].next = n.next
	}
	if n.next == empty {
		tq.tail = n.prev
	} else {
		tq.nodes[n.next].prev = n.prev
	}
}

func (tq *TimeoutQueue) freeNode(nodeIdx uint32) {
	tq.remove(nodeIdx)
	tq.nodes[nodeIdx].next = tq.free
	tq.nodes[nodeIdx].actionID++
	tq.nodes[nodeIdx].action = nil
	tq.free = nodeIdx
}

// Add takes a TimeoutAction and adds it to the queue. The TimeoutAction will be
// called after the TimeoutQueue's timeout duration unless modified by a Token
// method.
func (tq *TimeoutQueue) Add(action TimeoutAction) Token {
	timeout := time.Now().Add(tq.timeout)
	t := token{
		tq: tq,
	}

	tq.mux.Lock()
	if tq.free == empty {
		t.nodeIdx = uint32(len(tq.nodes))
		tq.nodes = append(tq.nodes, node{
			next:    empty,
			prev:    tq.tail,
			timeout: timeout,
			action:  action,
		})
	} else {
		t.nodeIdx, tq.free = tq.free, tq.nodes[tq.free].next
		tq.nodes[t.nodeIdx].next = empty
		tq.nodes[t.nodeIdx].prev = tq.tail
		tq.nodes[t.nodeIdx].timeout = timeout
		tq.nodes[t.nodeIdx].action = action
		t.actionID = tq.nodes[t.nodeIdx].actionID
	}
	tq.add(t.nodeIdx)
	if tq.running == 0 {
		tq.running = 1
		go tq.run(1)
	}
	tq.mux.Unlock()

	return t
}

// Timeout duration before the TimeoutAction is called.
func (tq *TimeoutQueue) Timeout() time.Duration {
	return tq.timeout
}

// SetTimeout changes the timeout duration of the queue. Everything in the queue
// will have it's timeout updated relative to when it was was added or reset. So
// if the timeout is reset from 5ms to 10ms and there is a TimeoutAction in the
// queueadded 3ms ago, it will go from expiring 2ms in the future to 7ms in the
// future.
func (tq *TimeoutQueue) SetTimeout(timeout time.Duration) {
	tq.mux.Lock()
	d := timeout - tq.timeout
	tq.timeout = timeout

	if tq.head != empty {
		for cur := tq.head; cur != empty; cur = tq.nodes[cur].next {
			tq.nodes[cur].timeout = tq.nodes[cur].timeout.Add(d)
		}
		if d < 0 {
			tq.running++
			go tq.run(tq.running)
		}
	}

	tq.mux.Unlock()
}

type token struct {
	tq       *TimeoutQueue
	nodeIdx  uint32
	actionID uint32
}

func (t token) Cancel() bool {
	t.tq.mux.Lock()
	n := t.tq.nodes[t.nodeIdx]
	remove := n.action != nil && n.actionID == t.actionID
	if remove {
		t.tq.freeNode(t.nodeIdx)
	}
	t.tq.mux.Unlock()
	return remove
}

func (t token) Reset() bool {
	timeout := time.Now().Add(t.tq.timeout)

	t.tq.mux.Lock()

	n := t.tq.nodes[t.nodeIdx]
	if n.action == nil || n.actionID != t.actionID {
		t.tq.mux.Unlock()
		return false
	}
	n.timeout = timeout

	t.tq.remove(t.nodeIdx)

	// add to end of list
	n.next = empty
	n.prev = t.tq.tail
	t.tq.nodes[t.nodeIdx] = n
	t.tq.add(t.nodeIdx)

	t.tq.mux.Unlock()
	return true
}

func (token) private() {}

// Token represents a TimeoutAction that was registered.
type Token interface {
	private()
	// Cancel will remove the TimeoutAction from the queue. The returned bool
	// indicates if the Cancel happened. Returning false means that the
	// TimeoutAction was either previously canceled or the TimeoutAction has
	// already run.
	Cancel() bool
	// Reset the timeout to the TimeoutQueue's duration. The returned bool
	// indicates if the Cancel happened. Returning false means that the
	// TimeoutAction was either previously canceled or the TimeoutAction has
	// already run.
	Reset() bool
}
