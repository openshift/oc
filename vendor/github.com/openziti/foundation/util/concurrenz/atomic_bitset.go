package concurrenz

import "sync/atomic"

type AtomicBitSet uint32

func (self *AtomicBitSet) Set(index int, val bool) {
	done := false
	for !done {
		current := self.Load()
		next := setBitAtIndex(current, index, val)
		done = self.CompareAndSetAll(current, next)
	}
}

func (self *AtomicBitSet) IsSet(index int) bool {
	return isBitSetAtIndex(self.Load(), index)
}

func (self *AtomicBitSet) CompareAndSet(index int, current, next bool) bool {
	for {
		currentSet := self.Load()
		if isBitSetAtIndex(currentSet, index) != current {
			return false
		}
		nextSet := setBitAtIndex(currentSet, index, next)
		if self.CompareAndSetAll(currentSet, nextSet) {
			return true
		}
	}
}

func (self *AtomicBitSet) Store(val uint32) {
	atomic.StoreUint32((*uint32)(self), val)
}

func (self *AtomicBitSet) Load() uint32 {
	return atomic.LoadUint32((*uint32)(self))
}

func (self *AtomicBitSet) CompareAndSetAll(current, next uint32) bool {
	return atomic.CompareAndSwapUint32((*uint32)(self), current, next)
}

func setBitAtIndex(bitset uint32, index int, val bool) uint32 {
	if val {
		return bitset | (1 << index)
	}
	return bitset & ^(1 << index)
}

func isBitSetAtIndex(bitset uint32, index int) bool {
	return bitset&(1<<index) != 0
}
