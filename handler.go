package xmpp

import (
	"time"
)

type Handler interface {
	GetHandleCh() chan *Event
	// if time < 0, no timeout limit
	GetEvent(time.Duration) *Event
	Filter(*Event) bool
	IsOneTime() bool
}

type DefaultHandler struct {
	handlCh chan *Event
}

func (self *DefaultHandler) GetHandleCh() chan *Event {
	return self.handlCh
}

func (self *DefaultHandler) GetEvent(d time.Duration) *Event {
	if d < 0 {
		event := <-self.handlCh
		return event
	} else {
		select {
		case event := <-self.handlCh:
			return event
		case <-time.After(d):
		}
	}
	return nil
}

// Chathandler
type ChatHandler struct {
	DefaultHandler
}

func NewChatHandler() Handler {
	c := &ChatHandler{}
	c.handlCh = make(chan *Event)
	return c
}

func (self *ChatHandler) Filter(event *Event) bool {
	if event.Type == Stanza {
		stanza := event.Stanza
		if stanza != nil {
			switch stanza := stanza.(type) {
			case *Message:
				return stanza.Type == "chat" && len(stanza.Body) > 0
			}
		}
	}
	return false
}
func (self *ChatHandler) IsOneTime() bool {
	return false
}

//Ping handler
type IqIDHandler struct {
	iqId string
	DefaultHandler
}

func NewIqIDHandler(iqId string) Handler {
	iqH := &IqIDHandler{}
	iqH.handlCh = make(chan *Event)
	iqH.iqId = iqId
	return iqH
}

func (self *IqIDHandler) Filter(event *Event) bool {
	if event.Type == Stanza {
		stanza := event.Stanza
		if stanza != nil {
			switch stanza := stanza.(type) {
			case *IQ:
				return stanza.Id == self.iqId
			}
		}
	}
	return false
}

func (self *IqIDHandler) IsOneTime() bool {
	return true
}
