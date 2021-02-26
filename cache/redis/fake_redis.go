// Copyright 2020 Google LLC. All Rights Reserved.

package redis

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"testing"
)

// FakeServer is a fake redis server for stress test.
type FakeServer struct {
	ln net.Listener
	tb testing.TB
}

// NewFakeServer starts a new fake redis server.
func NewFakeServer(tb testing.TB) *FakeServer {
	ln, err := net.Listen("tcp", "")
	if err != nil {
		tb.Fatal(err)
	}
	s := &FakeServer{ln: ln, tb: tb}
	go s.serve()
	tb.Cleanup(func() { s.Close() })
	return s
}

// Addr returns address of the fake redis server.
func (s *FakeServer) Addr() net.Addr {
	return s.ln.Addr()
}

// Close shuts down the fake redis server.
func (s *FakeServer) Close() {
	s.ln.Close()
}

func (s *FakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *FakeServer) handle(conn net.Conn) {
	defer conn.Close()
	b := bufio.NewReader(conn)
	for {
		_, err := s.readRequest(b)
		if err != nil {
			return
		}
		// s.tb.Logf("request: %q", line)
		// assume GET
		// *2\r\n$3\r\nGET\r\n$3\r\nkey\r\n

		conn.Write([]byte("$10\r\n0123456789\r\n"))
	}
}

func (s *FakeServer) readRequest(r *bufio.Reader) ([]byte, error) {
	var line []byte
	nline, _, err := r.ReadLine()
	if err != nil {
		return nil, err
	}
	line = append(line, nline...)
	if !bytes.HasPrefix(nline, []byte("*")) {
		return line, err
	}
	// *<n> array
	n, err := strconv.Atoi(string(nline[1:]))
	if err != nil {
		return line, fmt.Errorf("wrong array %q: %v", nline, err)
	}
	for i := 0; i < n; i++ {
		nline, _, err := r.ReadLine()
		if err != nil {
			return line, err
		}
		line = append(line, '\n')
		line = append(line, nline...)
		if !bytes.HasPrefix(nline, []byte("$")) {
			continue
		}
		// $<n>\r\n<value>\r\n
		sz, err := strconv.Atoi(string(nline[1:]))
		if err != nil {
			return line, fmt.Errorf("wrong bytes %q: %v", nline, err)
		}
		nline, _, err = r.ReadLine()
		if err != nil {
			return line, err
		}
		line = append(line, '\n')
		line = append(line, nline...)
		if sz != len(nline) {
			return line, fmt.Errorf("unexpected value sz=%d v=%q", sz, nline)
		}
	}
	return line, nil
}
