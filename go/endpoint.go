package fastway

import (
	"errors"
	"io"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/funny/link"
	"github.com/funny/slab"
)

// ErrRefused happens when virtual connection couldn't dial to remote endpoint.
var ErrRefused = errors.New("virtual connection refused")

// DialClient dial to gateway and return a client endpoint.
// addr is the gateway address.
// pool used to pooling message buffers.
// maxPacketSize limits max packet size.
// bufferSize settings bufio.Reader memory usage.
// sendChanSize settings async sending behavior for physical connection.
// recvChanSize settings async receiving behavior for virtual connection.
func DialClient(network, addr string, pool slab.Pool, maxPacketSize, bufferSize, sendChanSize, recvChanSize int) (*Endpoint, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return NewClient(conn, pool, maxPacketSize, bufferSize, sendChanSize, recvChanSize)
}

// DialServer dial to gateway and return a server endpoint.
// addr is the gateway address.
// pool used to pooling message buffers.
// serverID is the server ID of current server.
// key is the auth key used in server handshake.
// authTimeout is the IO waiting timeout when server handshake.
// maxPacketSize limits max packet size.
// bufferSize settings bufio.Reader memory usage.
// sendChanSize settings async sending behavior for physical connection.
// recvChanSize settings async receiving behavior for virtual connection.
func DialServer(network, addr string, pool slab.Pool, serverID uint32, key string, authTimeout time.Duration, maxPacketSize, bufferSize, sendChanSize, recvChanSize int) (*Endpoint, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return NewServer(conn, pool, serverID, key, authTimeout, maxPacketSize, bufferSize, sendChanSize, recvChanSize)
}

// NewClient dial to gateway and return a client endpoint.
// conn is the physical connection.
// pool used to pooling message buffers.
// maxPacketSize limits max packet size.
// bufferSize settings bufio.Reader memory usage.
// sendChanSize settings async sending behavior for physical connection.
// recvChanSize settings async receiving behavior for virtual connection.
func NewClient(conn net.Conn, pool slab.Pool, maxPacketSize, bufferSize, sendChanSize, recvChanSize int) (*Endpoint, error) {
	ep := newEndpoint(pool, maxPacketSize, recvChanSize)
	ep.session = link.NewSession(ep.newCodec(conn, bufferSize), sendChanSize)
	go ep.loop()
	return ep, nil
}

// NewServer dial to gateway and return a server endpoint.
// conn is the physical connection.
// pool used to pooling message buffers.
// serverID is the server ID of current server.
// key is the auth key used in server handshake.
// authTimeout is the IO waiting timeout when server handshake.
// maxPacketSize limits max packet size.
// bufferSize settings bufio.Reader memory usage.
// sendChanSize settings async sending behavior for physical connection.
// recvChanSize settings async receiving behavior for virtual connection.
func NewServer(conn net.Conn, pool slab.Pool, serverID uint32, key string, authTimeout time.Duration, maxPacketSize, bufferSize, sendChanSize, recvChanSize int) (*Endpoint, error) {
	ep := newEndpoint(pool, maxPacketSize, recvChanSize)
	if err := ep.serverInit(conn, serverID, []byte(key), authTimeout); err != nil {
		return nil, err
	}
	ep.session = link.NewSession(ep.newCodec(conn, bufferSize), sendChanSize)
	go ep.loop()
	return ep, nil
}

type vconn struct {
	Session  *link.Session
	ConnID   uint32
	RemoteID uint32
}

// Endpoint is can be a client or a server.
type Endpoint struct {
	protocol
	recvChanSize int
	session      *link.Session
	lastActive   int64
	newConnMutex sync.Mutex
	newConnChan  chan uint32
	dialMutex    sync.Mutex
	acceptChan   chan vconn
	connectChan  chan vconn
	virtualConns *link.Uint32Channel
	closeChan    chan struct{}
	closeOnce    sync.Once
}

func newEndpoint(pool slab.Pool, maxPacketSize, recvChanSize int) *Endpoint {
	return &Endpoint{
		protocol: protocol{
			pool:          pool,
			maxPacketSize: maxPacketSize,
		},
		recvChanSize: recvChanSize,
		newConnChan:  make(chan uint32),
		acceptChan:   make(chan vconn),
		connectChan:  make(chan vconn),
		virtualConns: link.NewUint32Channel(),
		closeChan:    make(chan struct{}),
	}
}

// Accept accept a virtual connection.
func (p *Endpoint) Accept() (session *link.Session, connID, remoteID uint32, err error) {
	select {
	case conn := <-p.connectChan:
		return conn.Session, conn.ConnID, conn.RemoteID, nil
	case <-p.closeChan:
		return nil, 0, 0, io.EOF
	}
}

// Dial create a virtual connection and dial to a remote endpoint.
func (p *Endpoint) Dial(remoteID uint32) (*link.Session, uint32, error) {
	p.dialMutex.Lock()
	defer p.dialMutex.Unlock()

	if err := p.send(p.session, p.encodeDialCmd(remoteID)); err != nil {
		return nil, 0, err
	}

	select {
	case conn := <-p.acceptChan:
		if conn.Session == nil {
			return nil, 0, ErrRefused
		}
		return conn.Session, conn.ConnID, nil
	case <-p.closeChan:
		return nil, 0, io.EOF
	}
}

// Close endpoint.
func (p *Endpoint) Close() {
	p.closeOnce.Do(func() {
		p.session.Close()
		close(p.closeChan)
	})
}

// Ping send ping command to gateway.
func (p *Endpoint) Ping() error {
	return p.send(p.session, p.encodePingCmd())
}

// LastActive returns endpoint last active time.
func (p *Endpoint) LastActive() time.Time {
	return time.Unix(atomic.LoadInt64(&p.lastActive), 0)
}

func (p *Endpoint) addVirtualConn(connID, remoteID uint32, c chan vconn) {
	codec := p.newVirtualCodec(p.session, connID, p.recvChanSize, &p.lastActive)
	session := link.NewSession(codec, 0)
	p.virtualConns.Put(connID, session)
	select {
	case c <- vconn{session, connID, remoteID}:
	case <-p.closeChan:
	default:
		p.send(p.session, p.encodeCloseCmd(connID))
	}
}

func (p *Endpoint) loop() {
	defer func() {
		p.Close()
		if err := recover(); err != nil {
			log.Printf("fast/gateway.Endpoint panic: %v\n%s", err, debug.Stack())
		}
	}()
	for {
		atomic.StoreInt64(&p.lastActive, time.Now().Unix())

		msg, err := p.session.Receive()
		if err != nil {
			return
		}

		buf := *(msg.(*[]byte))
		connID := p.decodePacket(buf)

		if connID == 0 {
			p.processCmd(buf)
			continue
		}

		vconn := p.virtualConns.Get(connID)
		if vconn != nil {
			select {
			case vconn.Codec().(*virtualCodec).recvChan <- buf:
				continue
			default:
				vconn.Close()
			}
		}
		p.free(buf)
		p.send(p.session, p.encodeCloseCmd(connID))
	}
}

func (p *Endpoint) processCmd(buf []byte) {
	cmd := p.decodeCmd(buf)
	switch cmd {
	case acceptCmd:
		connID, remoteID := p.decodeAcceptCmd(buf)
		p.free(buf)
		p.addVirtualConn(connID, remoteID, p.acceptChan)

	case refuseCmd:
		remoteID := p.decodeRefuseCmd(buf)
		p.free(buf)
		select {
		case p.acceptChan <- vconn{nil, 0, remoteID}:
		case <-p.closeChan:
			return
		}

	case connectCmd:
		connID, remoteID := p.decodeConnectCmd(buf)
		p.free(buf)
		p.addVirtualConn(connID, remoteID, p.connectChan)

	case closeCmd:
		connID := p.decodeCloseCmd(buf)
		p.free(buf)
		vconn := p.virtualConns.Get(connID)
		if vconn != nil {
			vconn.Close()
		}

	case pingCmd:
		p.free(buf)

	default:
		p.free(buf)
		panic("unsupported command")
	}
}
