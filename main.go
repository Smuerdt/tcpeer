package main

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"encoding/gob"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"
)

var foreign string
var port string

func init() {
	flag.StringVar(&foreign, "c", "", "Peer connects initially to the given Host (Format: host:port")
	flag.StringVar(&port, "s", "4000", "Peer works initially as server and handles new incoming connections. Listens on given Port.")
}

func main() {
	log.SetFlags(0)
	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	peer := &tcpeer{uint64(rand.Int63()), make(chan string), false, list.New()}
	log.Println("TCPeer", peer.id)

	if foreign != "" {
		// foreign = "127.0.0.1:9988"
		alien, err := net.ResolveTCPAddr("tcp", foreign)
		if err != nil {
			log.Println("Error: Could not resolve foreign address")
		}

		log.Println("Connecting to: ", alien.String())

		conn, err := net.Dial("tcp", alien.String())
		if err != nil {
			log.Println("Error: Could not connect")
		} else {
			defer conn.Close()

			log.Println("Successfully connected")
			peer.sendID(conn)
			go peer.connectionHandler(conn)
			peer.inputWatcher()
		}
	} else {
		// port = 4000
		peer.isLeader = true
		service := ":" + port
		tcpAddr, err := net.ResolveTCPAddr("tcp", service)
		if err != nil {
			log.Println("Error: Could not resolve local address")
		}

		netListen, err := net.Listen(tcpAddr.Network(), tcpAddr.String())
		if err != nil {
			log.Println(err)
		} else {
			defer netListen.Close()

			go peer.inputWatcher()

			for {
				log.Println("Waiting for clients on Port:", port)
				conn, err := netListen.Accept()
				if err != nil {
					log.Println("TCPeer error:", err)
				} else {
					peer.sendID(conn)
					go peer.connectionHandler(conn)
				}
			}
		}
	}
}

type tcpeer struct {
	id             uint64
	send           chan string //currently not in use
	isLeader       bool
	ConnectionList *list.List
}

func (t *tcpeer) sendID(c net.Conn) {
	log.Println("Connection sending ID:", t.id)

	buffer := make([]byte, 2048)
	bytesWritten := binary.PutUvarint(buffer, t.id)
	c.Write(buffer[0:bytesWritten])
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
	log.Println("Waiting for foreign ID")
	buffer := make([]byte, 2048)
	bytesRead, err := conn.Read(buffer)
	if err != nil {
		log.Println("Connection connection error:", err)
	}

	id, i := binary.Uvarint(buffer[0:bytesRead])
	if i < 0 {
		log.Println("Error: Could not resolve ID")
		return
	}

	log.Println("Received foreign ID:", id)

	newConnection := &connection{id, make(chan string), conn, make(chan bool)}

	t.ConnectionList.PushBack(*newConnection)

	if t.isLeader {
		t.sendNewHost(*newConnection)
	}

	go newConnection.connectionReader()
	go newConnection.connectionSender()
}

func (t *tcpeer) sendNewHost(c connection) {
	b, _ := encodeConnection(c)
	log.Println(len(b))
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
		log.Println("ConnectionReader received >", string(buffer[0:bytesRead]), "from", c.id)
	}
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

func encodeConnection(c connection) ([]byte, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)

	err := enc.Encode(c)
	if err != nil {
		log.Println("Error: Could not encode Connection")
		return nil, err
	}

	b := buffer.Bytes()
	log.Println("Encoded Connection. Buffer size:", len(b))

	return b, nil
}
