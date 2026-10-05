package proxy

import "sync"

// copyBufferSize matches the buffer httputil.ReverseProxy allocates per
// response when it has no BufferPool.
const copyBufferSize = 32 * 1024

// bufferPool reuses response copy buffers across proxied requests. It
// implements httputil.BufferPool.
type bufferPool struct {
	pool sync.Pool
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{New: func() any {
			buf := make([]byte, copyBufferSize)
			return &buf
		}},
	}
}

func (bp *bufferPool) Get() []byte {
	return *bp.pool.Get().(*[]byte)
}

func (bp *bufferPool) Put(buf []byte) {
	bp.pool.Put(&buf)
}
