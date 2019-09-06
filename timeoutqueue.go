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
		go n.action()
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
	tq.nodes[nodeIdx].actionID++
	tq.nodes[nodeIdx].action = nil
	tq.free = uint32(nodeIdx)
}

// Add takes a TimeoutAction and adds it to the queue. The TimeoutAction will be
// called after the TimeoutQueue's timeout duration unless modified by a Token
// method.
func (tq *TimeoutQueue) Add(action TimeoutAction) Token {
	timeout := time.Now().Add(tq.timeout)
	t := token{
		tq: tq,
	}

	tq.Lock()
	if tq.free == empty {
		t.nodeIdx = len(tq.nodes)
		tq.nodes = append(tq.nodes, node{
			next:    empty,
			prev:    tq.tail,
			timeout: timeout,
			action:  action,
		})
	} else {
		t.nodeIdx, tq.free = int(tq.free), tq.nodes[tq.free].next
		tq.nodes[t.nodeIdx].next = empty
		tq.nodes[t.nodeIdx].prev = tq.tail
		tq.nodes[t.nodeIdx].timeout = timeout
		tq.nodes[t.nodeIdx].action = action
		t.actionID = tq.nodes[t.nodeIdx].actionID
	}
	if tq.head == empty {
		tq.head = uint32(t.nodeIdx)
	} else {
		tq.nodes[tq.tail].next = uint32(t.nodeIdx)
	}
	tq.tail = uint32(t.nodeIdx)
	if !tq.running {
		tq.running = true
		go tq.run()
	}
	tq.Unlock()

	return t
}

type token struct {
	tq       *TimeoutQueue
	nodeIdx  int //TODO: make this uint32
	actionID uint32
}

func (t token) Cancel() bool {
	t.tq.Lock()
	n := t.tq.nodes[t.nodeIdx]
	remove := n.action != nil && n.actionID == t.actionID
	if remove {
		t.tq.remove(t.nodeIdx)
	}
	t.tq.Unlock()
	return remove
}

func (t token) Reset() bool {
	timeout := time.Now().Add(t.tq.timeout)

	t.tq.Lock()

	n := t.tq.nodes[t.nodeIdx]
	ok := n.action != nil && n.actionID == t.actionID
	if !ok {
		return false
	}
	n.timeout = timeout

	// remove node from middle of list
	if n.prev == empty {
		t.tq.head = n.next
	} else {
		t.tq.nodes[n.prev].next = n.next
	}
	if n.next == empty {
		t.tq.tail = n.prev
	} else {
		t.tq.nodes[n.next].prev = n.prev
	}

	// add to end of list
	n.next = empty
	n.prev = t.tq.tail
	if t.tq.head == empty {
		t.tq.head = uint32(t.nodeIdx)
	} else {
		t.tq.nodes[t.tq.tail].next = uint32(t.nodeIdx)
	}
	t.tq.tail = uint32(t.nodeIdx)
	t.tq.nodes[t.nodeIdx] = n

	t.tq.Unlock()
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
