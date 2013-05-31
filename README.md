go-xmpp
========
- Forked from mattn/go-xmpp
- It's a wrapper of mattn/go-xmpp
- Support add user-define handler and reconnect after ping failed

## Installation ##

```go
go get github.com/NoahShen/go-xmpp

// use in your .go code:
import (
    "github.com/NoahShen/go-xmpp"
)
```

## Usage ##

```go
xmpp.Debug = true // print stanza xml
xmppClient := xmpp.NewXmppClient(xmpp.ClientConfig{true, 3, 30 * time.Second, true, 5})
err := xmppClient.Connect(server, username, password)
...
// use default handler, also you can define your own handler which must implements xmpp.Handler
connErrorHandler := xmpp.NewConnErrorHandler()
xmppClient.AddHandler(connErrorHandler)

chathandler := xmpp.NewChatHandler()
xmppClient.AddHandler(chathandler)

subscribeHandler := xmpp.NewSubscribeHandler()
xmppClient.AddHandler(subscribeHandler)


roster := xmppClient.RequestRoster()
...
xmppClient.Send(&Presence{})

for {
	select {
	case event := <-connErrorHandler.GetEventCh():
		fmt.Printf("Event catch: connection error: %v, msg: %s\n", event.Error, event.Message)
	case event := <-chathandler.GetEventCh():
		msg := event.Stanza.(*xmpp.Message)
		xmppClient.SendChatMessage(msg.From, "echo "+msg.Body)
		xmppClient.SendPresenceStatus("echo " + msg.Body)
	case subEvent := <-subscribeHandler.GetEventCh():
		subPresence := subEvent.Stanza.(*xmpp.Presence)
		fmt.Println("****" + subPresence.From + " request to add me as a contact")
		subscribed := &Presence{
			To:   subPresence.From,
			Type: "subscribed",
		}
		xmppClient.Send(subscribed)
	}
}

```