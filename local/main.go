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
	LocalBackbone struct {
		shared.BackboneConn
		lastConnId uint16
	}
)

var (
	backbone *LocalBackbone
)

func (bb *LocalBackbone) OnCommandReceived(command string, sessionId uint16) bool {
	log.Printf("Command received: %s:%d", command, sessionId)
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
	serverCon.Write([]byte(fmt.Sprintf("%s:%s", shared.CreateCommand, "111")))
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

	backbone = new(LocalBackbone)
	backbone.Initialize(serverCon)
	backbone.Callback = backbone
	serverCon = nil

	var listner net.Listener

	// start backbone loops
	go func() {
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

		listner.Close()
	}()

	log.Println("listening")

	//wait for local connections
	listner, e = net.Listen("tcp", "127.1:8182")
	if e != nil {
		log.Panicln(e)
	}

	for {
		var c net.Conn

		c, e = listner.Accept()
		if e != nil {
			log.Println(e)
			break
		}

		rc := new(shared.RibConn)
		backbone.lastConnId++
		rc.SessionId = backbone.lastConnId

		rc.Conn = c

		backbone.Attach(rc, true)
	}
}
