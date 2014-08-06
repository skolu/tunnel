package shared

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	AttachCommand   = "attach"
	CreateCommand   = "create"
	DetachCommand   = "detach"
	ShutdownCommand = "shutdown"

	OkResponse    = "OK"
	ErrorResponse = "ERROR"

	MaxPacketLength    = int(1000)
	PacketMagic        = uint16(0xFFFF)
	PacketHeaderLength = 8
)

const (
	PacketFlagIsLast = 1 << iota
)

type (
	BackboneCallback interface {
		OnCommandReceived(command string, sessionId uint16) bool
	}

	muxSendRq struct {
		connId uint16
		isLast bool
		data   []byte
	}

	RibConn struct {
		Conn       net.Conn
		SessionId  uint16
		packetData []byte
	}

	BackboneConn struct {
		Callback    BackboneCallback
		conn        net.Conn
		connections map[uint16]*RibConn
		queue       chan *muxSendRq
	}
)

func (bc *BackboneConn) Initialize(c net.Conn) {
	bc.conn = c
	bc.connections = make(map[uint16]*RibConn, 32)
	bc.queue = make(chan *muxSendRq, 32)
}

func (bc *BackboneConn) Shutdown() {
	if bc.conn == nil {
		return
	}
	cc := bc.conn
	bc.conn = nil
	cc.Close()
	for _, rc := range bc.connections {
		rc.Conn.Close()
	}
	bc.connections = nil
	close(bc.queue)
	bc.queue = nil
}

func (bc *BackboneConn) Enqueue(rq *muxSendRq) {
	if bc.queue != nil {
		bc.queue <- rq
	}
}

func (bc *BackboneConn) sendInternalCommand(cmd string, sessionId uint16) {
	rq := new(muxSendRq)
	rq.connId = 0
	rq.isLast = true
	rq.data = []byte(fmt.Sprintf("%s:%d", cmd, sessionId))
	bc.Enqueue(rq)
}

func (bc *BackboneConn) Attach(rc *RibConn, doNotify bool) {
	if bc.conn == nil {
		return
	}

	if rc.SessionId == 0 {
		return
	}
	bc.connections[rc.SessionId] = rc

	if doNotify {
		bc.sendInternalCommand(AttachCommand, rc.SessionId)
	}

	go func() {
		var buf []byte = make([]byte, MaxPacketLength)
		var isLast bool = true
		var deadLine time.Time
		for {
			if isLast {
				deadLine = time.Unix(0, 0)
			} else {
				deadLine = time.Now().Add(time.Millisecond)
			}
			rc.Conn.SetReadDeadline(deadLine)
			n, e := rc.Conn.Read(buf)
			if e != nil {
				err, ok := e.(net.Error)
				if ok {
					if err.Timeout() {
						n = 0
						e = nil
					}
				}
				if e != nil {
					bc.Detach(rc.SessionId, true)
					break
				}
			}

			isLast = n < cap(buf)

			rq := new(muxSendRq)
			rq.connId = rc.SessionId
			rq.isLast = isLast
			rq.data = make([]byte, n)
			copy(rq.data, buf[:n])
			bc.Enqueue(rq)
		}
	}()
}

func (bc *BackboneConn) Detach(connId uint16, doNotify bool) {
	rc, ok := bc.connections[connId]
	if !ok {
		return
	}
	delete(bc.connections, connId)

	if doNotify {
		bc.sendInternalCommand(DetachCommand, rc.SessionId)
	}

	rc.Conn.Close()
}

func (bc *BackboneConn) QueueLoop() {
	defer log.Println("QueueLoop exited")

	var e error
	var buff []byte = make([]byte, 2048)
	var writer *bytes.Buffer = bytes.NewBuffer(buff)

	for rq := range bc.queue {
		for len(rq.data) > 0 {
			var pac []byte = nil
			if len(rq.data) <= MaxPacketLength {
				pac = rq.data
				rq.data = nil
			} else {
				pac = rq.data[:MaxPacketLength]
				rq.data = rq.data[MaxPacketLength:]
			}

			var flags uint16 = 0
			if rq.isLast {
				flags |= PacketFlagIsLast
			}
			writer.Reset()
			binary.Write(writer, binary.BigEndian, uint16(PacketMagic))
			binary.Write(writer, binary.BigEndian, uint16(rq.connId))
			binary.Write(writer, binary.BigEndian, uint16(len(pac)))
			binary.Write(writer, binary.BigEndian, uint16(flags))
			writer.Write(pac)

			if bc.conn == nil {
				return
			}
			bc.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			b := writer.Bytes()
			log.Printf("Writing %d to backbone", len(b))
			_, e = bc.conn.Write(b)
			if e != nil {
				neterr, ok := e.(net.Error)
				if ok {
					if neterr.Timeout() {
						log.Println(e)
						break
					}
				}
				return
			}
		}
	}
}

func (bc *BackboneConn) BackboneLoop() {
	defer log.Println("BackboneLoop exited")

	var e error
	var n int
	var chunk []byte = make([]byte, 2*(MaxPacketLength+PacketHeaderLength))

	var buffer []byte = make([]byte, 2*cap(chunk))
	var bufferHead int = 0
	var bufferTail int = 0

	var reader *bytes.Reader = bytes.NewReader(buffer)

	for {
		n, e = bc.conn.Read(chunk)
		if e != nil { // shutdown
			bc.sendInternalCommand(ShutdownCommand, 0)
			return
		}

		log.Printf("Read %d bytes from backbone\n", n)

		if bufferHead == bufferTail {
			copy(buffer, chunk[:n])
			bufferHead = 0
			bufferTail = n
		} else if bufferHead < bufferTail {
			if (bufferTail-bufferHead)+n < cap(buffer) {
				copy(buffer, append(buffer[bufferHead:bufferTail], chunk[:n]...))
				bufferTail -= bufferHead
				bufferHead = 0
				bufferTail += n
			} else {
				log.Panicln("Read buffer overflow")
			}
		} else {
			log.Panicln("Invalid buffer pointers")
			break
		}

		for (bufferTail - bufferHead) > int(PacketHeaderLength) {
			reader.Seek(int64(bufferHead), 0)
			var v [4]uint16
			for i := 0; i < 4; i++ {
				binary.Read(reader, binary.BigEndian, &(v[i]))
			}
			if v[0] != PacketMagic {
				log.Panicln("Invalid packet magic")
				break
			}

			var packetLength int = int(v[2] + PacketHeaderLength)
			if packetLength > (bufferTail - bufferHead) {
				break
			}

			connId := v[1]
			flags := v[3]
			b := buffer[bufferHead+PacketHeaderLength : bufferHead+packetLength]
			bufferHead += int(packetLength)

			if connId > 0 {
				rc, ok := bc.connections[connId]
				if ok {
					if rc.packetData == nil {
						rc.packetData = make([]byte, 0, MaxPacketLength)
					}
					rc.packetData = append(rc.packetData, b...)
					if (flags & PacketFlagIsLast) != 0 {
						log.Printf("Packet of %d bytes sent to desination", len(rc.packetData))
						rc.Conn.SetWriteDeadline(time.Now().Add(time.Second * 2))
						_, e = rc.Conn.Write(rc.packetData)
						rc.packetData = rc.packetData[0:0]
						if e != nil {
							bc.Detach(connId, true)
						}
					}
				}
			} else {
				pck := string(b)
				ss := strings.Split(pck, ":")
				cmd := strings.TrimSpace(ss[0])
				cmd = strings.ToLower(cmd)
				var sessionId uint16 = 0
				if len(ss) >= 2 {
					var cId uint64
					cId, e = strconv.ParseUint(strings.TrimSpace(ss[1]), 10, 16)
					if e == nil {
						sessionId = uint16(cId)
					}
				}
				processed := false
				if bc.Callback != nil {
					processed = bc.Callback.OnCommandReceived(cmd, sessionId)
				}
				if !processed {
					if cmd == DetachCommand {
						bc.Detach(sessionId, false)
					}
				}
			}
		}
	}
}
