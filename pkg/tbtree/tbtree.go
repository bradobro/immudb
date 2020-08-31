/*
Copyright 2019-2020 vChain, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package tbtree

import (
	"bytes"
	"errors"
	"sync"
)

var ErrIllegalArgument = errors.New("illegal arguments")
var ErrKeyNotFound = errors.New("key not found")
var ErrIllegalState = errors.New("illegal state")
var ErrAlreadyClosed = errors.New("already closed")
var ErrSnapshotsNotClosed = errors.New("snapshots not closed")

const MinNodeSize = 64
const DefaultMaxNodeSize = 4096
const DefaultInsertionCountThreshold = 100000

// TBTree implements a timed-btree
type TBtree struct {
	root                    node
	maxNodeSize             int
	insertionCount          uint64
	insertionCountThreshold uint64
	// bloom filter
	// file
	// node manager
	lastFlushedTs uint64
	snapshots     map[uint64]*Snapshot
	maxSnapshotId uint64
	closed        bool
	rwmutex       sync.RWMutex
}

type Options struct {
	maxNodeSize             int
	insertionCountThreshold uint64
}

func DefaultOptions() *Options {
	return &Options{
		maxNodeSize:             DefaultMaxNodeSize,
		insertionCountThreshold: DefaultInsertionCountThreshold,
	}
}

func (opt *Options) setMaxNodeSize(maxNodeSize int) *Options {
	opt.maxNodeSize = maxNodeSize
	return opt
}

func (opt *Options) setInsertionCountThreshold(insertionCountThreshold uint64) *Options {
	opt.insertionCountThreshold = insertionCountThreshold
	return opt
}

type path []*innerNode

type node interface {
	insertAt(key []byte, value []byte, ts uint64) (node, node, error)
	get(key []byte) (value []byte, ts uint64, err error)
	findLeafNode(keyPrefix []byte, path path, neqKey []byte, ascOrder bool) (path, *leafNode, int, error)
	maxKey() []byte
	ts() uint64
}

type innerNode struct {
	prevNode node
	nodes    []*childRef
	cts      uint64
	csize    int
	maxSize  int
	offset   uint64
}

type leafNode struct {
	prevNode node
	values   []*leafValue
	cts      uint64
	csize    int
	maxSize  int
	offset   uint64
}

type nodeRef struct {
	offset uint64
}

type leafValue struct {
	key    []byte
	ts     uint64
	prevTs uint64
	value  []byte
}

type childRef struct {
	key  []byte
	cts  uint64
	node node
}

func New() (*TBtree, error) {
	return NewWith(DefaultOptions())
}

func NewWith(opt *Options) (*TBtree, error) {
	if opt == nil || opt.maxNodeSize < MinNodeSize || opt.insertionCountThreshold < 1 {
		return nil, ErrIllegalArgument
	}

	tbtree := &TBtree{
		maxNodeSize:             opt.maxNodeSize,
		insertionCountThreshold: opt.insertionCountThreshold,
		root:                    &leafNode{maxSize: opt.maxNodeSize},
		snapshots:               make(map[uint64]*Snapshot),
	}

	return tbtree, nil
}

func (t *TBtree) Close() error {
	t.rwmutex.Lock()
	defer t.rwmutex.Unlock()

	if t.closed {
		return ErrAlreadyClosed
	}

	if len(t.snapshots) > 0 {
		return ErrSnapshotsNotClosed
	}

	if t.insertionCount > 0 {
		_, err := t.Snapshot()
		if err != nil {
			return err
		}
		// TODO: lastest snapshot must be flushed
	}

	t.closed = true

	return nil
}

func (t *TBtree) Insert(key []byte, value []byte, ts uint64) error {
	t.rwmutex.Lock()
	defer t.rwmutex.Unlock()

	if t.closed {
		return ErrAlreadyClosed
	}

	if key == nil || t.root.ts() >= ts {
		return ErrIllegalArgument
	}

	n1, n2, err := t.root.insertAt(key, value, ts)
	if err != nil {
		return err
	}

	t.insertionCount++

	if n2 == nil {
		t.root = n1
		return nil
	}

	ns := make([]*childRef, 2)
	newRoot := &innerNode{prevNode: t.root, maxSize: t.maxNodeSize, nodes: ns, cts: ts}

	ns[0] = &childRef{key: n1.maxKey(), cts: n1.ts(), node: n1}
	ns[1] = &childRef{key: n2.maxKey(), cts: n2.ts(), node: n2}

	newRoot.updateSize()

	t.root = newRoot

	return nil
}

func (t *TBtree) Snapshot() (*Snapshot, error) {
	t.rwmutex.Lock()
	defer t.rwmutex.Unlock()

	if t.closed {
		return nil, ErrAlreadyClosed
	}

	if len(t.snapshots) > 0 && t.insertionCount <= t.insertionCountThreshold {
		return t.snapshots[t.maxSnapshotId], nil
	}

	return t.newSnapshot(), nil
}

func (t *TBtree) newSnapshot() *Snapshot {
	snapshot := &Snapshot{
		t:       t,
		id:      t.maxSnapshotId,
		root:    t.root,
		readers: make(map[int]*Reader),
	}

	t.snapshots[snapshot.id] = snapshot

	t.maxSnapshotId++
	t.insertionCount = 0

	return snapshot
}

func (t *TBtree) snapshotClosed(snapshot *Snapshot) error {
	t.rwmutex.Lock()
	defer t.rwmutex.Unlock()

	if t.closed {
		return ErrAlreadyClosed
	}

	delete(t.snapshots, snapshot.id)

	// recycle allocations : node manager

	return nil
}

func (n *innerNode) insertAt(key []byte, value []byte, ts uint64) (n1 node, n2 node, err error) {
	insertAt := n.indexOf(key)

	cRef := n.nodes[insertAt]

	// TODO: jeroiraz it's possible that childRef is not loaded into main mem yet

	c1, c2, err := cRef.node.insertAt(key, value, ts)
	if err != nil {
		return nil, nil, err
	}

	if c2 == nil {
		newNode := &innerNode{
			prevNode: n,
			maxSize:  n.maxSize,
			nodes:    make([]*childRef, len(n.nodes)),
			cts:      ts,
		}

		copy(newNode.nodes[:insertAt], n.nodes[:insertAt])

		newNode.nodes[insertAt] = &childRef{key: c1.maxKey(), cts: c1.ts(), node: c1}

		if insertAt+1 < len(newNode.nodes) {
			copy(newNode.nodes[insertAt+1:], n.nodes[insertAt+1:])
		}

		newNode.updateSize()

		return newNode, nil, nil
	}

	newNode := &innerNode{
		prevNode: n,
		maxSize:  n.maxSize,
		nodes:    make([]*childRef, len(n.nodes)+1),
		cts:      ts,
	}

	copy(newNode.nodes[:insertAt], n.nodes[:insertAt])

	newNode.nodes[insertAt] = &childRef{key: c1.maxKey(), cts: c1.ts(), node: c1}
	newNode.nodes[insertAt+1] = &childRef{key: c2.maxKey(), cts: c2.ts(), node: c2}

	if insertAt+2 < len(newNode.nodes) {
		copy(newNode.nodes[insertAt+2:], n.nodes[insertAt+1:])
	}

	newNode.updateSize()

	n2, err = newNode.split()

	return newNode, n2, err
}

func (n *innerNode) get(key []byte) (value []byte, ts uint64, err error) {
	i := n.indexOf(key)

	if bytes.Compare(key, n.nodes[i].key) == 1 {
		return nil, 0, ErrKeyNotFound
	}

	return n.nodes[i].node.get(key)
}

func (n *innerNode) findLeafNode(keyPrefix []byte, path path, neqKey []byte, ascOrder bool) (path, *leafNode, int, error) {
	if ascOrder || neqKey == nil {
		for i := 0; i < len(n.nodes); i++ {
			if bytes.Compare(keyPrefix, n.nodes[i].key) < 1 && bytes.Compare(n.nodes[i].key, neqKey) == 1 {
				return n.nodes[i].node.findLeafNode(keyPrefix, append(path, n), neqKey, ascOrder)
			}
		}

		if ascOrder {
			return nil, nil, 0, ErrKeyNotFound
		}

		return n.nodes[len(n.nodes)-1].node.findLeafNode(keyPrefix, append(path, n), neqKey, ascOrder)
	}

	for i := len(n.nodes); i > 0; i-- {
		if bytes.Compare(n.nodes[i-1].key, keyPrefix) < 1 && bytes.Compare(n.nodes[i-1].key, neqKey) < 0 {
			return n.nodes[i-1].node.findLeafNode(keyPrefix, append(path, n), neqKey, ascOrder)
		}
	}

	return nil, nil, 0, ErrKeyNotFound
}

func (n *innerNode) ts() uint64 {
	return n.cts
}

func (n *innerNode) updateSize() {
	n.csize = 0

	for i := 0; i < len(n.nodes); i++ {
		n.csize += len(n.nodes[i].key)
	}
}

func (n *innerNode) maxKey() []byte {
	return n.nodes[len(n.nodes)-1].key
}

func (n *innerNode) indexOf(key []byte) int {
	for i := 0; i < len(n.nodes); i++ {
		if bytes.Compare(key, n.nodes[i].key) < 1 {
			return i
		}
	}
	return len(n.nodes) - 1
}

func (n *innerNode) split() (node, error) {
	if n.csize <= n.maxSize {
		return nil, nil
	}

	splitIndex, splitSize := n.splitInfo()

	newNode := &innerNode{
		maxSize: n.maxSize,
		nodes:   n.nodes[splitIndex:],
		csize:   n.csize - splitSize,
	}
	newNode.updateTs()

	n.nodes = n.nodes[:splitIndex]
	n.csize = splitSize
	n.updateTs()

	return newNode, nil
}

func (n *innerNode) splitInfo() (splitIndex int, splitSize int) {
	for i := 0; i < len(n.nodes); i++ {
		splitIndex = i
		if splitSize+len(n.nodes[i].key) > n.maxSize {
			break
		}
		splitSize += len(n.nodes[i].key)
	}
	return
}

func (n *innerNode) updateTs() {
	n.cts = 0
	for i := 0; i < len(n.nodes); i++ {
		if n.cts < n.nodes[i].cts {
			n.cts = n.nodes[i].cts
		}
	}
	return
}

////////////////////////////////////////////////////////////

func (l *leafNode) insertAt(key []byte, value []byte, ts uint64) (n1 node, n2 node, err error) {
	i, found := l.indexOf(key)

	if found {
		newLeaf := &leafNode{
			prevNode: l,
			maxSize:  l.maxSize,
			cts:      ts,
			values:   make([]*leafValue, len(l.values)),
			csize:    l.csize,
		}

		copy(newLeaf.values[:i], l.values[:i])

		newLeaf.values[i] = &leafValue{
			key:    key,
			ts:     ts,
			prevTs: l.values[i].ts,
			value:  value,
		}

		if i+1 < len(newLeaf.values) {
			copy(newLeaf.values[i+1:], l.values[i+1:])
		}

		return newLeaf, nil, nil
	}

	lv := &leafValue{
		key:    key,
		ts:     ts,
		prevTs: 0,
		value:  value,
	}

	newLeaf := &leafNode{
		prevNode: l,
		maxSize:  l.maxSize,
		cts:      ts,
		values:   make([]*leafValue, len(l.values)+1),
		csize:    l.csize + lv.size(),
	}

	copy(newLeaf.values[:i], l.values[:i])

	newLeaf.values[i] = lv

	if i+1 < len(newLeaf.values) {
		copy(newLeaf.values[i+1:], l.values[i:])
	}

	n2, err = newLeaf.split()

	return newLeaf, n2, err
}

func (l *leafNode) get(key []byte) (value []byte, ts uint64, err error) {
	i, found := l.indexOf(key)

	if !found {
		return nil, 0, ErrKeyNotFound
	}

	leafValue := l.values[i]
	return leafValue.value, leafValue.ts, nil
}

func (l *leafNode) findLeafNode(keyPrefix []byte, path path, neqKey []byte, ascOrder bool) (path, *leafNode, int, error) {
	if ascOrder || neqKey == nil {
		for i := 0; i < len(l.values); i++ {
			if bytes.Compare(keyPrefix, l.values[i].key) < 1 && bytes.Compare(l.values[i].key, neqKey) == 1 {
				return path, l, i, nil
			}
		}

		if ascOrder || len(l.values) == 0 {
			return nil, nil, 0, ErrKeyNotFound
		}

		return path, l, len(l.values) - 1, nil
	}

	for i := len(l.values); i > 0; i-- {
		if bytes.Compare(l.values[i-1].key, keyPrefix) < 1 && bytes.Compare(l.values[i-1].key, neqKey) < 0 {
			return path, l, i - 1, nil
		}
	}

	return nil, nil, 0, ErrKeyNotFound
}

func (l *leafNode) indexOf(key []byte) (index int, found bool) {
	for i := 0; i < len(l.values); i++ {
		if bytes.Equal(l.values[i].key, key) {
			return i, true
		}

		if bytes.Compare(l.values[i].key, key) == 1 {
			return i, false
		}
	}

	return len(l.values), false
}

func (l *leafNode) maxKey() []byte {
	return l.values[len(l.values)-1].key
}

func (l *leafNode) ts() uint64 {
	return l.cts
}

func (l *leafNode) split() (node, error) {
	if l.csize <= l.maxSize {
		return nil, nil
	}

	splitIndex, splitSize := l.splitInfo()

	newLeaf := &leafNode{
		maxSize: l.maxSize,
		values:  l.values[splitIndex:],
		csize:   l.csize - splitSize,
	}
	newLeaf.updateTs()

	l.values = l.values[:splitIndex]
	l.csize = splitSize
	l.updateTs()

	return newLeaf, nil
}

func (l *leafNode) splitInfo() (splitIndex int, splitSize int) {
	for i := 0; i < len(l.values); i++ {
		splitIndex = i
		if splitSize+l.values[i].size() > l.maxSize {
			break
		}
		splitSize += l.values[i].size()
	}

	return
}

func (l *leafNode) updateTs() {
	l.cts = 0

	for i := 0; i < len(l.values); i++ {
		if l.cts < l.values[i].ts {
			l.cts = l.values[i].ts
		}
	}

	return
}

func (lv *leafValue) size() int {
	return 16 + len(lv.key) + len(lv.value)
}
