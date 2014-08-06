package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
	"tunnel/shared"
)

type (
	TunnelNode struct {
		Arg        string
		LocalConn  net.Conn
		RemoteConn net.Conn
		Created    time.Time
	}
)

var (
	Tunnels map[string]*TunnelNode = make(map[string]*TunnelNode, 64)
)

func Stage1(c net.Conn) {
	var e error

	e = c.SetDeadline(time.Now().Add(time.Minute))
	if e == nil {
		var buffer []byte = make([]byte, 1024)
		var length int

		length, e = c.Read(buffer)
		if e == nil {
			if length > 0 {
				var ss []string = strings.Split(string(buffer[:length]), ":")
				if len(ss) >= 2 {
					cmd := strings.TrimSpace(ss[0])
					arg := strings.TrimSpace(ss[1])
					cmd = strings.ToLower(cmd)
					tnn, ok := Tunnels[arg]
					if cmd == shared.CreateCommand {
						if !ok {
							tnn = new(TunnelNode)
							tnn.Arg = arg
							tnn.Created = time.Now()
							tnn.LocalConn = c
							Tunnels[arg] = tnn
							return
						} else {
							c.Write([]byte(fmt.Sprintf("%s: %s", shared.ErrorResponse, "Already Exists")))
						}
					} else if cmd == shared.AttachCommand {
						if ok {
							if tnn.RemoteConn == nil {
								tnn.RemoteConn = c
								go tnn.Stage2()
								return
							} else {
								c.Write([]byte(fmt.Sprintf("%s: %s", shared.ErrorResponse, "Already Attached")))
							}
						} else {
							c.Write([]byte(fmt.Sprintf("%s: %s", shared.ErrorResponse, "Not Found")))
						}
					}
				}
			}
		}
	}

	c.Close()
}

func (tunnel *TunnelNode) Stage2() {
	tunnel.LocalConn.Write([]byte(shared.OkResponse))
	tunnel.RemoteConn.Write([]byte(shared.OkResponse))

	tunnel.LocalConn.SetDeadline(time.Unix(0, 0))
	tunnel.RemoteConn.SetDeadline(time.Unix(0, 0))

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(tunnel.RemoteConn, tunnel.LocalConn)
		tunnel.LocalConn.Close()
		tunnel.RemoteConn.Close()
	}()

	go func() {
		defer wg.Done()
		io.Copy(tunnel.LocalConn, tunnel.RemoteConn)
		tunnel.LocalConn.Close()
		tunnel.RemoteConn.Close()
	}()

	wg.Wait()

	log.Println("Closed")

	tunnel.LocalConn = nil
	tunnel.RemoteConn = nil
	delete(Tunnels, tunnel.Arg)
}

func main() {
	var port int
	flag.IntVar(&port, "port", 8181, "port number")
	flag.Parse()

	var e error
	var listner net.Listener

	log.Printf("Listening on port #%d\n", port)

	listner, e = net.Listen("tcp", fmt.Sprintf(":%d", port))
	if e != nil {
		log.Panicln(e)
	}

	defer listner.Close()
	for {
		var c net.Conn

		c, e = listner.Accept()
		if e != nil {
			log.Panicln(e)
		}

		go Stage1(c)
	}
}
