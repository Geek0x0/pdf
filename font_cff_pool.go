// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"sync"
)

// CFFObjectPool provides object pooling for CFF decoding operations
type CFFObjectPool struct {
	stackPool   sync.Pool
	commandPool sync.Pool
}

// NewCFFObjectPool creates a new CFF object pool
func NewCFFObjectPool() *CFFObjectPool {
	return &CFFObjectPool{
		stackPool: sync.Pool{
			New: func() interface{} {
				return make([]float64, 0, 48) // CFF spec max stack size
			},
		},
		commandPool: sync.Pool{
			New: func() interface{} {
				return make([]interface{}, 0, 32) // Reasonable initial capacity
			},
		},
	}
}

// GetStack retrieves a stack slice from the pool
func (p *CFFObjectPool) GetStack() []float64 {
	return p.stackPool.Get().([]float64)[:0] // Reset length but keep capacity
}

// PutStack returns a stack slice to the pool
func (p *CFFObjectPool) PutStack(stack []float64) {
	if cap(stack) <= 48 { // Only pool reasonable sizes
		p.stackPool.Put(stack[:0])
	}
}

// GetCommandSlice retrieves a command slice from the pool
func (p *CFFObjectPool) GetCommandSlice() []interface{} {
	return p.commandPool.Get().([]interface{})[:0] // Reset length but keep capacity
}

// PutCommandSlice returns a command slice to the pool
func (p *CFFObjectPool) PutCommandSlice(commands []interface{}) {
	if cap(commands) <= 128 { // Only pool reasonable sizes
		p.commandPool.Put(commands[:0])
	}
}

// Global CFF object pool instance
var globalCFFPool *CFFObjectPool

// init initializes the global CFF object pool
func init() {
	globalCFFPool = NewCFFObjectPool()
}

// GetGlobalCFFPool returns the global CFF object pool instance
func GetGlobalCFFPool() *CFFObjectPool {
	return globalCFFPool
}
