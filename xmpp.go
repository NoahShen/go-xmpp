// Copyright 2011 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package xmpp implements a simple Google Talk client
// using the XMPP protocol described in RFC 3920 and RFC 3921.
package xmpp

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var Debug = false

const (
	nsStream  = "http://etherx.jabber.org/streams"
	nsTLS     = "urn:ietf:params:xml:ns:xmpp-tls"
	nsSASL    = "urn:ietf:params:xml:ns:xmpp-sasl"
	nsBind    = "urn:ietf:params:xml:ns:xmpp-bind"
	nsSession = "urn:ietf:params:xml:ns:xmpp-session"
	nsClient  = "jabber:client"
)

var DefaultConfig tls.Config

type Client struct {
	conn   net.Conn // connection to server
	jid    string   // Jabber ID for our connection
	domain string
	p      *xml.Decoder
}

// NewClient creates a new connection to a host given as "hostname" or "hostname:port".
// If host is not specified, the  DNS SRV should be used to find the host from the domainpart of the JID.
// Default the port to 5222.
func NewClient(host, user, passwd string) (*Client, error) {
	addr := host

	if strings.TrimSpace(host) == "" {
		a := strings.SplitN(user, "@", 2)
		if len(a) == 2 {
			host = a[1]
		}
	}
	a := strings.SplitN(host, ":", 2)
	if len(a) == 1 {
		host += ":5222"
	}
	proxy := os.Getenv("HTTP_PROXY")
	if proxy == "" {
		proxy = os.Getenv("http_proxy")
	}
	if proxy != "" {
		url, err := url.Parse(proxy)
		if err == nil {
			addr = url.Host
		}
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	if proxy != "" {
		fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\n", host)
		fmt.Fprintf(c, "Host: %s\r\n", host)
		fmt.Fprintf(c, "\r\n")
		br := bufio.NewReader(c)
		req, _ := http.NewRequest("CONNECT", host, nil)
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			f := strings.SplitN(resp.Status, " ", 2)
			return nil, errors.New(f[1])
		}
	}

	if Debug {
		fmt.Printf("===xmpp===Connected host:%s\n", addr)
	}

	client := new(Client)
	client.conn = c
	if err := client.init(user, passwd); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) init(user, passwd string) error {
	c.p = xml.NewDecoder(c.conn)

	a := strings.SplitN(user, "@", 2)
	if len(a) != 2 {
		return errors.New("xmpp: invalid username (want user@domain): " + user)
	}
	user = a[0]
	c.domain = a[1]

	features, streamErr := c.openStreamAndGetFeatures()
	if streamErr != nil {
		return streamErr
	}

	if features.StartTLS != nil {
		if tlsErr := c.startTls(); tlsErr != nil {
			return tlsErr
		}
		features, streamErr = c.openStreamAndGetFeatures()
		if streamErr != nil {
			return streamErr
		}
	}

	if authErr := c.authenticate(features, user, passwd); authErr != nil {
		return authErr
	}

	features, streamErr = c.openStreamAndGetFeatures()
	if streamErr != nil {
		return streamErr
	}

	if features.Bind != nil {
		if bindResErr := c.bindResource(); bindResErr != nil {
			return bindResErr
		}
	}
	if features.Session != nil {
		if bindSessionErr := c.bindSession(); bindSessionErr != nil {
			return bindSessionErr
		}
	}

	return nil
}

func (c *Client) bindSession() error {
	// Send IQ message asking to bind to the local user name.
	iqBindSession := fmt.Sprintf("<iq type='set' id='x'><session xmlns='%s'/></iq>\n", nsSession)
	if Debug {
		fmt.Printf("===xmpp===send:\n%s\n", iqBindSession)
	}
	fmt.Fprint(c.conn, iqBindSession)
	var iq IQ
	if err := c.p.DecodeElement(&iq, nil); err != nil {
		return errors.New("unmarshal <iq>: " + err.Error())
	}
	if Debug {
		bytes, err := xml.MarshalIndent(iq, "", "    ")
		if err == nil {
			fmt.Printf("===xmpp===receive:%s\n", string(bytes))
		}
	}
	if iq.Type != "result" {
		return errors.New("bind session error!")
	}
	return nil
}

func (c *Client) bindResource() error {
	// Send IQ message asking to bind to the local user name.
	iqBindXml := fmt.Sprintf("<iq type='set' id='x'><bind xmlns='%s'/></iq>\n", nsBind)
	if Debug {
		fmt.Printf("===xmpp===send:\n%s\n", iqBindXml)
	}
	fmt.Fprint(c.conn, iqBindXml)
	var iq IQ
	if err := c.p.DecodeElement(&iq, nil); err != nil {
		return errors.New("unmarshal <iq>: " + err.Error())
	}
	if Debug {
		bytes, err := xml.MarshalIndent(iq, "", "    ")
		if err == nil {
			fmt.Printf("===xmpp===receive:%s\n", string(bytes))
		}
	}
	if &iq.Bind == nil {
		return errors.New("<iq> result missing <bind>")
	}
	c.jid = iq.Bind.Jid // our local id
	return nil
}

func (c *Client) authenticate(features *streamFeatures, user, password string) error {
	havePlain := false
	authenticated := false
	for _, m := range features.Mechanisms.Mechanism {
		switch m {
		case "PLAIN":
			havePlain = true
		case "DIGEST-MD5":
			// Digest-MD5 authentication
			md5Auth := fmt.Sprintf("<auth xmlns='%s' mechanism='DIGEST-MD5'/>\n", nsSASL)
			fmt.Fprintf(c.conn, md5Auth)
			if Debug {
				fmt.Printf("===xmpp===send:\n%s\n", md5Auth)
			}
			var ch saslChallenge
			if decodeErr := c.p.DecodeElement(&ch, nil); decodeErr != nil {
				return errors.New("unmarshal <challenge>: " + decodeErr.Error())
			}
			if Debug {
				challengeXml := fmt.Sprintf("<challenge xmlns='urn:ietf:params:xml:ns:xmpp-sasl'>%s</challenge>", ch)
				fmt.Printf("===xmpp===receive:%s\n", challengeXml)
			}

			b, err := base64.StdEncoding.DecodeString(string(ch))
			if err != nil {
				return err
			}
			tokens := map[string]string{}
			for _, token := range strings.Split(string(b), ",") {
				kv := strings.SplitN(strings.TrimSpace(token), "=", 2)
				if len(kv) == 2 {
					if kv[1][0] == '"' && kv[1][len(kv[1])-1] == '"' {
						kv[1] = kv[1][1 : len(kv[1])-1]
					}
					tokens[kv[0]] = kv[1]
				}
			}
			realm, _ := tokens["realm"]
			nonce, _ := tokens["nonce"]
			qop, _ := tokens["qop"]
			charset, _ := tokens["charset"]
			cnonceStr := cnonce()
			digestUri := "xmpp/" + c.domain
			nonceCount := fmt.Sprintf("%08x", 1)
			digest := saslDigestResponse(user, realm, password, nonce, cnonceStr, "AUTHENTICATE", digestUri, nonceCount)
			message := "username=\"" + user + "\"" +
				", realm=\"" + realm + "\"" +
				", nonce=\"" + nonce + "\"" +
				", cnonce=\"" + cnonceStr + "\"" +
				", nc=" + nonceCount +
				", qop=" + qop +
				", digest-uri=\"" + digestUri + "\"" +
				", response=" + digest +
				", charset=" + charset
			authResp := fmt.Sprintf("<response xmlns='%s'>%s</response>\n", nsSASL, base64.StdEncoding.EncodeToString([]byte(message)))
			fmt.Fprintf(c.conn, authResp)
			if Debug {
				fmt.Printf("===xmpp===send:\n%s\n", authResp)
			}

			//var saslResp saslResponse
			//if err = c.p.DecodeElement(&saslResp, nil); err != nil {
			//	return errors.New("unmarshal <challenge>: " + err.Error())
			//}
			//if Debug {
			//	saslRespXml := fmt.Sprintf("<response xmlns='urn:ietf:params:xml:ns:xmpp-sasl'>%s</response>", saslResp)
			//	fmt.Printf("===xmpp===receive:\n%s\n", saslRespXml)
			//}
			//b, err = base64.StdEncoding.DecodeString(string(saslResp))
			//if err != nil {
			//	return err
			//}

			//authResp2 := fmt.Sprintf("<response xmlns='%s'/>\n", nsSASL)
			//fmt.Fprintf(c.conn, authResp2)
			//if Debug {
			//	fmt.Printf("===xmpp===send:\n%s\n", authResp2)
			//}
			authenticated = true
			break
		}
	}

	if !authenticated {
		if !havePlain {
			return errors.New(fmt.Sprintf("PLAIN authentication is not an option: %v", features.Mechanisms.Mechanism))
		}

		// Plain authentication: send base64-encoded \x00 user \x00 password.
		raw := "\x00" + user + "\x00" + password
		enc := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
		base64.StdEncoding.Encode(enc, []byte(raw))

		authXml := fmt.Sprintf("<auth xmlns='%s' mechanism='PLAIN'>%s</auth>", nsSASL, enc)
		fmt.Fprintf(c.conn, authXml)
		if Debug {
			fmt.Printf("===xmpp===send:\n%s\n", authXml)
		}
	}

	// Next message should be either success or failure.
	name, val, err := next(c.p)
	if err != nil {
		return err
	}
	if Debug {
		bytes, err := xml.MarshalIndent(val, "", "    ")
		if err == nil {
			fmt.Printf("===xmpp===receive:%s\n", string(bytes))
		}
	}
	switch v := val.(type) {
	case *saslSuccess:
	case *saslFailure:
		// v.Any is type of sub-element in failure,
		// which gives a description of what failed.
		return errors.New("auth failure: " + v.Any.Local)
	default:
		return errors.New("expected <success> or <failure>, got <" + name.Local + "> in " + name.Space)
	}
	return nil
}

func saslDigestResponse(username, realm, passwd, nonce, cnonceStr, authenticate, digestUri, nonceCountStr string) string {
	h := func(text string) []byte {
		h := md5.New()
		h.Write([]byte(text))
		return h.Sum(nil)
	}
	hex := func(bytes []byte) string {
		return fmt.Sprintf("%x", bytes)
	}
	kd := func(secret, data string) []byte {
		return h(secret + ":" + data)
	}

	a1 := string(h(username+":"+realm+":"+passwd)) + ":" +
		nonce + ":" + cnonceStr
	a2 := authenticate + ":" + digestUri
	response := hex(kd(hex(h(a1)), nonce+":"+
		nonceCountStr+":"+cnonceStr+":auth:"+
		hex(h(a2))))
	return response
}

func cnonce() string {
	randSize := big.NewInt(0)
	randSize.Lsh(big.NewInt(1), 64)
	cn, err := rand.Int(rand.Reader, randSize)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%016x", cn)
}

func (c *Client) openStreamAndGetFeatures() (*streamFeatures, error) {
	// Declare intent to be a xmpp client.
	openStream := fmt.Sprintf("<?xml version='1.0'?><stream:stream to='%s' xmlns='%s' xmlns:stream='%s' version='1.0'>",
		xmlEscape(c.domain), nsClient, nsStream)
	if Debug {
		fmt.Printf("===xmpp===send:\n%s\n", openStream)
	}
	fmt.Fprint(c.conn, openStream)

	// Server should respond with a stream opening.
	se, err := nextStart(c.p)
	if err != nil {
		return nil, err
	}

	if se.Name.Space != nsStream || se.Name.Local != "stream" {
		return nil, errors.New("xmpp: expected <stream> but got <" + se.Name.Local + "> in " + se.Name.Space)
	}

	features := &streamFeatures{}
	if err = c.p.DecodeElement(features, nil); err != nil {
		return nil, errors.New("unmarshal <features>: " + err.Error())
	}
	if Debug {
		bytes, err := xml.MarshalIndent(features, "", "    ")
		if err == nil {
			fmt.Printf("===xmpp===receive:%s\n", string(bytes))
		}
	}
	return features, nil
}

func (c *Client) startTls() error {
	fmt.Fprintf(c.conn, "<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>")
	var proceed tlsProceed
	if err := c.p.DecodeElement(&proceed, nil); err != nil {
		return err
	}

	tlsconn := tls.Client(c.conn, &DefaultConfig)
	if err := tlsconn.Handshake(); err != nil {
		return err
	}
	c.conn = tlsconn
	if Debug {
		fmt.Println("===xmpp===TLS shake hand success.")
	}
	c.p = xml.NewDecoder(c.conn)
	//if strings.LastIndex(host, ":") > 0 {
	//	host = host[:strings.LastIndex(host, ":")]
	//}
	//if err = tlsconn.VerifyHostname(host); err != nil {
	//	return nil, err
	//}

	return nil
}

// Recv wait next token of chat.
func (c *Client) Recv() (stanza interface{}, err error) {
	for {
		_, stanza, err := next(c.p)
		if err != nil {
			return nil, err
		}
		if Debug {
			bytes, err := xml.MarshalIndent(stanza, "", "    ")
			if err == nil {
				fmt.Printf("===xmpp===receive:%s\n", string(bytes))
			}
		}
		return stanza, nil
	}
	panic("unreachable")
}

// Send sends message text.
func (c *Client) Send(stanza interface{}) error {
	bytes, err := xml.MarshalIndent(stanza, "", "    ")
	if err != nil {
		return err
	}
	if Debug {
		fmt.Printf("===xmpp===send:%s\n", string(bytes))
	}
	_, sendErr := c.conn.Write(bytes)
	return sendErr
}

// RFC 3920  C.1  Streams name space
type streamFeatures struct {
	XMLName    xml.Name `xml:"http://etherx.jabber.org/streams features"`
	StartTLS   *tlsStartTLS
	Mechanisms saslMechanisms
	Bind       *bindBind
	Session    *bindSession
}

type streamError struct {
	XMLName xml.Name `xml:"http://etherx.jabber.org/streams error"`
	Any     xml.Name
	Text    string
}

// RFC 3920  C.3  TLS name space

type tlsStartTLS struct {
	XMLName  xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-tls starttls"`
	Required bool
}

type tlsProceed struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-tls proceed"`
}

type tlsFailure struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-tls failure"`
}

// RFC 3920  C.4  SASL name space

type saslMechanisms struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl mechanisms"`
	Mechanism []string `xml:"mechanism"`
}

type saslAuth struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl auth"`
	Mechanism string   `xml:",attr"`
}

type saslChallenge string

type saslResponse string

type saslAbort struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl abort"`
}

type saslSuccess struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl success"`
}

type saslFailure struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl failure"`
	Any     xml.Name
}

// RFC 3920  C.5  Resource binding name space

type bindBind struct {
	XMLName  xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-bind bind"`
	Resource string   `xml:"resource,omitempty"`
	Jid      string   `xml:"jid,omitempty"`
}

type bindSession struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-session session"`
}

// RFC 3921  B.1  jabber:client

type Message struct {
	XMLName xml.Name `xml:"jabber:client message"`
	From    string   `xml:"from,attr,omitempty"`
	Id      string   `xml:"id,attr,omitempty"`
	To      string   `xml:"to,attr,omitempty"`
	Type    string   `xml:"type,attr,omitempty"` // chat, error, groupchat, headline, or normal

	// These should technically be []clientText,
	// but string is much more convenient.
	Subject string `xml:"subject,omitempty"`
	Body    string `xml:"body,omitempty"`
	Thread  string `xml:"thread,omitempty"`
}

type clientText struct {
	Lang string `xml:",attr"`
	Body string `xml:"chardata"`
}

type Presence struct {
	XMLName xml.Name `xml:"jabber:client presence"`
	From    string   `xml:"from,attr,omitempty"`
	Id      string   `xml:"id,attr,omitempty"`
	To      string   `xml:"to,attr,omitempty"`
	Type    string   `xml:"type,attr,omitempty"` // error, probe, subscribe, subscribed, unavailable, unsubscribe, unsubscribed
	Lang    string   `xml:"lang,attr,omitempty"`

	Show     string `xml:"show,omitempty"`   // away, chat, dnd, xa
	Status   string `xml:"status,omitempty"` // sb []clientText
	Priority string `xml:"priority,omitempty"`
	Error    *Error
}

type IQ struct { // info/query
	XMLName xml.Name `xml:"jabber:client iq"`
	From    string   `xml:"from,attr,omitempty"`
	Id      string   `xml:"id,attr,omitempty"`
	To      string   `xml:"to,attr,omitempty"`
	Type    string   `xml:"type,attr,omitempty"` // error, get, result, set
	Error   *Error
	Bind    *bindBind
	Roster  *IQRoster
	Ping    *Ping
}

type IQRoster struct {
	XMLName xml.Name     `xml:"jabber:iq:roster query"`
	Items   []RosterItem `xml:"item,omitempty"`
}

type RosterItem struct {
	XMLName      xml.Name `xml:"item"`
	Jid          string   `xml:"jid,attr,omitempty"`
	Subscription string   `xml:"subscription,attr,omitempty"`
	Name         string   `xml:"name,attr,omitempty"`
	Ask          string   `xml:"ask,attr,omitempty"`
	Groups       []string `xml:"groups,omitempty"`
}

type Ping struct {
	XMLName xml.Name `xml:"urn:xmpp:ping ping"`
}

type Error struct {
	XMLName xml.Name `xml:"jabber:client error"`
	Code    string   `xml:",attr"`
	Type    string   `xml:",attr"`
	Any     xml.Name
	Text    string
}

// Scan XML token stream to find next StartElement.
func nextStart(p *xml.Decoder) (xml.StartElement, error) {
	for {
		t, err := p.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		switch t := t.(type) {
		case xml.StartElement:
			return t, nil
		}
	}
	panic("unreachable")
}

// Scan XML token stream for next element and save into val.
// If val == nil, allocate new element based on proto map.
// Either way, return val.
func next(p *xml.Decoder) (xml.Name, interface{}, error) {
	// Read start element to find out what type we want.
	se, err := nextStart(p)
	if err != nil {
		return xml.Name{}, nil, err
	}

	// Put it in an interface and allocate one.
	var nv interface{}
	switch se.Name.Space + " " + se.Name.Local {
	case nsStream + " features":
		nv = &streamFeatures{}
	case nsStream + " error":
		nv = &streamError{}
	case nsTLS + " starttls":
		nv = &tlsStartTLS{}
	case nsTLS + " proceed":
		nv = &tlsProceed{}
	case nsTLS + " failure":
		nv = &tlsFailure{}
	case nsSASL + " mechanisms":
		nv = &saslMechanisms{}
	case nsSASL + " challenge":
		nv = ""
	case nsSASL + " response":
		nv = ""
	case nsSASL + " abort":
		nv = &saslAbort{}
	case nsSASL + " success":
		nv = &saslSuccess{}
	case nsSASL + " failure":
		nv = &saslFailure{}
	case nsBind + " bind":
		nv = &bindBind{}
	case nsClient + " message":
		nv = &Message{}
	case nsClient + " presence":
		nv = &Presence{}
	case nsClient + " iq":
		nv = &IQ{}
	case nsClient + " error":
		nv = &Error{}
	default:
		return xml.Name{}, nil, errors.New("unexpected XMPP message " +
			se.Name.Space + " <" + se.Name.Local + "/>")
	}

	// Unmarshal into that storage.
	if err = p.DecodeElement(nv, &se); err != nil {
		return xml.Name{}, nil, err
	}
	return se.Name, nv, err
}

var xmlSpecial = map[byte]string{
	'<':  "&lt;",
	'>':  "&gt;",
	'"':  "&quot;",
	'\'': "&apos;",
	'&':  "&amp;",
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for i := 0; i < len(s); i++ {
		c := s[i]
		if s, ok := xmlSpecial[c]; ok {
			b.WriteString(s)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

type tee struct {
	r io.Reader
	w io.Writer
}

func (t tee) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		t.w.Write(p[0:n])
	}
	return
}
