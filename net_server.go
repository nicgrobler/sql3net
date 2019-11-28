package main

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	NET_LISTENER_PROTOCOL = "tcp"
	MAX_BUFFER_BYTES      = 4048
)

// Conn is a thin wrapper around net.Conn that allows us to set deadline timers as well as a buffer size limit
type QConn struct {
	Conn          net.Conn
	pool          *connPool
	IdleTimeout   time.Duration
	MaxReadBuffer int64
}

func (c *QConn) Write(p []byte) (n int, err error) {
	if e := c.incrementDeadline(); e != nil {
		return 0, e
	}
	n, err = c.Conn.Write(p)
	return
}

func (c *QConn) Read(b []byte) (n int, err error) {
	if e := c.incrementDeadline(); e != nil {
		return 0, e
	}
	r := io.LimitReader(c.Conn, c.MaxReadBuffer)
	n, err = r.Read(b)
	return
}

func (c *QConn) Close() (err error) {
	err = c.Conn.Close()
	// delete myself from the pool
	c.pool.deleteFromPool(c)
	c.pool = nil
	return
}

func (c *QConn) incrementDeadline() error {
	idleDeadline := time.Now().Add(c.IdleTimeout)
	err := c.Conn.SetDeadline(idleDeadline)
	return err

}

// connPool is a type that allows us to keep track of connections so we can "drain" them on shutdown
type connPool struct {
	locker      *sync.Mutex
	connections map[*QConn]struct{}
}

func (s *connPool) addToPool(connection *QConn) {
	s.locker.Lock()
	// add reference to pool to enable self-removal from the pool following closure
	connection.pool = s
	s.connections[connection] = struct{}{}
	s.locker.Unlock()
}

func (s *connPool) deleteFromPool(connection *QConn) {
	s.locker.Lock()
	delete(s.connections, connection)
	s.locker.Unlock()
}

func (s *NetServer) checkPoolSize(done chan struct{}) {
	// tick every 100ms
	t := time.NewTicker(time.Duration(100000) * time.Nanosecond)
	for {
		<-t.C
		if len(s.connections) == 0 {
			done <- struct{}{}
			return
		}
	}
}

func (s *NetServer) stopListener(ctx context.Context) error {
	var err error
	// prevent any new connections coming in
	if err = s.listener.Close(); err != nil {
		return err
	}
	// drain the pool
	done := make(chan struct{}, 1)
	go s.checkPoolSize(done)
	for {
		select {
		case <-ctx.Done():
			log.Warnf("pool still contains %v connections, force closing", len(s.connections))
			return ctx.Err()
		case <-done:
			log.Debugf("pool contains %v connoections, drained ok", len(s.connections))
			return nil
		}
	}

}

type Tracker struct {
	sync.Mutex
	Shutdown bool
}

func (t *Tracker) isShutdown() bool {
	t.Lock()
	defer t.Unlock()
	return t.Shutdown
}

func (t *Tracker) setShutdown() {
	t.Lock()
	defer t.Unlock()
	t.Shutdown = true
}

// NetServer is the basic abstraction used for handling net requests
type NetServer struct {
	slug     string
	address  string
	Done     chan error
	listener net.Listener
	handler  func(*QConn)
	// default value of shutDown is 0. 1 means "yes" shutdown called
	t *Tracker
	connPool
}

// NewNETServer takes a valid address - can be of form IP:Port, or :Port - and returns a server
func NewNETServer(ctx context.Context, description, address string) *NetServer {
	n := &NetServer{slug: description, address: address, Done: make(chan error), t: &Tracker{}}

	config := &net.ListenConfig{}

	l, err := config.Listen(ctx, NET_LISTENER_PROTOCOL, n.address)
	if err != nil {
		log.Fatalf("net listen:%+s", err)
	}
	n.listener = l
	n.connPool = connPool{locker: &sync.Mutex{}, connections: make(map[*QConn]struct{})}

	return n
}

// RegisterHandler allows caller to set handler function that actually does the work on the connections
func (s *NetServer) RegisterHandler(handlerfn func(*QConn)) {
	s.handler = handlerfn
}

// StartListener starts the server's listener with a context, allowing for later graceful shutdown.
// the supplied timeout is the amount of time that is allowed before the server forcefully
// closes any remaining conections. Once done close Done channel
// note: this is a blocking call
func (s *NetServer) StartListener(ctx context.Context, timeout time.Duration) {
	var err error
	go func() {
		for {
			// Listen for an incoming connection.
			connection, err := s.listener.Accept()
			if err != nil {
				// check flag
				if !s.t.isShutdown() {
					// not in shutdown, so log error
					log.Fatalf("error accepting: %s", err)
				}
				// is set, so return
				return
			}
			// wrap the listner with IdleTimeout
			c := QConn{Conn: connection, IdleTimeout: timeout, MaxReadBuffer: MAX_BUFFER_BYTES}

			// add this connection to our pool
			s.addToPool(&c)
			c.Conn.SetDeadline(time.Now().Add(c.IdleTimeout))

			log.Debugf("accepted connection from: %v", c.Conn.RemoteAddr())

			// run the handler
			go s.handler(&c)

		}
	}()
	defer s.listener.Close()

	log.Infoln(s.slug + " on: " + s.address)

	<-ctx.Done()

	// set the shutdown flag
	s.t.setShutdown()

	log.Infoln(s.slug + " stopping")
	ctxShutDown, cancel := context.WithTimeout(context.Background(), timeout)
	defer func() {
		cancel()
	}()

	if err = s.stopListener(ctxShutDown); err != nil {
		log.Warnf(s.slug+" graceful shutdown failed:%+s", err)
	}

	log.Infoln(s.slug + " stopped")

	// let parent know that we are done
	close(s.Done)

}

// GetDBName returns a string representation of the remote address of this connection
func (c *QConn) GetDBName() string {
	if addr, ok := c.Conn.RemoteAddr().(*net.TCPAddr); ok {
		return addr.IP.String() + ".db"
	}
	return c.Conn.RemoteAddr().String() + ".db"
}
