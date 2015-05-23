package main

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
)

func main() {
	log.SetFlags(0)
	log.Println("TCPeer")

	peer := &tcpeer{uint64(rand.Int63()), make(chan string), list.New()}

	service := ":9988"
	tcpAddr, err := net.ResolveTCPAddr("tcp", service)
	if err != nil {
		log.Println("Error: Could not resolve address")
	} else {
		var str string
		fmt.Scanf("%s", &str)

		if bytes.Equal([]byte(str), []byte("c")) {

		} else {
			netListen, err := net.Listen(tcpAddr.Network(), tcpAddr.String())
			if err != nil {
				log.Println(err)
			} else {
				defer netListen.Close()

				for {
					log.Println("Waiting for clients")
					connection, err := netListen.Accept()
					if err != nil {
						log.Println("TCPeer error:", err)
					} else {
						go peer.connectionHandler(connection)
					}
				}
			}
		}
	}
}

type tcpeer struct {
	id             uint64
	send           chan string //currently not in use
	ConnectionList *list.List
}

func (t *tcpeer) remove(c connection) {
	for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
		conn := entry.Value.(connection)
		if conn.equal(&c) {
			log.Println("Remove:", c.id)
			t.ConnectionList.Remove(entry)
		}
	}
}

func (t *tcpeer) inputWatcher() {
	for {
		var send string
		fmt.Scanf("%s\n", &send)

		for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
			connection := entry.Value.(connection)
			connection.Outgoing <- send
		}
	}
}

func (t *tcpeer) connectionHandler(conn net.Conn) {
	buffer := make([]byte, 1024)
	bytesRead, err := conn.Read(buffer)
	if err != nil {
		log.Println("Connection connection error:", err)
	}

	id, i := binary.Uvarint(buffer[0:bytesRead])
	if i < 0 {
		log.Println("Error: Could not resolve ID")
		return
	}

	newConnection := &connection{id, make(chan string), conn, make(chan bool)}

	go newConnection.connectionReader()
	go newConnection.connectionSender()

	t.ConnectionList.PushBack(*newConnection)
}

type connection struct {
	id       uint64
	Outgoing chan string
	Conn     net.Conn
	Quit     chan bool
}

func (c *connection) connectionSender() {
	for {
		select {
		case buffer := <-c.Outgoing:
			log.Println("ConnectionSender sendig", string(buffer))
			count := 0
			for i := 0; i < len(buffer); i++ {
				if buffer[i] == 0x00 {
					break
				}
				count++
			}
			log.Println("Send size: ", count)
			c.Conn.Write([]byte(buffer)[0:count])
		case <-c.Quit:
			log.Println("Connection", c.id, "quitting")
			c.Conn.Close()
			break
		}
	}
}

func (c *connection) connectionReader() {
	for {
		buffer := make([]byte, 2048)
		bytesRead, err := c.Conn.Read(buffer)
		if err != nil {
			c.Conn.Close()
			log.Println(err)
			break
		}

		log.Println("Read", bytesRead, "bytes")
		log.Println("ConnectionReader received >", string(buffer[0:bytesRead]))
	}
}

func (c *connection) sendID() {
	log.Println("Connection sending ID:")

	buffer := make([]byte, 1024)
	bytesWritten := binary.PutUvarint(buffer, c.id)
	c.Conn.Write(buffer[0:bytesWritten])
}

func (c *connection) read(buffer []byte) (int, bool) {
	bytesRead, err := c.Conn.Read(buffer)
	if err != nil {
		c.Conn.Close()
		log.Println(err)
		return 0, false
	}
	log.Println("Read", bytesRead, "bytes")
	return bytesRead, true
}

func (c *connection) equal(other *connection) bool {
	if c.id == other.id {
		if c.Conn == other.Conn {
			return true
		}
	}
	return false
}
