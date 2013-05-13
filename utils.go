package xmpp

import (
	"bytes"
	"errors"
	"math/rand"
	"strings"
	"time"
)

func ToBareJID(jid string) string {
	i := strings.Index(jid, "/")
	if i < 0 {
		return jid
	}
	bareJid := jid[0:i]
	return bareJid
}

func GetDomain(jid string) (string, error) {
	j := strings.TrimSpace(jid)
	a := strings.SplitN(j, "@", 2)
	if len(a) == 2 {
		return a[1], nil
	}
	return "", errors.New("invalid jid!")
}

const alpha = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func RandomString(l int) string {
	var result bytes.Buffer
	var temp string
	for i := 0; i < l; {
		c := randChar()
		if c != temp {
			temp = c
			result.WriteString(temp)
			i++
		}
	}
	return result.String()
}

func randChar() string {
	rand.Seed(time.Now().UTC().UnixNano())
	return string(alpha[rand.Intn(len(alpha)-1)])
}
