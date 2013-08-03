package xmpp

import (
	"fmt"
	"testing"
	"time"
)

//var username = "NoahPi87@gmail.com"
//var password = "15935787"
var username = "NoahShen@jabber.org"
var password = "159357"

func TestSendMessage(t *testing.T) {
	Debug = true
	xmppClient := NewXmppClient(ClientConfig{true, 1, 10 * time.Second, true, 5})
	err := xmppClient.Connect("", username, password)
	if err != nil {
		t.Fatal(err)
	}

	connErrorHandler := NewConnErrorHandler()
	xmppClient.AddHandler(connErrorHandler)

	chathandler := NewChatHandler()
	xmppClient.AddHandler(chathandler)

	subscribeHandler := NewSubscribeHandler()
	xmppClient.AddHandler(subscribeHandler)

	//make sure will receive roster and subscribe message
	xmppClient.RequestRoster()
	xmppClient.Send(&Presence{})
	for {
		select {
		case event := <-connErrorHandler.GetEventCh():
			fmt.Printf("Event catch: connection error: %v, msg: %s\n", event.Error, event.Message)
			t.FailNow()
		case event := <-chathandler.GetEventCh():
			msg := event.Stanza.(*Message)
			xmppClient.SendChatMessage(msg.From, "echo "+msg.Body)
			xmppClient.SendPresenceStatus("echo " + msg.Body)
		case subEvent := <-subscribeHandler.GetEventCh():
			subPresence := subEvent.Stanza.(*Presence)
			fmt.Println("****" + subPresence.From + " request to add me as a contact")
			subscribed := &Presence{
				To:   subPresence.From,
				Type: "subscribed",
			}
			xmppClient.Send(subscribed)
		}
	}

}
