package smartdevice

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Config is how a Server presents itself.
type Config struct {
	// Port is the TCP port readers connect to; 0 tries DefaultPort, then
	// any free one: readers learn it from the discovery answer.
	Port int
	// Name is this computer's, said in the discovery answer.
	Name string
	// ContentPort is said in the discovery answer as calibre's content
	// server port; readers ignore it, but it must be a number.
	ContentPort int
	// Password, when set, is what readers must have been given.
	Password string
	// Library tells readers which library sends: its name and uuid,
	// asked as each reader connects.
	Library func() (name, uuid string)
	// OnChange is called, on any goroutine, when a reader connects or
	// goes, and when what it has changes.
	OnChange func()
}

// Server waits for a reader and keeps the one that connected.
type Server struct {
	cfg Config
	ln  net.Listener
	udp net.PacketConn // nil when no broadcast port was free

	mu      sync.Mutex
	dev     *Device
	pending bool // a reader is connecting
	lastErr error
	closed  bool
}

// Listen starts serving: the TCP port readers connect to and, when one is
// free, a broadcast port they find it by.
func Listen(cfg Config) (*Server, error) {
	var ln net.Listener
	var err error
	if cfg.Port > 0 {
		ln, err = net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	} else {
		ln, err = net.Listen("tcp", ":"+strconv.Itoa(DefaultPort))
		if err != nil {
			ln, err = net.Listen("tcp", ":0")
		}
	}
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, ln: ln}
	for _, p := range BroadcastPorts {
		if pc, err := net.ListenPacket("udp4", ":"+strconv.Itoa(p)); err == nil {
			s.udp = pc
			break
		}
	}
	if s.udp != nil {
		go s.answerDiscovery()
	}
	go s.accept()
	return s, nil
}

// Port is the TCP port readers connect to.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Discoverable reports whether readers can find the server by themselves;
// otherwise they need its address.
func (s *Server) Discoverable() bool { return s.udp != nil }

// Device is the reader connected, nil when none is.
func (s *Server) Device() *Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dev
}

// Err is why the last reader that tried couldn't connect, nil once one
// does.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Close stops serving and lets the reader connected go.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	dev := s.dev
	s.mu.Unlock()
	if s.udp != nil {
		s.udp.Close()
	}
	err := s.ln.Close()
	if dev != nil {
		dev.Disconnect()
	}
	return err
}

func (s *Server) changed() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
}

// discoveryAnswer is what a reader's broadcast is answered with:
// "calibre wireless device client (on NAME);CONTENT,PORT". KOReader takes
// the last number as the port to connect to.
func (s *Server) discoveryAnswer() []byte {
	name := strings.Map(func(r rune) rune {
		if r == ')' || r == ';' || r < ' ' {
			return -1
		}
		return r
	}, s.cfg.Name)
	if name == "" {
		name = "Dalibre"
	}
	return []byte(discoveryPrefix + " (on " + name + ");" + strconv.Itoa(max(s.cfg.ContentPort, 0)) + "," + strconv.Itoa(s.Port()))
}

func (s *Server) answerDiscovery() {
	buf := make([]byte, 512)
	for {
		_, from, err := s.udp.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		s.udp.WriteTo(s.discoveryAnswer(), from)
	}
}

func (s *Server) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		s.mu.Lock()
		busy := s.dev != nil || s.pending || s.closed
		other := ""
		if s.dev != nil {
			other = s.dev.Info().Name
		}
		if !busy {
			s.pending = true
		}
		s.mu.Unlock()
		if busy {
			// As calibre does: one reader at a time.
			go func() {
				writeFrame(conn, opCalibreBusy, map[string]any{"otherDevice": other})
				conn.Close()
			}()
			continue
		}
		go s.serve(conn)
	}
}

// serve greets a reader that connected and keeps it until it goes.
func (s *Server) serve(conn net.Conn) {
	d := newDevice(s, conn)
	err := d.handshake()
	s.mu.Lock()
	s.pending = false
	if err == nil && s.closed {
		err = net.ErrClosed
	}
	if err != nil {
		if !errors.Is(err, net.ErrClosed) {
			s.lastErr = err
		}
		s.mu.Unlock()
		d.close()
		s.changed()
		return
	}
	s.dev, s.lastErr = d, nil
	s.mu.Unlock()
	s.changed()
	go d.keepAlive()
	<-d.done
	s.mu.Lock()
	if s.dev == d {
		s.dev = nil
	}
	s.mu.Unlock()
	s.changed()
}
