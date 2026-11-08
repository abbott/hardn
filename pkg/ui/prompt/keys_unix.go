//go:build linux || darwin

package prompt

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// keySignals are the signals that end hardn by default and that a key read
// catches, to put the terminal back before they take their default action:
// SIGINT is not among them, as the read's own relay wakes it (interrupt),
// and the keyboard sends no signal while it reads keys (ISIG off).
var keySignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// raise sends s to the process once its handling is given back, so that it
// takes its default action. Tests replace it.
var raise = func(s syscall.Signal) { _ = syscall.Kill(syscall.Getpid(), s) }

// keyMode is the terminal set for one key read: its attributes as they
// were, to put back, and the relay that puts them back when a keySignal
// comes.
type keyMode struct {
	conn syscall.Conn
	// mu guards the terminal's attributes as the read sets them and puts
	// them back, from the relay too: saved, read when got, and back once
	// put back, after which nothing sets them again.
	mu        sync.Mutex
	saved     unix.Termios
	got, back bool
	gone      atomic.Bool // a keySignal put the terminal back
	sigs      chan os.Signal
	done      chan struct{}
	wg        sync.WaitGroup
}

// errPutBack: a keySignal put the terminal back before it was set.
var errPutBack = errors.New("prompt: the terminal was put back")

// startKeys sets the terminal for a key read and returns how to put it
// back, or nil when the Reader is not a terminal or its attributes cannot
// be read or set; the prompt is then a line. The terminal's line editing,
// echo, keyboard signals and extended input processing (ICANON, ECHO,
// ISIG, IEXTEN) go off, and a read returns each byte as it comes (VMIN 1,
// VTIME 0); its output processing stays, so "\n" still ends a line. The
// relay is running before the terminal is set. A keySignal that is ignored
// is left so: it ends nothing.
func (r *Reader) startKeys() *keyMode {
	c, ok := r.in.(syscall.Conn)
	if !r.tty || !ok {
		return nil
	}
	km := &keyMode{conn: c, sigs: make(chan os.Signal, 1), done: make(chan struct{})}
	var caught []os.Signal
	for _, s := range keySignals {
		if !signal.Ignored(s) {
			caught = append(caught, s)
		}
	}
	if len(caught) > 0 { // Notify with no signal would relay every one
		signal.Notify(km.sigs, caught...)
	}
	km.wg.Go(func() {
		select {
		case s := <-km.sigs:
			km.put()
			km.gone.Store(true)
			signal.Stop(km.sigs)
			if sig, ok := s.(syscall.Signal); ok {
				raise(sig)
			}
		case <-km.done:
		}
	})
	km.mu.Lock()
	err := control(c, func(fd int) error {
		if km.back {
			return errPutBack
		}
		t, err := getTermios(fd)
		if err != nil {
			return err
		}
		km.saved, km.got = *t, true
		k := *t
		k.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
		k.Cc[unix.VMIN], k.Cc[unix.VTIME] = 1, 0
		return setTermios(fd, &k)
	})
	km.mu.Unlock()
	if err != nil {
		km.restore() // what a set that failed part-way changed, and the relay
		return nil
	}
	return km
}

// put puts the terminal's attributes back, once, where they were read;
// after it nothing sets them.
func (km *keyMode) put() {
	km.mu.Lock()
	defer km.mu.Unlock()
	if km.back {
		return
	}
	km.back = true
	if km.got {
		_ = control(km.conn, func(fd int) error { return setTermios(fd, &km.saved) })
	}
}

// ended reports whether a keySignal put the terminal back during the read
// and hardn goes on, as where another handler takes it: the read ends.
func (km *keyMode) ended() bool { return km.gone.Load() }

// restore puts the terminal back and ends the relay. The terminal comes
// first, so that a keySignal from then on finds it as it was; one that came
// while the relay was being stopped is raised.
func (km *keyMode) restore() {
	km.put()
	signal.Stop(km.sigs)
	close(km.done)
	km.wg.Wait()
	select {
	case s := <-km.sigs:
		if sig, ok := s.(syscall.Signal); ok {
			raise(sig)
		}
	default:
	}
}

// wait blocks until the input has a byte to read (true), until timeout
// passes with none (false; zero waits without limit), or until
// os.Interrupt wakes it (errWoken).
func (km *keyMode) wait(it *interrupt, timeout time.Duration) (bool, error) {
	wake := -1
	if it != nil {
		wake = int(it.wake.Fd())
	}
	var ready bool
	err := control(km.conn, func(fd int) error {
		var err error
		ready, err = selectKey(fd, wake, timeout)
		return err
	})
	return ready, err
}

// selectKey waits with select(2), as selectRead does, until fd or wake
// (when not -1) is readable or timeout (when not zero) passes.
func selectKey(fd, wake int, timeout time.Duration) (bool, error) {
	if top := max(fd, wake); top >= unix.FD_SETSIZE {
		return false, fmt.Errorf("prompt: descriptor %d is beyond select's limit of %d", top, unix.FD_SETSIZE)
	}
	deadline := time.Now().Add(timeout)
	for {
		var set unix.FdSet
		set.Set(fd)
		if wake >= 0 {
			set.Set(wake)
		}
		var tv *unix.Timeval
		if timeout > 0 {
			t := unix.NsecToTimeval(max(time.Until(deadline), 0).Nanoseconds())
			tv = &t
		}
		n, err := unix.Select(max(fd, wake)+1, &set, nil, nil, tv)
		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case err != nil:
			return false, fmt.Errorf("prompt: select: %w", err)
		case wake >= 0 && set.IsSet(wake):
			return false, errWoken
		}
		return n > 0, nil
	}
}

// control runs f on c's descriptor (syscall.RawConn.Control).
func control(c syscall.Conn, f func(fd int) error) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = f(int(fd)) }); err != nil {
		return err
	}
	return ferr
}
