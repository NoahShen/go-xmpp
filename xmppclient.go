package xmpp

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type EventType int

const (
	Connection = EventType(0)
	Stanza     = EventType(1)
)

type Event struct {
	Type    EventType
	Stanza  interface{}
	Error   error
	Message string
}

type ClientConfig struct {
	PingEnable      bool
	PingErrorTimes  int
	PingInterval    time.Duration
	ReconnectEnable bool
	ReconnectTimes  int
}

type XmppClient struct {
	client     *Client
	config     ClientConfig
	host       string
	jid        string
	password   string
	domain     string
	sendQueue  chan interface{}
	stopSendCh chan int
	stopPingCh chan int
	mutex      sync.Mutex
	handlers   []Handler
}

func NewXmppClient(conf ClientConfig) *XmppClient {
	xmppClient := new(XmppClient)
	xmppClient.config = conf
	xmppClient.sendQueue = make(chan interface{}, 10)
	xmppClient.stopSendCh = make(chan int)
	xmppClient.stopPingCh = make(chan int)
	return xmppClient
}

func (self *XmppClient) Connect(host, jid, password string) error {
	client, err := NewClient(host, jid, password)
	if err != nil {
		return err
	}
	self.client = client
	self.host = host
	self.jid = jid
	self.password = password
	self.domain, _ = GetDomain(jid)

	if reconnectTimes > 0 {
		reconnectTimes = 0
	}
	go self.startSendMessage()
	go self.startReadMessage()
	if self.config.PingEnable {
		go self.startPing()
	}
	return nil
}

func (self *XmppClient) Disconnect() error {
	self.stopSendCh <- 1
	if self.config.PingEnable {
		self.stopPingCh <- 1
	}
	return self.client.Close()
}

func (self *XmppClient) Send(msg interface{}) {
	self.sendQueue <- msg
}

func (self *XmppClient) SendChatMessage(jid, content string) {
	msg := &Message{}
	msg.To = jid
	msg.Type = "chat"
	msg.Body = content
	self.sendQueue <- msg
}

func (self *XmppClient) SendPresenceStatus(status string) {
	presence := &Presence{}
	presence.Status = status
	self.sendQueue <- presence
}

func (self *XmppClient) startSendMessage() {
	stopSend := false
	for !stopSend {
		select {
		case msg := <-self.sendQueue:
			err := self.client.Send(msg)
			if err != nil {
				self.fireHandler(&Event{Connection, nil, err, "send stanza error"})
				stopSend = true
				break
			}
		case <-self.stopSendCh:
			close(self.sendQueue)
			stopSend = true
			break
		}
	}
}

func (self *XmppClient) startReadMessage() {
	for {
		stanza, err := self.client.Recv()
		if err != nil {
			self.fireHandler(&Event{Connection, nil, err, "receive stanza error"})
			break
		}
		self.fireHandler(&Event{Stanza, stanza, nil, ""})
	}
}

func (self *XmppClient) startPing() {
	errCount := 0
	stopPing := false
	for !stopPing {
		select {
		case <-time.After(self.config.PingInterval):
			err := self.doPing()
			if err != nil {
				errCount++
				if errCount > self.config.PingErrorTimes {
					self.fireHandler(&Event{Connection, nil, err, "Ping timeout!"})
					stopPing = true
					break
				}
			} else {
				if errCount > 0 {
					errCount = 0
				}
			}
		case <-self.stopPingCh:
			stopPing = true
			break
		}
	}
}

func (self *XmppClient) doPing() error {
	iqId := RandomString(10)
	pingHandler := NewIqIDHandler(iqId)
	self.AddHandler(pingHandler)
	ping := &IQ{
		Id:   iqId,
		To:   self.domain,
		Type: "get",
		Ping: &Ping{},
	}
	self.Send(ping)

	// whatever result or unsupporting ping error
	event := pingHandler.GetEvent(5 * time.Second)
	if event == nil {
		return errors.New("Ping timeout!")
	}
	return nil
}

func (self *XmppClient) AddHandler(handler Handler) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.handlers = append(self.handlers, handler)
}

func (self *XmppClient) RemoveHandler(handler Handler) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for i, oldHandler := range self.handlers {
		if oldHandler == handler {
			self.handlers = append(self.handlers[0:i], self.handlers[i+1:]...)
			break
		}
	}
}

func (self *XmppClient) RemoveHandlerByIndex(i int) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.handlers = append(self.handlers[0:i], self.handlers[i+1:]...)
}

func (self *XmppClient) fireHandler(event *Event) {
	e := event
	if event.Type == Connection && event.Error != nil {
		if err := self.handleConnError(event); err != nil {
			e = &Event{Connection, nil, err, "All reconnecting error!"}
		}
	}
	copyHandlers := make([]Handler, len(self.handlers))
	copy(copyHandlers, self.handlers)
	for i := len(copyHandlers) - 1; i >= 0; i-- {
		h := copyHandlers[i]
		if h.Filter(e) {
			h.GetEventCh() <- e
			if h.IsOneTime() {
				self.RemoveHandlerByIndex(i)
			}
		}
	}
}

var reconnectTimes = 0

func (self *XmppClient) handleConnError(event *Event) error {
	if self.config.ReconnectEnable {
		reconnectTimes++
		if reconnectTimes > self.config.ReconnectTimes {
			msg := fmt.Sprintf("Connect failed after retring %d times", self.config.ReconnectTimes)
			return errors.New(msg)
		}
		self.Disconnect()
		if connErr := self.Connect(self.host, self.jid, self.password); connErr != nil {
			e := &Event{Connection, nil, connErr, "Reconnecting error!"}
			self.handleConnError(e)
		}
	}
	return nil
}
