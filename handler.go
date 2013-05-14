package xmpp

import (
	"time"
)

type Handler interface {
	GetEventCh() chan *Event
	// if time < 0, no timeout limit
	GetEvent(time.Duration) *Event
	Filter(*Event) bool
	IsOneTime() bool
}

type DefaultHandler struct {
	EventCh chan *Event
}

func (self *DefaultHandler) GetEventCh() chan *Event {
	return self.EventCh
}

func (self *DefaultHandler) GetEvent(d time.Duration) *Event {
	if d < 0 {
		event := <-self.EventCh
		return event
	} else {
		select {
		case event := <-self.EventCh:
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
	c.EventCh = make(chan *Event)
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

//Someone subscribe me
type SubscribeHandler struct {
	DefaultHandler
}

func NewSubscribeHandler() Handler {
	h := &SubscribeHandler{}
	h.EventCh = make(chan *Event)
	return h
}

func (self *SubscribeHandler) Filter(event *Event) bool {
	if event.Type == Stanza {
		stanza := event.Stanza
		if stanza != nil {
			switch stanza := stanza.(type) {
			case *Presence:
				return stanza.Type == "subscribe"
			}
		}
	}
	return false
}

func (self *SubscribeHandler) IsOneTime() bool {
	return false
}

//Ping handler
type IqIDHandler struct {
	iqId string
	DefaultHandler
}

func NewIqIDHandler(iqId string) Handler {
	iqH := &IqIDHandler{}
	iqH.EventCh = make(chan *Event)
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

//Connection Error handler
type ConnErrorHandler struct {
	DefaultHandler
}

func NewConnErrorHandler() Handler {
	ch := &ConnErrorHandler{}
	ch.EventCh = make(chan *Event)
	return ch
}

func (self *ConnErrorHandler) Filter(event *Event) bool {
	if event.Type == Connection {
		return event.Error != nil
	}
	return false
}

func (self *ConnErrorHandler) IsOneTime() bool {
	return false
}
