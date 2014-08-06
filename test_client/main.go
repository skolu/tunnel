package main

import (
	"log"
	"net"
)

func main() {
	var e error
	var c net.Conn
	c, e = net.Dial("tcp", "127.1:8182")
	if e != nil {
		log.Panic(e)
	}
	log.Println("Connected")

	var b []byte = make([]byte, 1000)
	cnt := 0
	for {
		n, e := c.Read(b)
		if e != nil {
			break
		}
		log.Printf("Read %d byte packet\n", n)

		for i := 0; i < n; i++ {
			bt := byte((cnt + i) % 255)
			if bt != b[i] {
				log.Panicf("Incorrect byte %d, Expected %d at %d", b[i], bt, cnt+i)
			}
		}
		cnt += n
	}

	log.Printf("Done %d bytes", cnt)
}
