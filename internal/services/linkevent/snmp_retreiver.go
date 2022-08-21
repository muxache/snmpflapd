package linkevent

import (
	"errors"
	"log"
	"net"
	"sync"

	g "github.com/gosnmp/gosnmp"
)

type RequestSemaphore struct {
	// requestQueue []linkEvent
	mx sync.Mutex
}

func doSNMPRequest(oid string, ip net.IP, community string) (pdu *g.SnmpPacket, err error) {

	c := g.Default
	c.Community = community
	c.Target = ip.String()

	if err = c.Connect(); err != nil {
		log.Println(err)
		return nil, err
	}
	defer c.Conn.Close()

	return g.Default.Get([]string{oid})
}

func getSNMPString(oid string, ip net.IP, community string) (val *string, err error) {

	snmpSema.mx.Lock()
	defer snmpSema.mx.Unlock()

	pdu, err := doSNMPRequest(oid, ip, community)
	if err != nil {
		return nil, err
	}
	value := pdu.Variables[0].Value
	fromByte, ok := value.([]byte)
	if ok {
		s := string(fromByte)
		return &s, nil
	}
	return nil, errors.New("received nil from the device")
}
