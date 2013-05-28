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
	connected  bool
	sendQueue  chan interface{}
	stopSendCh chan int
	stopPingCh chan int
	mutex      sync.Mutex
	handlers   []Handler
}

func NewXmppClient(conf ClientConfig) *XmppClient {
	xmppClient := new(XmppClient)
	xmppClient.config = conf

	return xmppClient
}

func (self *XmppClient) Connect(host, jid, password string) error {
	if self.connected {
		return errors.New("It's already connected!")
	}
	self.sendQueue = make(chan interface{}, 10)
	self.stopSendCh = make(chan int, 1)
	self.stopPingCh = make(chan int, 1)

	client, err := NewClient(host, jid, password)
	if err != nil {
		return err
	}
	self.client = client
	self.host = host
	self.jid = jid
	self.password = password
	self.domain, _ = GetDomain(jid)

	go self.startSendMessage()
	go self.startReadMessage()
	if self.config.PingEnable {
		go self.startPing()
	}

	self.connected = true
	if reconnectTimes > 0 {
		reconnectTimes = 0
	}
	return nil
}

func (self *XmppClient) Disconnect() error {
	self.connected = false
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

func (self *XmppClient) RequestRoster() *IQRoster {
	iqId := RandomString(10)
	rosterHandler := NewIqIDHandler(iqId)
	self.AddHandler(rosterHandler)
	iq := &IQ{
		Type:   "get",
		Id:     iqId,
		Roster: &IQRoster{},
	}

	self.Send(iq)
	event := rosterHandler.GetEvent(10 * time.Second)
	if event != nil {
		iqResp := event.Stanza.(*IQ)
		if iqResp.Type == "result" {
			return iqResp.Roster
		}
	}
	return nil
}

func (self *XmppClient) startSendMessage() {
	for self.connected {
		select {
		case msg := <-self.sendQueue:
			err := self.client.Send(msg)
			if err != nil {
				if self.connected {
					self.fireHandler(&Event{Connection, nil, err, "send stanza error"})
				}
				break
			}
		case <-self.stopSendCh:
			close(self.sendQueue)
			break
		}
	}
}

func (self *XmppClient) startReadMessage() {
	for self.connected {
		stanza, err := self.client.Recv()
		if err != nil {
			if self.connected {
				self.fireHandler(&Event{Connection, nil, err, "receive stanza error"})
			}
			break
		}
		self.fireHandler(&Event{Stanza, stanza, nil, ""})
	}
}

func (self *XmppClient) startPing() {
	errCount := 0
	stopPing := false // consider of reconnecting, so use stopPing instead of self.connected
	for !stopPing {
		select {
		case <-time.After(self.config.PingInterval):
			err := self.doPing()
			if err != nil {
				errCount++
				if errCount >= self.config.PingErrorTimes {
					if Debug {
						fmt.Println("Error!Ping timeout!")
					}
					self.handlePingError(err)
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
	copyHandlers := make([]Handler, len(self.handlers))
	copy(copyHandlers, self.handlers)
	for i := len(copyHandlers) - 1; i >= 0; i-- {
		h := copyHandlers[i]
		if h.Filter(event) {
			h.GetEventCh() <- event
			if h.IsOneTime() {
				self.RemoveHandlerByIndex(i)
			}
		}
	}
}

var reconnectTimes = 0

func (self *XmppClient) handlePingError(err error) {
	self.Disconnect()

	if !self.config.ReconnectEnable {
		self.fireHandler(&Event{Connection, nil, err, "Ping timeout!"})
		return
	}

	reconnectedSuccess := false
	for reconnectTimes < self.config.ReconnectTimes {
		reconnectTimes++
		if Debug {
			fmt.Printf("Reconnecting %d\n", reconnectTimes)
		}
		connErr := self.Connect(self.host, self.jid, self.password)
		if connErr != nil {
			if Debug {
				fmt.Println("Reconnecting error:", connErr)
				fmt.Printf("Next reconnecting after %d seconds\n", reconnectTimes*5)
			}
			// sleep more time when reconnectTimes increase
			time.Sleep(time.Duration(reconnectTimes*5) * time.Second)
			continue
		}
		if Debug {
			fmt.Printf("Reconnecting success!")
		}
		reconnectedSuccess = true
		break
	}
	if !reconnectedSuccess {
		msg := fmt.Sprintf("Ping timeout and reconnect failed after retring %d times", self.config.ReconnectTimes)
		self.fireHandler(&Event{Connection, nil, err, msg})
		return
	}

	//make sure will receive roster and subscribe message
	self.RequestRoster()
	self.SendPresenceStatus("")
}
