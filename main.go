package main

import (
	"container/list"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"time"
)

var foreign string
var port string

func init() {
	flag.StringVar(&foreign, "c", "", "Peer connects to the given Host (Format: host:port")
	flag.StringVar(&port, "p", "4000", "Peer works initially as server and handles new incoming connections. Listens on given Port.")
}

func main() {
	log.SetFlags(0)
	rand.Seed(time.Now().UTC().UnixNano())

	flag.Parse()

	ip, err := externalIP()
	if err != nil {
		log.Println("Error: Could not resolve external IP")
	}

	peer := &tcpeer{uint64(rand.Int63()), make(chan string), list.New(), ip, port}
	log.Println("TCPeer", peer.id)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		peer.sendToAll("/quit")
		os.Exit(0)
	}()

	if foreign != "" {
		// foreign = "127.0.0.1:9988"
		peer.connect(foreign)
	}

	// port = 4000
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

type tcpeer struct {
	id             uint64
	send           chan string //currently not in use
	ConnectionList *list.List
	ip             string
	port           string
}

func (t *tcpeer) sendID(c net.Conn) {
	log.Println("Connection sending ID and IP:Port:", t.id, t.ip+":"+t.port)

	buffer := make([]byte, 512)
	buffer[0] = 0x01
	bytesWritten := binary.PutUvarint(buffer[1:], t.id)

	buffer[bytesWritten+1] = 0x04
	ipbuf := []byte(t.ip + ":" + t.port)

	copy(buffer[bytesWritten+2:], ipbuf)

	c.Write(buffer[0 : bytesWritten+len(ipbuf)+2])
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

func (t *tcpeer) alreadyKnown(c connection) bool {
	for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
		conn := entry.Value.(connection)
		if conn.equal(&c) {
			return true
		}
	}
	return false
}

func (t *tcpeer) hostKnown(host string) bool {
	for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
		conn := entry.Value.(connection)
		if conn.address == host {
			return true
		}
	}
	return false
}

func (t *tcpeer) sendToAll(str string) {
	for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
		conn := entry.Value.(connection)
		conn.sendData(str)
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
	buffer := make([]byte, 512)
	bytesRead, err := conn.Read(buffer)
	if err != nil {
		log.Println("Connection connection error:", err)
	}

	if buffer[0] == 0x01 {
		var rawid []byte
		var host string
		for i, v := range buffer {
			if v == 0x04 {
				rawid = make([]byte, i-1)
				copy(rawid, buffer[1:i])
				host = string(buffer[i+1 : bytesRead])
				break
			}
		}

		id, i := binary.Uvarint(rawid)
		if i < 0 {
			log.Println("Error: Could not resolve ID")
			return
		}

		log.Println("Received foreign ID:", id, "from", host)

		newConnection := &connection{id, make(chan string), conn, make(chan bool), t, host}

		if !t.alreadyKnown(*newConnection) {
			t.sendNewPeer(host)

			go newConnection.connectionReader()
			go newConnection.connectionSender()

			t.ConnectionList.PushBack(*newConnection)
		} else {
			log.Println("Connection already known. Terminating needless connection")
			newConnection.close()
		}
	}
}

func (t *tcpeer) sendNewPeer(host string) {
	buffer := make([]byte, getStringLength(host)+1)
	buffer[0] = 0x03
	copy(buffer[1:], []byte(host))

	for entry := t.ConnectionList.Front(); entry != nil; entry = entry.Next() {
		conn := entry.Value.(connection)
		conn.Conn.Write(buffer)
	}
}

func (t *tcpeer) addNewPeer(foreign string) {
	if !t.hostKnown(foreign) {
		t.connect(foreign)
	}
}

func (t *tcpeer) connect(foreign string) {
	alien, err := net.ResolveTCPAddr("tcp", foreign)
	if err != nil {
		log.Println("Error: Could not resolve foreign address")
	}

	log.Println("Connecting to: ", alien.String())

	conn, err := net.Dial("tcp", alien.String())
	if err != nil {
		log.Println("Error: Could not connect")
	} else {
		log.Println("Successfully connected")
		t.sendID(conn)
		go t.connectionHandler(conn)
	}
}

type connection struct {
	id       uint64
	Outgoing chan string
	Conn     net.Conn
	Quit     chan bool
	owner    *tcpeer
	address  string
}

func (c *connection) connectionSender() {
	for {
		select {
		case buffer := <-c.Outgoing:
			log.Println("ConnectionSender sendig", string(buffer))
			c.sendData(buffer)
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

		if buffer[0] == 0x02 {
			str := string(buffer[1:bytesRead])
			log.Println("ConnectionReader received >", str, "from", c.id)

			if str == "/quit" {
				c.close()
				break
			}
		} else if buffer[0] == 0x03 {
			log.Println("Received new Host address:", string(buffer[1:bytesRead]), "from", c.id)
			c.owner.addNewPeer(string(buffer[1:bytesRead]))
		}

	}
}

func (c *connection) sendData(str string) {
	count := getStringLength(str)
	log.Println("Send size: ", count)

	b := make([]byte, count+1)
	copy(b[1:], []byte(str)[0:count])
	b[0] = 0x02

	c.Conn.Write(b)
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
		if c.address == other.address {
			return true
		}
	}
	return false
}

func (c *connection) close() {
	log.Println("Closing connection", c.id)
	c.Quit <- true
	c.Conn.Close()
	c.owner.remove(*c)
}

func getStringLength(buffer string) int {
	count := 0
	for i := 0; i < len(buffer); i++ {
		if buffer[i] == 0x00 {
			break
		}
		count++
	}
	return count
}

// currently returns intranet ip
func externalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

// func getOwnIp() {
// 	ifaces, err := net.Interfaces()
// 	if err != nil {
// 		log.Println("Error: Could not get Network Interfaces")
// 	}

// 	for _, i := range ifaces {
// 		addrs, err := i.Addrs()
// 		if err != nil {
// 			log.Println("Error: Could not get Network Adresses of Interfaces")
// 		}

// 		for _, addr := range addrs {
// 			switch v := addr.(type) {
// 			case *net.IPAddr:
// 				log.Println(v.String())
// 			}

// 		}
// 	}
// }
