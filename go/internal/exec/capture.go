package exec

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"
)

// stdoutCapture buffers stdout up to a limit, then replays the buffered prefix
// exactly once and streams the rest directly. It never omits or duplicates a
// byte and never emits a candidate after transition.
type stdoutCapture struct {
	mu       sync.Mutex
	buf      []byte
	limit    int
	lineLim  int
	curLine  int
	raw      bool
	done     bool
	complete bool
	writeErr error
	total    int64

	out   io.Writer
	stop  chan struct{}
	timer *time.Timer
	errCh chan struct{}
	once  sync.Once
}

func newStdoutCapture(policy *CapturePolicy, out io.Writer) *stdoutCapture {
	c := &stdoutCapture{out: out, stop: make(chan struct{}), errCh: make(chan struct{})}
	if policy == nil {
		c.raw = true
		return c
	}
	c.limit = policy.MemoryLimit
	c.lineLim = policy.LineLimit
	if !policy.Deadline.IsZero() {
		d := time.Until(policy.Deadline)
		if d <= 0 {
			c.raw = true
		} else {
			c.timer = time.AfterFunc(d, func() {
				c.mu.Lock()
				c.switchRawLocked()
				c.mu.Unlock()
			})
		}
	}
	return c
}

func (c *stdoutCapture) read(r *os.File) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c.mu.Lock()
			c.acceptLocked(buf[:n])
			c.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *stdoutCapture) acceptLocked(p []byte) {
	c.total += int64(len(p))
	if c.raw {
		c.writeLocked(p)
		return
	}
	if c.lineLim > 0 {
		for _, b := range p {
			if b == '\n' {
				c.curLine = 0
				continue
			}
			c.curLine++
			if c.curLine > c.lineLim {
				c.switchRawLocked()
				c.writeLocked(p)
				return
			}
		}
	}
	if c.limit > 0 && len(c.buf)+len(p) > c.limit {
		c.switchRawLocked()
		c.writeLocked(p)
		return
	}
	c.buf = append(c.buf, p...)
}

// switchRawLocked flushes the captured prefix once and enters streaming mode.
func (c *stdoutCapture) switchRawLocked() {
	if c.raw || c.done {
		return
	}
	c.raw = true
	if len(c.buf) > 0 {
		c.writeLocked(c.buf)
		c.buf = nil
	}
}

func (c *stdoutCapture) writeLocked(p []byte) {
	if c.writeErr != nil {
		return
	}
	if _, err := c.out.Write(p); err != nil {
		c.writeErr = err
		c.once.Do(func() { close(c.errCh) })
	}
}

func (c *stdoutCapture) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	if c.timer != nil {
		c.timer.Stop()
	}
	if !c.raw {
		c.complete = true
	}
}

func (c *stdoutCapture) data() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.complete {
		return nil
	}
	out := make([]byte, len(c.buf))
	copy(out, c.buf)
	return out
}

func (c *stdoutCapture) isComplete() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.complete
}

func (c *stdoutCapture) stats() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total, c.writeErr
}

// mirrorWriter forwards stderr immediately and keeps a bounded copy.
type mirrorWriter struct {
	mu       sync.Mutex
	out      io.Writer
	buf      bytes.Buffer
	limit    int
	enabled  bool
	complete bool
	writeErr error
	total    int64
	errCh    chan struct{}
	once     sync.Once
}

func newMirror(out io.Writer, limit int) *mirrorWriter {
	return &mirrorWriter{out: out, limit: limit, enabled: limit > 0, complete: limit > 0, errCh: make(chan struct{})}
}

func (m *mirrorWriter) read(r *os.File) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			m.write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (m *mirrorWriter) write(p []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total += int64(len(p))
	if m.writeErr == nil {
		if _, err := m.out.Write(p); err != nil {
			m.writeErr = err
			m.once.Do(func() { close(m.errCh) })
		}
	}
	if !m.enabled || !m.complete {
		return
	}
	if m.buf.Len()+len(p) > m.limit {
		m.complete = false
		m.buf.Reset()
		return
	}
	m.buf.Write(p)
}

func (m *mirrorWriter) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled {
		return
	}
	if m.total <= int64(m.limit) && m.writeErr == nil {
		m.complete = true
	}
}

func (m *mirrorWriter) data() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.complete {
		return nil
	}
	return append([]byte(nil), m.buf.Bytes()...)
}

func (m *mirrorWriter) isComplete() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.complete
}

func (m *mirrorWriter) stats() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.total, m.writeErr
}
