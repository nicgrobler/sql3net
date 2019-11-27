package main

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestIncrement(t *testing.T) {
	c := &QConn{}
	c.MaxReadBuffer = int64(10)
	fc := GetFakeConn("0.0.0.0")
	c.Conn = fc
	idleTimeout := 100 * time.Second
	c.IdleTimeout = idleTimeout

	shouldGet := (time.Now().Add(idleTimeout)).Unix()

	c.Write([]byte{}) // this dirty hack causes write to write Dealine into internal data - which can then be retreived via call to Read
	deadlineBytes := make([]byte, 10)
	c.Read(deadlineBytes)

	got, _ := strconv.Atoi(string(deadlineBytes))

	assert.Equal(t, shouldGet, int64(got), "should be equal if increment of timeout was successful")

	b := make([]byte, 10)
	c.Read(b)
	assert.Equal(t, idleTimeout, c.IdleTimeout, "should be equal if increment of timeout was successful")
}

func TestWrite(t *testing.T) {
	c := &QConn{}
	c.MaxReadBuffer = int64(10)
	fc := GetFakeConn("0.0.0.0")
	c.Conn = fc
	timeAsInt := time.Now().Unix()
	bs := []byte(strconv.FormatInt(timeAsInt, 10))
	c.Write(bs)
	b := make([]byte, 10)
	c.Read(b)
	shouldBe := []byte(strconv.FormatInt(time.Now().Unix(), 10))
	assert.Equal(t, string(shouldBe), string(b), "These should be equal - to within 1 second")
}

func TestRead(t *testing.T) {
	c := &QConn{}
	// verify that we only get back the right number of bytes - this tests the limited reader
	c.MaxReadBuffer = int64(4)
	fc := GetFakeConn("22.22.22.22")
	c.Conn = fc
	timeAsInt := time.Now().Unix()
	bs := []byte(strconv.FormatInt(timeAsInt, 10))
	c.Write(bs)
	b := make([]byte, 4)
	c.Read(b)
	shouldBe := []byte(strconv.FormatInt(time.Now().Unix(), 10))
	assert.Equal(t, string(shouldBe[0:4]), string(b), "These should be equal - to within 1 second")
}

func TestClose(t *testing.T) {
	c := &QConn{}
	c.Conn = GetFakeConn("1.1.1.1")
	pool := getEmptyPool()
	pool.addToPool(c)
	// pool should have a single connection in it...
	assert.Equal(t, 1, len(pool.connections), "The pool should contain 1 connection")
	// verify that the connection cleans-up after itself by closing the connection and removing itself from the pool when Close() is called
	for tc := range pool.connections {
		tc.Close()
	}
	assert.Equal(t, 0, len(pool.connections), "The pool should contain no connections")
}

func getEmptyPool() connPool {
	return connPool{locker: &sync.Mutex{}, connections: make(map[*QConn]struct{})}
}

func getLoadedPool(numberOfConnections int) connPool {
	pool := connPool{locker: &sync.Mutex{}, connections: make(map[*QConn]struct{})}
	for i := 0; i < numberOfConnections; i++ {
		c := &QConn{}
		pool.addToPool(c)
	}
	return pool
}

func TestAddToPool(t *testing.T) {
	c := &QConn{}
	pool := getEmptyPool()
	pool.addToPool(c)
	// pool should have a single connection in it...
	assert.Equal(t, 1, len(pool.connections), "The pool should contain 1 connection")
	// verify that the connection is the SAME one we added
	for tc := range pool.connections {
		assert.Equal(t, c, tc, "The connection objects should be the same")
	}
}

func TestDeleteFromPool(t *testing.T) {
	pool := getLoadedPool(2)
	assert.Equal(t, 2, len(pool.connections), "The pool should contain 2 connections")
	for c := range pool.connections {
		pool.deleteFromPool(c)
	}
	assert.Equal(t, 0, len(pool.connections), "The pool should contain no connections")
}

func TestStartListener(t *testing.T) {
	log.SetLevel(log.WarnLevel)
	// grab ctx to pass onto server(s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan string, 1)

	// initialise net listener
	netServer := NewTESTNETServer(ctx, "net listener", "whatever:more")
	netServer.RegisterHandler(func(conn *QConn) { result <- conn.Conn.RemoteAddr().String() })

	// run net server's listener
	go netServer.StartListener(ctx, time.Duration(1*time.Second))

	assert.Equal(t, false, netServer.t.isShutdown(), "These should be equal")
	assert.Equal(t, "5.6.7.8", <-result, "These should be equal")

}

func TestStartListenerDuringShutdown(t *testing.T) {
	log.SetLevel(log.ErrorLevel)
	// grab ctx to pass onto server(s)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan string, 1)

	// initialise net listener
	netServer := NewTESTNETServer(ctx, "net listener", "whatever:more")
	netServer.RegisterHandler(func(conn *QConn) { result <- conn.Conn.RemoteAddr().String() })

	assert.Equal(t, false, netServer.t.isShutdown(), "These should be equal")

	var closed bool
	go func(c *bool) {
		<-netServer.Done
		*c = true
	}(&closed)

	// run net server's listener
	go netServer.StartListener(ctx, time.Duration(5*time.Millisecond))
	// give it some time to actually run
	time.Sleep(100 * time.Millisecond)
	// now signal it
	cancel()
	// give it some time to actually run
	time.Sleep(100 * time.Millisecond)
	// verify that the listener accepted the connection
	assert.Equal(t, "5.6.7.8", <-result, "These should be equal")

	// verify that the flag has been set to indicate shutdown state
	assert.Equal(t, true, netServer.t.isShutdown(), "These should be equal")
	assert.Equal(t, true, closed, "should be the same if channel closed")
}

/*
type Addr interface {
    Network() string // name of the network (for example, "tcp", "udp")
    String() string  // string form of address (for example, "192.0.2.1:25", "[2001:db8::1]:80")
}
*/

type mocked_addr struct {
	networkType   string
	networkString string
}

func (m mocked_addr) Network() string { return m.networkType }
func (m mocked_addr) String() string  { return m.networkString }

func getFakeAddress(t, s string) *mocked_addr {
	return &mocked_addr{networkType: t, networkString: s}
}

/*
type Conn interface {
    Read(b []byte) (n int, err error)
	Write(b []byte) (n int, err error)
	Close() error
	LocalAddr() Addr
	RemoteAddr() Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}
*/

type FakeCon struct {
	Local    net.Addr
	Remote   net.Addr
	Data     []byte
	Deadline time.Time
}

func GetFakeConn(localAddress string) *FakeCon {
	return &FakeCon{
		Local:  mocked_addr{networkType: "tcp", networkString: localAddress},
		Remote: mocked_addr{networkType: "tcp", networkString: "5.6.7.8"},
	}
}

/*
type Listener interface {
	Accept() (Conn, error)
	Close() error
	Addr() Addr
}
*/

type FakeListener struct {
	conn    *FakeCon
	address *mocked_addr
}

func GetFakeListener(address string) *FakeListener {
	f := &FakeListener{
		conn:    GetFakeConn(address),
		address: getFakeAddress("udp", address),
	}
	return f
}

func NewTESTNETServer(ctx context.Context, description, address string) *NetServer {
	n := &NetServer{slug: description, address: address, Done: make(chan error), t: &Tracker{}}
	n.listener = GetFakeListener(address)

	n.connPool = connPool{locker: &sync.Mutex{}, connections: make(map[*QConn]struct{})}

	return n
}

func (l *FakeListener) Accept() (net.Conn, error) { return l.conn, nil }
func (l *FakeListener) Close() error              { return nil }
func (l *FakeListener) Addr() net.Addr            { return mocked_addr{} }

func (f *FakeCon) Read(b []byte) (n int, err error) {
	if len(b) == 0 {
		for i := range f.Data {
			b = append(b, f.Data[i])
		}
		return len(b), io.EOF
	}
	for i := range b {
		if i == len(f.Data) {
			return i, io.EOF
		}
		b[i] = f.Data[i]
	}
	return len(b), io.EOF
}

func (f *FakeCon) Write(b []byte) (n int, err error) {
	// this is NOT a real implementation as the target is reset with each call to write
	// and is only designed to work for the testing we need
	if len(b) == 0 {
		// let's write our Deadline time into the data slice
		d := []byte(strconv.FormatInt(f.Deadline.Unix(), 10))
		for i := range d {
			f.Data = append(f.Data, d[i])
		}
		return len(b), nil
	}
	for i := range b {
		f.Data = append(f.Data, b[i])
	}
	return len(b), nil
}

func (f *FakeCon) Close() error                       { return nil }
func (f *FakeCon) LocalAddr() net.Addr                { return f.Local }
func (f *FakeCon) RemoteAddr() net.Addr               { return f.Remote }
func (f *FakeCon) SetDeadline(t time.Time) error      { f.Deadline = t; return nil }
func (f *FakeCon) SetReadDeadline(t time.Time) error  { return nil }
func (f *FakeCon) SetWriteDeadline(t time.Time) error { return nil }

// GetDBName returns a string representation of the remote address of this connection
func (f *FakeCon) GetDBName() string {
	return "mysterious.db"
}
