package xmpp

import (
	//"fmt"
	"testing"
)

var server = "talk.google.com:443"
var username = "username@gmail.com"
var password = "password"

func TestSendMessage(t *testing.T) {
	Debug = true
	xmppClient := NewXmppClient()
	err := xmppClient.Connect(server, username, password)
	if err != nil {
		t.Fatal(err)
	}

	chathandler := NewChatHandler()
	xmppClient.AddHandler(chathandler)
	for event := range chathandler.GetHandleCh() {
		msg := event.Stanza.(*Message)
		xmppClient.SendChatMessage(msg.From, "echo "+msg.Body)
		xmppClient.SendPresenceStatus("echo " + msg.Body)
	}

}
