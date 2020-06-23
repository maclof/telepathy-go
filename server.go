package telepathy

import (
	"io"
	"log"
	"fmt"
	"net"
	"time"
	"sync"
	"errors"
	"encoding/binary"

	"github.com/emirpasic/gods/maps/hashmap"
	"github.com/enriquebris/goconcurrentqueue"
)

type Server struct {
	listener              net.Listener
	isRunning             bool
	connIdCounter         int
	connIdCounterMutex    sync.Mutex
	connectedClients      *hashmap.Map
	connectedClientsMutex sync.Mutex
	receiveQueue          *goconcurrentqueue.FIFO
	sendQueue             *goconcurrentqueue.FIFO
}

func (server *Server) Start(port int) error {
	if server.isRunning {
		return errors.New("The server is already running!")
	}

	addr := fmt.Sprintf("0.0.0.0:%d", port)

	log.Printf("Starting telepathy server on: %s", addr)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Error starting server: %s", err.Error())
	}
	server.listener = listener
	server.isRunning = true

	go server.handleConnections()
	go server.processSendQueue()

	for {
		if !server.isRunning {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}

	return nil
}

func (server *Server) nextConnectionId() int {
	server.connIdCounterMutex.Lock()
	defer server.connIdCounterMutex.Unlock()
	server.connIdCounter++
	return server.connIdCounter
}

func (server *Server) handleConnections() {
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			log.Printf("Failed to accept new connection: %s", err.Error())
			continue
		}

		tcpConn := conn.(*net.TCPConn)

		err = tcpConn.SetNoDelay(true)
		if err != nil {
			log.Printf("Failed to set 'no delay' option for new connection: %s", err.Error())
		}

		tcpConn.SetReadBuffer(MAX_MESSAGE_SIZE)
		tcpConn.SetWriteBuffer(MAX_MESSAGE_SIZE)

		client := &serverClient{
			conn: tcpConn,
			connId: server.nextConnectionId(),
			connected: true,
			sendQueue: goconcurrentqueue.NewFIFO(),
		}

		log.Printf("Accepted a new client: %d - %s", client.connId, client.conn.RemoteAddr().String())

		server.connectedClientsMutex.Lock()
		server.connectedClients.Put(client.connId, client)
		server.connectedClientsMutex.Unlock()

		go server.handleClientConnection(client)
	}
}

func (server *Server) handleClientConnection(client *serverClient) {
	go client.processSendQueue()

	server.receiveQueue.Enqueue(&Message{
		EventType: MessageEventType_Connected,
		ConnectionId: client.connId,
	})

	var recvHeaderBuf []byte
	var recvDataBuf []byte
	for {
		recvHeaderBuf = make([]byte, 4)

		client.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 10))
		numBytes, err := client.conn.Read(recvHeaderBuf)
		if err != nil {
			// if err, ok := err.(net.Error); ok && err.Timeout() || err == io.EOF {
			// 	log.Printf("Client disconnected from server: %d", client.connId)
			// 	break
			// }

			// log.Printf("Failed to read from client %d - %s", client.connId, err.Error())

			if err == io.EOF {
				server.handleClientDisconnection(client)
				break
			}
			continue
		}
		if numBytes == 0 {
			continue
		}

		size := int(binary.BigEndian.Uint32(recvHeaderBuf))
		log.Printf("Size Header: %v", size)

		if size > MAX_MESSAGE_SIZE {
			log.Printf("Possible allocation attack with a size header of: %d bytes.", size)
			continue
		}

		recvDataBuf = make([]byte, size)

		client.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 10))
		numBytes, err = client.conn.Read(recvDataBuf)
		if err != nil {
			// if err, ok := err.(net.Error); ok && err.Timeout() || err == io.EOF {
			// 	log.Printf("Client disconnected from server: %d", client.connId)
			// 	break
			// }

			// log.Printf("Failed to read from client %d - %s", client.connId, err.Error())

			if err == io.EOF {
				server.handleClientDisconnection(client)
				break
			}
			continue
		}
		if numBytes == 0 {
			continue
		}

		server.receiveQueue.Enqueue(&Message{
			EventType: MessageEventType_Data,
			ConnectionId: client.connId,
			Data: recvDataBuf,
		})
	}
}

func (server *Server) handleClientDisconnection(client *serverClient) {
	log.Printf("Client disconnected from server: %d", client.connId)
	client.conn.Close()
	client.connected = false
	server.connectedClientsMutex.Lock()
	defer server.connectedClientsMutex.Unlock()
	server.connectedClients.Remove(client.connId)
	server.receiveQueue.Enqueue(&Message{
		EventType: MessageEventType_Disconnected,
		ConnectionId: client.connId,
	})
}

func (server *Server) processSendQueue() {
	for {
		msgInterface, err := server.sendQueue.Dequeue()
		if err != nil {
			continue
		}

		msg := msgInterface.(*serverSendMessage)

		server.connectedClientsMutex.Lock()
		clientInterface, clientExists := server.connectedClients.Get(msg.connId)
		server.connectedClientsMutex.Unlock()

		if !clientExists {
			continue
		}

		client := clientInterface.(*serverClient)

		client.sendQueueMutex.Lock()
		client.sendQueue.Enqueue(msg)
		client.sendQueueMutex.Unlock()
	}
}

func (server *Server) Stop() {
	if server.listener == nil {
		return
	}
	log.Println("Stopping telepathy server")
	server.listener.Close()
	server.isRunning = false
}

func (server *Server) GetNextMessage() *Message {
	msgInterface, err := server.receiveQueue.Dequeue()
	if err != nil {
		return nil
	}

	return msgInterface.(*Message)
}

func (server *Server) Send(connId int, data []byte) {
	log.Printf("Queueing message to send to client: %d", connId)

	for i := 0; i < 1; i++ {
		server.sendQueue.Enqueue(&serverSendMessage{
			connId: connId,
			data: data,
		})
	}
}

type serverClient struct {
	conn           *net.TCPConn
	connId         int
	connected      bool
	sendQueue      *goconcurrentqueue.FIFO
	sendQueueMutex sync.Mutex
}

func (client *serverClient) processSendQueue() {
	for {
		if !client.connected {
			log.Printf("Closing send queue for client: %d", client.connId)
			break
		}

		client.sendQueueMutex.Lock()

		queueLen := client.sendQueue.GetLen()
		if queueLen == 0 {
			client.sendQueueMutex.Unlock()
			continue
		}

		var sendBuf []byte
		for i := 0; i < queueLen; i++ {
			messageInterface, err := client.sendQueue.Dequeue()
			if err != nil {
				continue
			}
			message := messageInterface.(*serverSendMessage)
			header := make([]byte, 4)
			binary.BigEndian.PutUint32(header, uint32(len(message.data)))
			sendBuf = append(sendBuf, header...)
			sendBuf = append(sendBuf, message.data...)
		}

		// debugMsg := fmt.Sprintf("sendBuf - ")
		// for i := 0; i < len(sendBuf); i++ {
		// 	debugMsg += fmt.Sprintf("%d ", sendBuf[i])
		// }
		// log.Println(debugMsg)

		client.conn.Write(sendBuf)

		client.sendQueueMutex.Unlock()
	}
}

type serverSendMessage struct {
	connId int
	data   []byte
}

func NewServer() *Server {
	return &Server{
		isRunning: false,
		connIdCounter: 0,
		connectedClients: hashmap.New(),
		receiveQueue: goconcurrentqueue.NewFIFO(),
		sendQueue: goconcurrentqueue.NewFIFO(),
	}
}
