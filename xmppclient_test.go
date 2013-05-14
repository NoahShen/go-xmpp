package xmpp

import (
	"fmt"
	"testing"
	"time"
)

var server = "talk.google.com:443"
var username = "username@gmail.com"
var password = "password"

func TestSendMessage(t *testing.T) {
	Debug = true
	xmppClient := NewXmppClient(ClientConfig{false, 3, 60 * time.Second, false, 1})
	err := xmppClient.Connect(server, username, password)
	if err != nil {
		t.Fatal(err)
	}

	chathandler := NewChatHandler()
	xmppClient.AddHandler(chathandler)

	subscribeHandler := NewSubscribeHandler()
	xmppClient.AddHandler(subscribeHandler)

	//make sure will receive roster and subscribe message
	roster := xmppClient.RequestRoster()
	fmt.Println("======= roster:", roster)

	for {
		select {
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
