# Telepathy-Go

A Golang port of the Telepathy library (https://github.com/vis2k/Telepathy)

Simple, message based, MMO Scale TCP networking in Go. And no magic.

## Usage samples

### Client
```go

```

### Server


```go
package main

import (
	"log"

	"github.com/maclof/telepathy-go"
)

func main() {
	server := telepathy.NewServer()
	err := server.Start(1337)
	if err != nil {
		log.Fatalf("Failed to start telepathy server: %s", err.Error())
	}

	for {
		msg := server.GetNextMessage()
		if msg == nil {
			continue
		}

		switch msg.EventType {
		case telepathy.MessageEventType_Connected:
			log.Printf("%d Connected", msg.ConnectionId)
			break
		case telepathy.MessageEventType_Data:
			log.Printf("%d Data", msg.ConnectionId)
			break
		case telepathy.MessageEventType_Disconnected:
			log.Printf("%d Disconnected", msg.ConnectionId)
			break
		}
	}

	server.Send(0, []byte{0,1,2,3})

	server.Stop()
}
```
