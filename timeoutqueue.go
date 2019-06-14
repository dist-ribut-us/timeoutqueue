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
	id         uint32
	action     TimeoutAction
}

// TimeoutQueue manages a queue of TimeoutActions that may be canceled before
// they timeout. The timeout duration is constant within a queue.
type TimeoutQueue struct {
	timeout time.Duration
	running bool
	// nodes in use form a doubly linked list
	head uint32
	tail uint32
	// free nodes form a singly linked list
	free  uint32
	nodes []node
	sync.Mutex
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

func (tq *TimeoutQueue) run() {
	time.Sleep(tq.timeout)
	for {
		tq.Lock()
		if tq.head == empty {
			tq.running = false
			tq.Unlock()
			return
		}
		n := tq.nodes[tq.head]
		if d := n.timeout.Sub(time.Now()); d > 0 {
			tq.Unlock()
			time.Sleep(d)
			continue
		}
		tq.remove(int(tq.head))
		tq.Unlock()
		n.action()
	}
}

// IMPORTANT NOTE
// remove actually requires a mux lock - but all callers already have a mux
// lock, so rather than unlocking and reaquiring, we just call and unlock when
// done.
func (tq *TimeoutQueue) remove(nodeIdx int) {
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
	tq.nodes[nodeIdx].next = tq.free
	tq.nodes[nodeIdx].id++
	tq.nodes[nodeIdx].action = nil
	tq.free = uint32(nodeIdx)
}

// Cancel is returned by Add. It is a function that will cancel the
// TimeoutAction that was passed in. It returns a bool indicating if the cancel
// happened. Cancel will return false if the TimeoutAction has already been
// called or if Cancel has already been called.
type Cancel func() bool

// Add takes a TimeoutAction and adds it to the queue. It returns a Cancel, that
// if called, will cancel the TimeoutAction. If cancel is not called,
// TimeoutAction will be called after the TimeoutQueue's timeout duration.
func (tq *TimeoutQueue) Add(action TimeoutAction) Cancel {
	var nodeIdx int
	var actionID uint32
	timeout := time.Now().Add(tq.timeout)

	tq.Lock()
	if tq.free == empty {
		nodeIdx = len(tq.nodes)
		tq.nodes = append(tq.nodes, node{
			next:    empty,
			prev:    tq.tail,
			timeout: timeout,
			action:  action,
		})
	} else {
		nodeIdx, tq.free = int(tq.free), tq.nodes[tq.free].next
		tq.nodes[nodeIdx].next = empty
		tq.nodes[nodeIdx].prev = tq.tail
		tq.nodes[nodeIdx].timeout = timeout
		tq.nodes[nodeIdx].action = action
		actionID = tq.nodes[nodeIdx].id
	}
	if tq.head == empty {
		tq.head = uint32(nodeIdx)
	} else {
		tq.nodes[tq.tail].next = uint32(nodeIdx)
	}
	tq.tail = uint32(nodeIdx)
	if !tq.running {
		tq.running = true
		go tq.run()
	}
	tq.Unlock()

	return func() bool {
		tq.Lock()
		n := tq.nodes[nodeIdx]
		remove := n.action != nil && n.id == actionID
		if remove {
			tq.remove(nodeIdx)
		}
		tq.Unlock()
		return remove
	}
}
