package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"tunnel/shared"
)

type (
	RemoteBackbone struct {
		shared.BackboneConn
	}
)

var (
	backbone *RemoteBackbone
	dialTo   string = "127.1:8183"
)

func (bb *RemoteBackbone) OnCommandReceived(command string, sessionId uint16) bool {
	log.Printf("Command received: %s:%d", command, sessionId)
	if command == shared.AttachCommand {
		var e error
		var remoteConn net.Conn
		remoteConn, e = net.Dial("tcp", dialTo)
		if e == nil {
			rc := new(shared.RibConn)
			rc.Conn = remoteConn
			rc.SessionId = sessionId
			backbone.Attach(rc, false)
		}
		return true
	}
	return false
}

func main() {
	log.Println("Dialing...")

	//connect
	var e error
	var serverCon net.Conn
	serverCon, e = net.Dial("tcp", "127.1:8181")
	if e != nil {
		log.Panic(e)
	}

	// create end-point
	serverCon.Write([]byte(fmt.Sprintf("%s:%s", shared.AttachCommand, "111")))
	var b []byte = make([]byte, 256)
	var r int
	r, e = serverCon.Read(b)
	if e != nil {
		log.Panic(e)
	}
	s := string(b[:r])
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	if s != shared.OkResponse {
		log.Panic(fmt.Sprintf("Incorrect response: %s", s))
	}
	b = nil

	log.Println("Connected")

	backbone = new(RemoteBackbone)
	backbone.Initialize(serverCon)
	backbone.Callback = backbone
	serverCon = nil

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		backbone.QueueLoop()
		backbone.Shutdown()
		wg.Done()
	}()
	go func() {
		backbone.BackboneLoop()
		backbone.Shutdown()
		wg.Done()
	}()
	wg.Wait()

}
