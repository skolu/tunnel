package main

import (
	"log"
	"net"
)

const (
	PacketLength = 500
)

func main() {
	log.Println("Started...")

	var listner net.Listener
	var e error

	listner, e = net.Listen("tcp", "127.1:8183")
	if e != nil {
		log.Panicln(e)
	}

	var b []byte = make([]byte, 10240)
	for i := 0; i < len(b); i++ {
		b[i] = byte(i % 255)
	}

	for {
		var c net.Conn

		c, e = listner.Accept()
		if e != nil {
			log.Println(e)
			break
		}

		go func() {
			log.Println("Dump values")
			cnt := 0
			for cnt < len(b) {
				var ff []byte
				if (cnt + PacketLength) < len(b) {
					ff = b[cnt : cnt+PacketLength]
				} else {
					ff = b[cnt:]
				}
				cnt += len(ff)
				_, ee := c.Write(ff)
				if ee != nil {
					log.Panic(ee)
				}
			}
			c.Close()
			log.Printf("Done %d bytes", cnt)
		}()
	}
}
