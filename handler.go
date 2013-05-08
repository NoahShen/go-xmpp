package xmpp

import ()

type Handler interface {
	GetHandleCh() chan *Event
	Filter(*Event) bool
	IsOnce() bool
}

type ChatHandler struct {
	handlCh chan *Event
}

func NewChatHandler() Handler {
	return &ChatHandler{make(chan *Event)}
}

func (self *ChatHandler) GetHandleCh() chan *Event {
	return self.handlCh
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

func (self *ChatHandler) IsOnce() bool {
	return false
}
